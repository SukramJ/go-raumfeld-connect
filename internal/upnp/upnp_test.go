// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ

package upnp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseDevice(t *testing.T) {
	t.Parallel()
	zone, err := ParseDevice("http://192.0.2.65:54572/9a3ff63f.xml", mustRead(t, "desc_zone.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if zone.ModelDescription != "Virtual Media Player" || zone.FriendlyName != "Terrace" {
		t.Errorf("zone = %+v", zone)
	}
	av, ok := zone.Service(ServiceAVTransport)
	if !ok {
		t.Fatal("zone: no AVTransport")
	}
	// SCPDURL without leading slash and controlURL with one both resolve
	// against the description's location.
	if av.SCPDURL != "http://192.0.2.65:54572/TransportService.xml" ||
		av.ControlURL != "http://192.0.2.65:54572/TransportService/Control" {
		t.Errorf("zone AVTransport = %+v", av)
	}

	room, err := ParseDevice("http://192.0.2.69:56239/d63d237a.xml", mustRead(t, "desc_room.xml"))
	if err != nil {
		t.Fatal(err)
	}
	rc, ok := room.Service(ServiceRenderingControl)
	if !ok || rc.ControlURL != "http://192.0.2.69:56239/RenderingControl/ctrl" {
		t.Errorf("room RenderingControl = %+v, %v", rc, ok)
	}
	if room.ModelDescription != "Digital Media Player" {
		t.Errorf("room = %+v", room)
	}
}

// fakeService answers every SOAP request with the named testdata file and
// status, after checking the SOAPACTION header and the request body.
func fakeService(t *testing.T, svcType, action, wantBody, file string, status int) (*http.Client, Service) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("SOAPACTION"), `"`+svcType+"#"+action+`"`; got != want {
			t.Errorf("SOAPACTION = %s, want %s", got, want)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), wantBody) {
			t.Errorf("request body %s lacks %s", body, wantBody)
		}
		w.WriteHeader(status)
		_, _ = w.Write(mustRead(t, file))
	}))
	t.Cleanup(srv.Close)
	return srv.Client(), Service{Type: svcType, ControlURL: srv.URL + "/ctrl"}
}

func TestReadWrappers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volArgs := "<InstanceID>0</InstanceID><Channel>Master</Channel>"

	hc, svc := fakeService(t, ServiceRenderingControl, "GetVolume", volArgs, "soap_zone_GetVolume.xml", http.StatusOK)
	if v, err := GetVolume(ctx, hc, svc); err != nil || v != 34 {
		t.Errorf("GetVolume = %d, %v", v, err)
	}

	hc, svc = fakeService(t, ServiceRenderingControl, "GetMute", volArgs, "soap_zone_GetMute.xml", http.StatusOK)
	if m, err := GetMute(ctx, hc, svc); err != nil || m {
		t.Errorf("GetMute = %v, %v", m, err)
	}

	hc, svc = fakeService(t, ServiceAVTransport, "GetTransportInfo", "<InstanceID>0</InstanceID>", "soap_zone_GetTransportInfo.xml", http.StatusOK)
	if ti, err := GetTransportInfo(ctx, hc, svc); err != nil || ti != (TransportInfo{State: "STOPPED", Status: "OK", Speed: "1"}) {
		t.Errorf("GetTransportInfo = %+v, %v", ti, err)
	}

	hc, svc = fakeService(t, ServiceAVTransport, "GetPositionInfo", "<InstanceID>0</InstanceID>", "soap_zone_GetPositionInfo.xml", http.StatusOK)
	pi, err := GetPositionInfo(ctx, hc, svc)
	if err != nil || pi.Track != "1" || pi.TrackURI != "http://192.0.2.1/stream.mp3" ||
		!strings.HasPrefix(pi.TrackMetaData, "<DIDL-Lite") {
		t.Errorf("GetPositionInfo zone = %+v, %v", pi, err)
	}

	hc, svc = fakeService(t, ServiceAVTransport, "GetPositionInfo", "<InstanceID>0</InstanceID>", "soap_room_GetPositionInfo.xml", http.StatusOK)
	if pi, err := GetPositionInfo(ctx, hc, svc); err != nil ||
		pi != (PositionInfo{TrackDuration: "NOT_IMPLEMENTED", RelTime: "NOT_IMPLEMENTED"}) {
		t.Errorf("GetPositionInfo room = %+v, %v", pi, err)
	}

	hc, svc = fakeService(t, ServiceAVTransport, "GetMediaInfo", "<InstanceID>0</InstanceID>", "soap_zone_GetMediaInfo.xml", http.StatusOK)
	if mi, err := GetMediaInfo(ctx, hc, svc); err != nil || mi.NrTracks != "1" || mi.CurrentURI != "http://192.0.2.1/stream.mp3" {
		t.Errorf("GetMediaInfo = %+v, %v", mi, err)
	}
}

func TestFault(t *testing.T) {
	t.Parallel()
	hc, svc := fakeService(t, ServiceAVTransport, "GetBogus", "<InstanceID>0</InstanceID>", "soap_fault_unknown_action.xml", http.StatusInternalServerError)
	_, err := Call(context.Background(), hc, svc, "GetBogus", instance0)
	var upnpErr *Error
	if !errors.As(err, &upnpErr) || upnpErr.Code != 401 || upnpErr.Description != "Invalid Action" {
		t.Errorf("err = %v, want upnp error 401 Invalid Action", err)
	}
}

func TestMissingArgument(t *testing.T) {
	t.Parallel()
	// The room renderer's GetPositionInfo response lacks CurrentVolume, so
	// using it as a GetVolume answer must fail instead of returning 0.
	hc, svc := fakeService(t, ServiceRenderingControl, "GetVolume", "<Channel>Master</Channel>", "soap_room_GetPositionInfo.xml", http.StatusOK)
	if _, err := GetVolume(context.Background(), hc, svc); err == nil || !strings.Contains(err.Error(), "CurrentVolume") {
		t.Errorf("err = %v, want missing CurrentVolume", err)
	}
}

func TestCallEscapesArguments(t *testing.T) {
	t.Parallel()
	hc, svc := fakeService(t, ServiceAVTransport, "X", "<A>a&amp;b&lt;c</A>", "soap_zone_GetTransportInfo.xml", http.StatusOK)
	if _, err := Call(context.Background(), hc, svc, "X", Arg{Name: "A", Value: "a&b<c"}); err != nil {
		t.Fatal(err)
	}
}
