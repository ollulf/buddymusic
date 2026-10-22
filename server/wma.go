package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

var asfMagic = []byte{0x30, 0x26, 0xB2, 0x75, 0x8E, 0x66, 0xCF, 0x11, 0xA6, 0xD9, 0x00, 0xAA, 0x00, 0x62, 0xCE, 0x6C}

var errConvert = errors.New("conversion failed")

func isASF(head []byte) bool {
	return bytes.HasPrefix(head, asfMagic)
}

func wmaCodec(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-print_format", "json", "-select_streams", "a:0", "-show_entries", "stream=codec_name:format=format_name", "--", path).Output()
	if err != nil {
		return "", fmt.Errorf("ffprobe: %w", err)
	}
	var p struct {
		Format struct {
			FormatName string `json:"format_name"`
		} `json:"format"`
		Streams []struct {
			CodecName string `json:"codec_name"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return "", fmt.Errorf("ffprobe output: %w", err)
	}
	if p.Format.FormatName != "asf" || len(p.Streams) == 0 {
		return "", errNotAudio
	}
	return p.Streams[0].CodecName, nil
}

func wmaTarget(codec string) (string, []string, error) {
	switch codec {
	case "wmalossless":
		return ".flac", []string{"-c:a", "flac", "-compression_level", "8"}, nil
	case "wmav1", "wmav2", "wmapro", "wmavoice":
		return ".mp3", []string{"-c:a", "libmp3lame", "-q:a", "0", "-id3v2_version", "3"}, nil
	}
	return "", nil, errNotAudio
}

func convertWMA(ctx context.Context, in, base string) (string, string, error) {
	codec, err := wmaCodec(ctx, in)
	if err != nil {
		return "", "", err
	}
	ext, codecArgs, err := wmaTarget(codec)
	if err != nil {
		return "", "", err
	}
	out := base + ext
	tmp := base + ".converting" + ext
	if err := runConvert(ctx, in, tmp, ext, codecArgs, true); err != nil {
		os.Remove(tmp)
		if err := runConvert(ctx, in, tmp, ext, codecArgs, false); err != nil {
			os.Remove(tmp)
			return "", "", fmt.Errorf("%w: %v", errConvert, err)
		}
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return "", "", err
	}
	return out, ext, nil
}

func runConvert(ctx context.Context, in, out, ext string, codecArgs []string, withCover bool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	args := []string{"-v", "error", "-nostdin", "-i", in, "-map", "0:a:0"}
	if withCover {
		args = append(args, "-map", "0:v:0?", "-c:v", "copy", "-disposition:v", "attached_pic")
	}
	args = append(args, "-map_metadata", "0")
	args = append(args, codecArgs...)
	args = append(args, "-f", ffmpegFormat(ext), "-y", out)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(msg)))
	}
	return nil
}
