// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package upnp

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
)

// State-changing wrappers. Each one was measured on a Raumfeld zone renderer
// (2026-10-15) before it was written; argument names and order are the
// zone renderer SCPD's.

// MaxVolume is the upper bound of RenderingControl's Volume state variable
// (allowedValueRange 0..100, step 1, in the zone renderer's SCPD).
const MaxVolume = 100

// SetVolume sets RenderingControl's Master volume.
func SetVolume(ctx context.Context, hc *http.Client, svc Service, volume int) error {
	if volume < 0 || volume > MaxVolume {
		return fmt.Errorf("upnp: SetVolume: %d outside 0..%d", volume, MaxVolume)
	}
	_, err := Call(ctx, hc, svc, "SetVolume", instance0, master,
		Arg{Name: "DesiredVolume", Value: strconv.Itoa(volume)})
	return err
}

// SetAVTransportURI sets the URI the renderer plays next. An empty
// metadata string is accepted; the renderer then builds its own metadata.
// The renderer's host starts fetching uri immediately, and an unreachable
// uri is not reported: the call succeeds and the transport stays
// TRANSITIONING.
func SetAVTransportURI(ctx context.Context, hc *http.Client, svc Service, uri, metadata string) error {
	_, err := Call(ctx, hc, svc, "SetAVTransportURI", instance0,
		Arg{Name: "CurrentURI", Value: uri},
		Arg{Name: "CurrentURIMetaData", Value: metadata})
	return err
}

// Play starts playback at normal speed ("1" is the only allowed Speed).
func Play(ctx context.Context, hc *http.Client, svc Service) error {
	_, err := Call(ctx, hc, svc, "Play", instance0, Arg{Name: "Speed", Value: "1"})
	return err
}

// Stop stops playback.
func Stop(ctx context.Context, hc *http.Client, svc Service) error {
	_, err := Call(ctx, hc, svc, "Stop", instance0)
	return err
}

// LeaveStandby wakes the room with the given UDN (e.g.
// "uuid:5d4abab7-..."); powerState in getZones turns ACTIVE.
func LeaveStandby(ctx context.Context, hc *http.Client, svc Service, roomUDN string) error {
	_, err := Call(ctx, hc, svc, "LeaveStandby", instance0, Arg{Name: "Room", Value: roomUDN})
	return err
}

// EnterAutomaticStandby returns the room with the given UDN to automatic
// standby; getZones showed AUTOMATIC_STANDBY about ten seconds later.
func EnterAutomaticStandby(ctx context.Context, hc *http.Client, svc Service, roomUDN string) error {
	_, err := Call(ctx, hc, svc, "EnterAutomaticStandby", instance0, Arg{Name: "Room", Value: roomUDN})
	return err
}

// EnterManualStandby puts the room with the given UDN into manual standby.
// Its SCPD takes InstanceID and Room, like EnterAutomaticStandby; measured
// 2026-10-15: a zone in MANUAL_STANDBY answers Play with transport status
// "Please turn on a device." until LeaveStandby.
func EnterManualStandby(ctx context.Context, hc *http.Client, svc Service, roomUDN string) error {
	_, err := Call(ctx, hc, svc, "EnterManualStandby", instance0, Arg{Name: "Room", Value: roomUDN})
	return err
}
