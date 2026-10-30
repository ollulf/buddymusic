package main

import (
	"net/http"
	"testing"
)

func TestUploadRejectsNonMp3Flac(t *testing.T) {
	needFFmpeg(t)
	a := newTestApp(t)
	cookie := login(t, a)
	for _, name := range []string{"r.m4a", "r.ogg", "r.opus"} {
		data := makeAudio(t, name, false, "title", "Nope")
		rec, _ := postUpload(t, a, cookie, formPart{"file", name, data})
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	wma := append([]byte{0x30, 0x26, 0xB2, 0x75, 0x8E, 0x66, 0xCF, 0x11, 0xA6, 0xD9, 0x00, 0xAA, 0x00, 0x62, 0xCE, 0x6C}, make([]byte, 600)...)
	rec, _ := postUpload(t, a, cookie, formPart{"file", "r.wma", wma})
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("wma: %d %s", rec.Code, rec.Body)
	}
}
