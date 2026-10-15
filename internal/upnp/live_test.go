// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build live

package upnp

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestLiveVolumeRoundTrip changes a real renderer's volume by one step and
// restores it. It runs only with `-tags live` and
// RAUMFELD_LIVE_RENDERER set to a zone renderer's description URL; use it
// only on a room the owner has released for write access.
func TestLiveVolumeRoundTrip(t *testing.T) {
	loc := os.Getenv("RAUMFELD_LIVE_RENDERER")
	if loc == "" {
		t.Skip("RAUMFELD_LIVE_RENDERER not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	hc := &http.Client{Timeout: 5 * time.Second}

	dev, err := FetchDevice(ctx, hc, loc)
	if err != nil {
		t.Fatal(err)
	}
	rc, ok := dev.Service(ServiceRenderingControl)
	if !ok {
		t.Fatal("no RenderingControl")
	}
	orig, err := GetVolume(ctx, hc, rc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := SetVolume(context.Background(), hc, rc, orig); err != nil {
			t.Errorf("restore volume %d: %v", orig, err)
		}
	})
	want := orig - 1
	if orig == 0 {
		want = 1
	}
	if err := SetVolume(ctx, hc, rc, want); err != nil {
		t.Fatal(err)
	}
	if got, err := GetVolume(ctx, hc, rc); err != nil || got != want {
		t.Fatalf("after SetVolume(%d): GetVolume = %d, %v", want, got, err)
	}
	t.Logf("%s: volume %d -> %d -> restoring %d", dev.FriendlyName, orig, want, orig)
}
