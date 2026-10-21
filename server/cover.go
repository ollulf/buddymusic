package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	coverMaxSide  = 1500
	coverMaxBytes = 1 << 20
	coverTarget   = 1200
)

func coverTooBig(data []byte) bool {
	if len(data) > coverMaxBytes {
		return true
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return false
	}
	return cfg.Width > coverMaxSide || cfg.Height > coverMaxSide
}

func shrinkCover(ctx context.Context, dir string, data []byte) []byte {
	if !coverTooBig(data) {
		return data
	}
	out, err := resizeCover(ctx, dir, data)
	if err != nil {
		log.Printf("cover shrink failed, keeping original: %v", err)
		return data
	}
	return out
}

func resizeCover(ctx context.Context, dir string, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	in, err := os.CreateTemp(dir, ".cover-in-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(in.Name())
	if _, err := in.Write(data); err != nil {
		in.Close()
		return nil, err
	}
	if err := in.Close(); err != nil {
		return nil, err
	}
	out := in.Name() + ".jpg"
	defer os.Remove(out)
	scale := fmt.Sprintf("scale='min(%d,iw)':'min(%d,ih)':force_original_aspect_ratio=decrease", coverTarget, coverTarget)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-nostdin", "-i", in.Name(), "-frames:v", "1", "-vf", scale, "-pix_fmt", "yuvj420p", "-q:v", "3", "-f", "image2", "-c:v", "mjpeg", "-y", out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(msg)))
	}
	res, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	if sniffImage(res) != ".jpg" {
		return nil, fmt.Errorf("ffmpeg produced no jpeg")
	}
	return res, nil
}
