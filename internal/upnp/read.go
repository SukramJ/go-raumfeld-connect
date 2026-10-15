// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package upnp

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
)

// Read-only wrappers. Argument names and their order are taken from the
// SCPDs the Raumfeld renderers serve (zone and room renderer agree on these
// actions, except GetPositionInfo — see PositionInfo).

var (
	instance0 = Arg{Name: "InstanceID", Value: "0"}
	master    = Arg{Name: "Channel", Value: "Master"}
)

// GetVolume returns RenderingControl's CurrentVolume for the Master channel.
func GetVolume(ctx context.Context, hc *http.Client, svc Service) (int, error) {
	out, err := Call(ctx, hc, svc, "GetVolume", instance0, master)
	if err != nil {
		return 0, err
	}
	if err := requireArgs("GetVolume", out, "CurrentVolume"); err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(out["CurrentVolume"])
	if err != nil {
		return 0, fmt.Errorf("upnp: GetVolume: %w", err)
	}
	return v, nil
}

// GetMute returns RenderingControl's CurrentMute for the Master channel.
// Only "0" and "1" are accepted: "0" is the only value measured so far,
// anything else is reported rather than guessed.
func GetMute(ctx context.Context, hc *http.Client, svc Service) (bool, error) {
	out, err := Call(ctx, hc, svc, "GetMute", instance0, master)
	if err != nil {
		return false, err
	}
	if err := requireArgs("GetMute", out, "CurrentMute"); err != nil {
		return false, err
	}
	switch v := out["CurrentMute"]; v {
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, fmt.Errorf("upnp: GetMute: unexpected CurrentMute %q", v)
	}
}

// TransportInfo is AVTransport's GetTransportInfo result, verbatim.
type TransportInfo struct {
	State  string // CurrentTransportState, e.g. "STOPPED"
	Status string // CurrentTransportStatus, e.g. "OK"
	Speed  string // CurrentSpeed
}

// GetTransportInfo calls AVTransport GetTransportInfo.
func GetTransportInfo(ctx context.Context, hc *http.Client, svc Service) (TransportInfo, error) {
	out, err := Call(ctx, hc, svc, "GetTransportInfo", instance0)
	if err != nil {
		return TransportInfo{}, err
	}
	if err := requireArgs("GetTransportInfo", out, "CurrentTransportState", "CurrentTransportStatus", "CurrentSpeed"); err != nil {
		return TransportInfo{}, err
	}
	return TransportInfo{
		State:  out["CurrentTransportState"],
		Status: out["CurrentTransportStatus"],
		Speed:  out["CurrentSpeed"],
	}, nil
}

// PositionInfo is AVTransport's GetPositionInfo result, verbatim. The zone
// renderer returns the full UPnP argument set; the room renderer's SCPD
// declares only TrackDuration and RelTime and answers "NOT_IMPLEMENTED" for
// both. Fields the device did not send are empty.
type PositionInfo struct {
	Track         string
	TrackDuration string
	TrackMetaData string
	TrackURI      string
	RelTime       string
	AbsTime       string
}

// GetPositionInfo calls AVTransport GetPositionInfo.
func GetPositionInfo(ctx context.Context, hc *http.Client, svc Service) (PositionInfo, error) {
	out, err := Call(ctx, hc, svc, "GetPositionInfo", instance0)
	if err != nil {
		return PositionInfo{}, err
	}
	if err := requireArgs("GetPositionInfo", out, "TrackDuration", "RelTime"); err != nil {
		return PositionInfo{}, err
	}
	return PositionInfo{
		Track:         out["Track"],
		TrackDuration: out["TrackDuration"],
		TrackMetaData: out["TrackMetaData"],
		TrackURI:      out["TrackURI"],
		RelTime:       out["RelTime"],
		AbsTime:       out["AbsTime"],
	}, nil
}

// MediaInfo is AVTransport's GetMediaInfo result, verbatim. Only the zone
// renderer's SCPD declares GetMediaInfo.
type MediaInfo struct {
	NrTracks           string
	MediaDuration      string
	CurrentURI         string
	CurrentURIMetaData string
}

// GetMediaInfo calls AVTransport GetMediaInfo.
func GetMediaInfo(ctx context.Context, hc *http.Client, svc Service) (MediaInfo, error) {
	out, err := Call(ctx, hc, svc, "GetMediaInfo", instance0)
	if err != nil {
		return MediaInfo{}, err
	}
	if err := requireArgs("GetMediaInfo", out, "NrTracks", "MediaDuration", "CurrentURI", "CurrentURIMetaData"); err != nil {
		return MediaInfo{}, err
	}
	return MediaInfo{
		NrTracks:           out["NrTracks"],
		MediaDuration:      out["MediaDuration"],
		CurrentURI:         out["CurrentURI"],
		CurrentURIMetaData: out["CurrentURIMetaData"],
	}, nil
}
