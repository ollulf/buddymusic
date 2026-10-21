package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type upstreamMode struct {
	name    string
	length  bool
	cutAt   int
	stallAt int
	noHead  bool
	status  int
}

type streamOutcome struct {
	status   int
	got      int
	err      error
	hung     bool
	elapsed  time.Duration
	upClosed *atomic.Bool
}

func (o streamOutcome) String() string {
	switch {
	case o.hung:
		return fmt.Sprintf("hang (got %d bytes, still waiting after %s)", o.got, o.elapsed.Round(100*time.Millisecond))
	case o.err != nil:
		return fmt.Sprintf("status %d, error after %d bytes in %s", o.status, o.got, o.elapsed.Round(100*time.Millisecond))
	default:
		return fmt.Sprintf("status %d, clean end after %d bytes in %s", o.status, o.got, o.elapsed.Round(100*time.Millisecond))
	}
}

var streamBody = bytes.Repeat([]byte("OggS-opus-frame-"), 4096)

func stubStream(mode upstreamMode, closed *atomic.Bool) func(w http.ResponseWriter, r *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/stream.view") {
			return false
		}
		defer func() {
			if r.Context().Err() != nil {
				closed.Store(true)
			}
		}()
		if mode.noHead {
			<-r.Context().Done()
			return true
		}
		if mode.status != 0 {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(mode.status)
			return true
		}
		w.Header().Set("Content-Type", "audio/ogg")
		if mode.length {
			w.Header().Set("Content-Length", strconv.Itoa(len(streamBody)))
		}
		w.WriteHeader(http.StatusOK)
		end := len(streamBody)
		if mode.cutAt > 0 {
			end = mode.cutAt
		}
		if mode.stallAt > 0 {
			end = mode.stallAt
		}
		for off := 0; off < end; off += 8192 {
			w.Write(streamBody[off:min(off+8192, end)])
			w.(http.Flusher).Flush()
		}
		if mode.stallAt > 0 {
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
			}
			return true
		}
		if mode.cutAt > 0 {
			panic(http.ErrAbortHandler)
		}
		return true
	}
}

func runStreamScenario(t *testing.T, mode upstreamMode, wait time.Duration) streamOutcome {
	t.Helper()
	var closed atomic.Bool
	a := newTestAppWith(t, stubStream(mode, &closed))
	a.cfg.TrustedNets = append(a.cfg.TrustedNets, netip.MustParsePrefix("127.0.0.0/8"))
	app := httptest.NewServer(a.Routes())
	t.Cleanup(app.Close)
	target, _ := url.Parse(app.URL)
	edge := httptest.NewUnstartedServer(httputil.NewSingleHostReverseProxy(target))
	edge.EnableHTTP2 = true
	edge.Config.ErrorLog = nil
	edge.StartTLS()
	t.Cleanup(edge.Close)
	client := edge.Client()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", edge.URL+"/api/music/stream/s1?format=opus&maxBitRate=128", nil)
	req.AddCookie(login(t, a))
	start := time.Now()
	out := streamOutcome{upClosed: &closed}
	res, err := client.Do(req)
	if err != nil {
		out.err = err
		out.hung = errors.Is(err, context.DeadlineExceeded)
		out.elapsed = time.Since(start)
		return out
	}
	defer res.Body.Close()
	if res.ProtoMajor != 2 {
		t.Fatalf("edge spoke %s, want HTTP/2", res.Proto)
	}
	out.status = res.StatusCode
	n, err := io.Copy(io.Discard, res.Body)
	out.got = int(n)
	out.err = err
	out.hung = errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil
	out.elapsed = time.Since(start)
	return out
}

func withStreamIdle(t *testing.T, d time.Duration) {
	old := streamIdle
	streamIdle = d
	t.Cleanup(func() { streamIdle = old })
}

func TestStreamStabilityScenarios(t *testing.T) {
	withStreamIdle(t, 700*time.Millisecond)
	full := len(streamBody)
	cases := []struct {
		mode  upstreamMode
		check func(o streamOutcome) bool
	}{
		{upstreamMode{name: "original file, Content-Length", length: true}, func(o streamOutcome) bool { return o.err == nil && o.got == full }},
		{upstreamMode{name: "transcode, no Content-Length"}, func(o streamOutcome) bool { return o.err == nil && o.got == full }},
		{upstreamMode{name: "original cut mid-body", length: true, cutAt: full / 2}, func(o streamOutcome) bool { return o.err != nil && !o.hung }},
		{upstreamMode{name: "transcode cut mid-body", cutAt: full / 2}, func(o streamOutcome) bool { return o.err != nil && !o.hung }},
		{upstreamMode{name: "transcode stalls mid-body", stallAt: full / 4}, func(o streamOutcome) bool { return o.err != nil && !o.hung }},
		{upstreamMode{name: "original stalls mid-body", length: true, stallAt: full / 4}, func(o streamOutcome) bool { return o.err != nil && !o.hung }},
		{upstreamMode{name: "upstream never answers", noHead: true}, func(o streamOutcome) bool { return o.status == http.StatusBadGateway }},
		{upstreamMode{name: "transcode Range 416", status: http.StatusRequestedRangeNotSatisfiable}, func(o streamOutcome) bool { return o.status == http.StatusRequestedRangeNotSatisfiable }},
	}
	for _, c := range cases {
		t.Run(c.mode.name, func(t *testing.T) {
			o := runStreamScenario(t, c.mode, 3*time.Second)
			t.Logf("RESULT %s => %s", c.mode.name, o)
			if !c.check(o) {
				t.Errorf("%s: %s", c.mode.name, o)
			}
		})
	}
}

func TestStreamClientGoneCancelsUpstream(t *testing.T) {
	o := runStreamScenario(t, upstreamMode{name: "client leaves during stall", stallAt: 8192}, 400*time.Millisecond)
	if !o.hung && o.err == nil {
		t.Fatalf("expected the client to give up: %s", o)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !o.upClosed.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !o.upClosed.Load() {
		t.Fatal("upstream request still open after the client left")
	}
	t.Logf("RESULT client leaves during stall => upstream request cancelled")
}
