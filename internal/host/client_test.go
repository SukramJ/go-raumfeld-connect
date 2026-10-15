// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package host

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

const testSession = "5f95f034-f64b-4c62-b25f-36dd07f00301"

// newFakeHost mimics the measured host: requests outside a session prefix
// get 307 to /<session>/<path>, requests inside it get the captured file.
func newFakeHost(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var redirects atomic.Int32
	files := map[string]string{"getZones": "testdata/getZones.xml", "listDevices": "testdata/listDevices.xml"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/" + testSession + "/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			redirects.Add(1)
			w.Header().Set("Location", "http://"+r.Host+prefix+strings.TrimPrefix(r.URL.Path, "/"))
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		f, ok := files[strings.TrimPrefix(r.URL.Path, prefix)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		body, err := os.ReadFile(f)
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("updateID", "427800099")
		w.Header().Set("Content-Type", "text/xml; charset=UTF-8")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &redirects
}

func TestGetZones(t *testing.T) {
	t.Parallel()
	srv, redirects := newFakeHost(t)
	c, err := New(srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	zc, err := c.GetZones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if zc.UpdateID != "427800099" || zc.NumRooms != 4 || zc.SpotifyMode != "singleRoom" {
		t.Errorf("header/attrs = %q %d %q", zc.UpdateID, zc.NumRooms, zc.SpotifyMode)
	}
	if len(zc.Zones) != 3 {
		t.Fatalf("zones = %d, want 3", len(zc.Zones))
	}
	z := zc.Zones[1]
	if z.UDN != "uuid:9a3ff63f-e18a-481a-9701-8297b6b30f76" || len(z.Rooms) != 1 ||
		z.Rooms[0].Name != "Terrace" || z.Rooms[0].PowerState != "MANUAL_STANDBY" ||
		z.Rooms[0].Renderers[0].Name != "Speaker Terrace" {
		t.Errorf("zone[1] = %+v", z)
	}
	if len(zc.UnassignedRooms) != 1 || zc.UnassignedRooms[0].Name != "Living Room" ||
		zc.UnassignedRooms[0].Renderers[0].SpotifyConnect != "active" {
		t.Errorf("unassigned = %+v", zc.UnassignedRooms)
	}

	// The session learned on the first call is reused.
	if _, err := c.ListDevices(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := redirects.Load(); n != 1 {
		t.Errorf("redirects = %d, want 1", n)
	}
}

func TestListDevices(t *testing.T) {
	t.Parallel()
	srv, _ := newFakeHost(t)
	c, err := New(srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	dl, err := c.ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dl.Devices) != 13 {
		t.Fatalf("devices = %d, want 13", len(dl.Devices))
	}
	var renderers int
	for _, d := range dl.Devices {
		if d.Type == "urn:schemas-upnp-org:device:MediaRenderer:1" {
			renderers++
		}
	}
	if renderers != 7 {
		t.Errorf("renderers = %d, want 7", renderers)
	}
	d := dl.Devices[2]
	if d.Name != "Speaker Bathroom" || d.Location != "http://192.0.2.67:54184/39bcee0b-1644-46d3-9ba8-ffb284bcfe21.xml" {
		t.Errorf("device[2] = %+v", d)
	}
}

func TestSessionFromLocation(t *testing.T) {
	t.Parallel()
	c, err := New("http://192.0.2.65:47365", http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ loc, want string }{
		{"/d76889a2-4d82-4b17-b7de-19e3edab6d65/", "d76889a2-4d82-4b17-b7de-19e3edab6d65"},
		{"http://192.0.2.65:47365/f666e281-8c88-4a3c-a7b3-b4a5694200cb/getZones", "f666e281-8c88-4a3c-a7b3-b4a5694200cb"},
	} {
		got, err := sessionFromLocation(c.base, tc.loc)
		if err != nil || got != tc.want {
			t.Errorf("sessionFromLocation(%q) = %q, %v; want %q", tc.loc, got, err, tc.want)
		}
	}
	if _, err := sessionFromLocation(c.base, ""); err == nil {
		t.Error("empty Location: want error")
	}
}
