package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var requiredFields = []string{"title", "artist", "album", "track", "cover"}

var textFields = []string{"title", "artist", "album", "track", "album_artist", "year", "disc"}

var tagAliases = map[string]string{
	"title":        "title",
	"artist":       "artist",
	"album":        "album",
	"track":        "track",
	"tracknumber":  "track",
	"album_artist": "album_artist",
	"albumartist":  "album_artist",
	"album artist": "album_artist",
	"date":         "year",
	"year":         "year",
	"disc":         "disc",
	"discnumber":   "disc",
}

const maxTagLen = 300

type Tags struct {
	Fields map[string]string
	Cover  bool
}

type probeOutput struct {
	Format struct {
		FormatName string            `json:"format_name"`
		Tags       map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecType   string            `json:"codec_type"`
		Tags        map[string]string `json:"tags"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
}

var errNotAudio = errors.New("not an audio file")

func probe(ctx context.Context, path string) (*Tags, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-print_format", "json", "-show_format", "-show_streams", "--", path).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	return parseProbe(out)
}

func parseProbe(out []byte) (*Tags, error) {
	var p probeOutput
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("ffprobe output: %w", err)
	}
	if !allowedFormat(p.Format.FormatName) {
		return nil, errNotAudio
	}
	t := &Tags{Fields: map[string]string{}}
	hasAudio := false
	for _, s := range p.Streams {
		switch {
		case s.CodecType == "audio":
			hasAudio = true
			addTags(t.Fields, s.Tags)
		case s.CodecType == "video" && s.Disposition.AttachedPic == 1:
			t.Cover = true
		}
	}
	if !hasAudio {
		return nil, errNotAudio
	}
	addTags(t.Fields, p.Format.Tags)
	return t, nil
}

func allowedFormat(name string) bool {
	for _, f := range strings.Split(name, ",") {
		switch f {
		case "flac", "mp3", "ogg", "mov", "mp4", "m4a":
			return true
		}
	}
	return false
}

func addTags(dst, src map[string]string) {
	for k, v := range src {
		key, ok := tagAliases[strings.ToLower(k)]
		if !ok {
			continue
		}
		if v = cleanTag(v); v != "" {
			dst[key] = v
		}
	}
}

func cleanTag(v string) string {
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, v)
	v = strings.Join(strings.Fields(v), " ")
	if len(v) > maxTagLen {
		v = strings.ToValidUTF8(v[:maxTagLen], "")
	}
	return v
}

func parseTyped(raw []byte) (map[string]string, error) {
	out := map[string]string{}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return out, nil
	}
	var in map[string]any
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	for _, k := range textFields {
		var s string
		switch v := in[k].(type) {
		case string:
			s = v
		case float64:
			s = strconv.FormatFloat(v, 'f', -1, 64)
		}
		if s = cleanTag(s); s != "" {
			out[k] = s
		}
	}
	return out, nil
}

func number(v string) int {
	head, _, _ := strings.Cut(strings.TrimSpace(v), "/")
	n, err := strconv.Atoi(strings.TrimSpace(head))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func mergeTags(file *Tags, typed map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range file.Fields {
		out[k] = v
	}
	for k, v := range typed {
		out[k] = v
	}
	return out
}

func missingTags(fields map[string]string, hasCover bool) []string {
	missing := []string{}
	for _, k := range requiredFields {
		switch k {
		case "cover":
			if !hasCover {
				missing = append(missing, k)
			}
		case "track":
			if number(fields[k]) == 0 {
				missing = append(missing, k)
			}
		default:
			if strings.TrimSpace(fields[k]) == "" {
				missing = append(missing, k)
			}
		}
	}
	return missing
}

func writeTags(ctx context.Context, in, out, ext string, typed map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	flag := "-metadata"
	if ext == ".ogg" || ext == ".opus" {
		flag = "-metadata:s:a:0"
	}
	args := []string{"-v", "error", "-nostdin", "-i", in, "-map", "0", "-c", "copy"}
	if ext == ".mp3" {
		args = append(args, "-id3v2_version", "3")
	}
	for _, k := range textFields {
		v, ok := typed[k]
		if !ok {
			continue
		}
		key := k
		if k == "year" {
			key = "date"
		}
		args = append(args, flag, key+"="+v)
	}
	args = append(args, "-f", ffmpegFormat(ext), "-y", out)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if msg, err := cmd.CombinedOutput(); err != nil {
		os.Remove(out)
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(msg)))
	}
	return nil
}

func ffmpegFormat(ext string) string {
	switch ext {
	case ".m4a":
		return "ipod"
	case ".opus":
		return "opus"
	case ".ogg":
		return "ogg"
	}
	return strings.TrimPrefix(ext, ".")
}

func extractCover(ctx context.Context, audio, out string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tmp := out + ".tmp.jpg"
	defer os.Remove(tmp)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-nostdin", "-i", audio, "-map", "0:v:0", "-frames:v", "1", "-f", "image2", "-c:v", "mjpeg", "-y", tmp)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(msg)))
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	return writeNew(out, data)
}
