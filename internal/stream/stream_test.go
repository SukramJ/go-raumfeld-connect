// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package stream

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWAVHeader(t *testing.T) {
	t.Parallel()
	h := wavHeader(CD)
	if len(h) != 44 || !bytes.Equal(h[0:4], []byte("RIFF")) || !bytes.Equal(h[8:16], []byte("WAVEfmt ")) ||
		!bytes.Equal(h[36:40], []byte("data")) {
		t.Fatalf("header = % x", h)
	}
	for _, c := range []struct {
		off  int
		want uint32
	}{{24, 44100}, {28, 176400}, {40, 0xFFFFFFFF - 36}} {
		if got := binary.LittleEndian.Uint32(h[c.off:]); got != c.want {
			t.Errorf("header[%d] = %d, want %d", c.off, got, c.want)
		}
	}
	if got := binary.LittleEndian.Uint16(h[32:]); got != 4 {
		t.Errorf("block align = %d, want 4", got)
	}
}

func TestTonePacing(t *testing.T) {
	t.Parallel()
	tone := NewTone(440, 0.1, 300*time.Millisecond)
	start := time.Now()
	data, err := io.ReadAll(tone)
	if err != nil {
		t.Fatal(err)
	}
	if want := int(0.3 * float64(CD.BytesPerSecond())); len(data) != want {
		t.Errorf("bytes = %d, want %d", len(data), want)
	}
	if el := time.Since(start); el < 250*time.Millisecond {
		t.Errorf("300 ms of audio delivered in %v: not paced", el)
	}
}

func TestHandlerServesWAV(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(NewHandler(NewTone(440, 0.1, 200*time.Millisecond), slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/zone.wav")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "audio/wav" {
		t.Errorf("Content-Type = %q", ct)
	}
	if want := 44 + int(0.2*float64(CD.BytesPerSecond())); len(body) != want {
		t.Errorf("body = %d bytes, want %d", len(body), want)
	}

	req, _ := http.NewRequest(http.MethodHead, srv.URL+"/zone.wav", http.NoBody)
	head, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = head.Body.Close()
	if head.StatusCode != http.StatusOK {
		t.Errorf("HEAD = %s", head.Status)
	}
}

func TestHandlerTakeover(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(NewHandler(NewTone(440, 0.1, 0), slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)

	first, err := srv.Client().Get(srv.URL + "/zone.wav")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Body.Close() }()
	second, err := srv.Client().Get(srv.URL + "/zone.wav")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Body.Close() }()

	// The first stream ends once the second takes over.
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, first.Body); done <- err }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("first stream still running after takeover")
	}
	buf := make([]byte, 1000)
	if _, err := io.ReadFull(second.Body, buf); err != nil {
		t.Fatalf("second stream: %v", err)
	}
}
