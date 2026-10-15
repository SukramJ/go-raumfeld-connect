// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package airplay

import (
	"log/slog"
	"net"
	"path/filepath"
	"testing"
)

func TestSessionLine(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ in, want string }{
		{
			`         0.000462635 "player.c:4254" Connection 1: AirPlay 2 Buffered playback.`,
			"Connection 1: AirPlay 2 Buffered playback.",
		},
		{
			`         8.414630578 "player.c:3397" Connection 1: Playback stopped. Total playing time 00:00:45.`,
			"Connection 1: Playback stopped. Total playing time 00:00:45.",
		},
		{`plain "quoted" text`, `plain "quoted" text`},
	} {
		if got := sessionLine(c.in); got != c.want {
			t.Errorf("sessionLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestZoneVolume(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   float64
		max  int
		want int
	}{{-144, 100, 0}, {-30, 100, 0}, {-15, 100, 50}, {0, 100, 100}, {-15, 40, 20}, {3, 40, 40}} {
		if got := ZoneVolume(c.in, c.max); got != c.want {
			t.Errorf("ZoneVolume(%v, %d) = %d, want %d", c.in, c.max, got, c.want)
		}
	}
}

func TestVolumeHookRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	sock := filepath.Join(t.TempDir(), "v.sock")
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "unix", sock)
	if err != nil {
		t.Skip("unix sockets unavailable:", err)
	}
	defer func() { _ = ln.Close() }()
	vol := make(chan float64, 1)
	go acceptVolume(ctx, ln, vol, slog.New(slog.DiscardHandler))

	if err := SendVolume(ctx, sock, "-12.500000"); err != nil {
		t.Fatal(err)
	}
	if v := <-vol; v != -12.5 {
		t.Fatalf("volume = %v, want -12.5", v)
	}
	if err := SendVolume(ctx, sock, "loud"); err == nil {
		t.Fatal("SendVolume accepted a non-number")
	}
}
