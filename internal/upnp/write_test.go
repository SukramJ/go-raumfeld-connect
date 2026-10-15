// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package upnp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// The success reply's content is not read by the write wrappers; any
// well-formed response envelope will do.
const okReply = "soap_zone_GetTransportInfo.xml"

func TestWriteWrappers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const room = "uuid:5d4abab7-f576-4939-bf66-0e1ec55d8c8f"
	for _, tc := range []struct {
		svcType, action, wantBody string
		call                      func(*http.Client, Service) error
	}{
		{
			ServiceRenderingControl, "SetVolume",
			"<InstanceID>0</InstanceID><Channel>Master</Channel><DesiredVolume>12</DesiredVolume>",
			func(hc *http.Client, s Service) error { return SetVolume(ctx, hc, s, 12) },
		},
		{
			ServiceAVTransport, "SetAVTransportURI",
			"<InstanceID>0</InstanceID><CurrentURI>http://192.0.2.4:59317/live.wav?a=1&amp;b=2</CurrentURI><CurrentURIMetaData></CurrentURIMetaData>",
			func(hc *http.Client, s Service) error {
				return SetAVTransportURI(ctx, hc, s, "http://192.0.2.4:59317/live.wav?a=1&b=2", "")
			},
		},
		{
			ServiceAVTransport, "Play", "<InstanceID>0</InstanceID><Speed>1</Speed>",
			func(hc *http.Client, s Service) error { return Play(ctx, hc, s) },
		},
		{
			ServiceAVTransport, "Stop", "<InstanceID>0</InstanceID></u:Stop>",
			func(hc *http.Client, s Service) error { return Stop(ctx, hc, s) },
		},
		{
			ServiceAVTransport, "LeaveStandby", "<InstanceID>0</InstanceID><Room>" + room + "</Room>",
			func(hc *http.Client, s Service) error { return LeaveStandby(ctx, hc, s, room) },
		},
		{
			ServiceAVTransport, "EnterAutomaticStandby", "<InstanceID>0</InstanceID><Room>" + room + "</Room>",
			func(hc *http.Client, s Service) error { return EnterAutomaticStandby(ctx, hc, s, room) },
		},
	} {
		hc, svc := fakeService(t, tc.svcType, tc.action, tc.wantBody, okReply, http.StatusOK)
		if err := tc.call(hc, svc); err != nil {
			t.Errorf("%s: %v", tc.action, err)
		}
	}
}

func TestSetVolumeRange(t *testing.T) {
	t.Parallel()
	for _, v := range []int{-1, MaxVolume + 1} {
		// No server: an out-of-range volume must fail before any request.
		if err := SetVolume(context.Background(), http.DefaultClient, Service{}, v); err == nil ||
			!strings.Contains(err.Error(), "outside") {
			t.Errorf("SetVolume(%d) = %v, want range error", v, err)
		}
	}
}

func TestWriteFault(t *testing.T) {
	t.Parallel()
	hc, svc := fakeService(t, ServiceAVTransport, "Play", "<Speed>1</Speed>", "soap_fault_unknown_action.xml", http.StatusInternalServerError)
	if err := Play(context.Background(), hc, svc); err == nil {
		t.Error("Play with fault reply: want error")
	}
}
