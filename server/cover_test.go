package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	jpegdec "image/jpeg"
	pngenc "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func testImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	return img
}

func TestReplaceCoverShrinksLarge(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "cover.png"), []byte("old"), 0o644)
	var buf bytes.Buffer
	if err := pngenc.Encode(&buf, testImage(2400, 1800)); err != nil {
		t.Fatal(err)
	}
	if err := replaceCover(context.Background(), dir, buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cover.png")); !os.IsNotExist(err) {
		t.Fatal("cover.png should be removed")
	}
	data, err := os.ReadFile(filepath.Join(dir, "cover.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpegdec.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 1200 || cfg.Height != 900 {
		t.Fatalf("got %dx%d, want 1200x900", cfg.Width, cfg.Height)
	}
}

func TestReplaceCoverKeepsSmall(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := pngenc.Encode(&buf, testImage(300, 300)); err != nil {
		t.Fatal(err)
	}
	if err := replaceCover(context.Background(), dir, buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "cover.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, buf.Bytes()) {
		t.Fatal("small cover changed")
	}
}
