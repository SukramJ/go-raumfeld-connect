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

func TestHookRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	sock := filepath.Join(t.TempDir(), "h.sock")
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "unix", sock)
	if err != nil {
		t.Skip("unix sockets unavailable:", err)
	}
	defer func() { _ = ln.Close() }()
	events := make(chan Event, 1)
	go acceptEvents(ctx, ln, events, slog.New(slog.DiscardHandler))

	for _, c := range []struct {
		args []string
		want Event
	}{
		{[]string{"active"}, Event{Kind: Active}},
		{[]string{"volume", "-12.500000"}, Event{Kind: Volume, Volume: -12.5}},
		{[]string{"inactive"}, Event{Kind: Inactive}},
	} {
		if err := SendEvent(ctx, sock, c.args); err != nil {
			t.Fatal(err)
		}
		if got := <-events; got != c.want {
			t.Fatalf("%v: event = %+v, want %+v", c.args, got, c.want)
		}
	}
	for _, bad := range [][]string{{"volume", "loud"}, {"volume"}, {"stop"}, nil} {
		if err := SendEvent(ctx, sock, bad); err == nil {
			t.Errorf("SendEvent(%q) accepted", bad)
		}
	}
}
