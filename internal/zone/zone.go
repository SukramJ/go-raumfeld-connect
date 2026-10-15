// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package zone resolves a Raumfeld room to the zone renderer that plays it.
package zone

import (
	"context"
	"fmt"
	"net/http"

	"github.com/SukramJ/go-raumfeld-connect/internal/host"
	"github.com/SukramJ/go-raumfeld-connect/internal/upnp"
)

// Zone is a room's zone renderer with the services the bridge uses.
type Zone struct {
	UDN       string
	Room      host.Room
	Renderer  *upnp.Device
	Transport upnp.Service
	Rendering upnp.Service
}

// NoZoneError reports a room that exists but is in no zone, so it has no
// zone renderer.
type NoZoneError struct {
	Room host.Room
}

func (e *NoZoneError) Error() string { return fmt.Sprintf("zone: room %q is in no zone", e.Room.Name) }

// ForRoom finds the zone containing the room named name. A room outside
// every zone is reported as *NoZoneError.
func ForRoom(ctx context.Context, c *host.Client, hc *http.Client, name string) (*Zone, error) {
	zc, err := c.GetZones(ctx)
	if err != nil {
		return nil, err
	}
	var z *Zone
	for _, hz := range zc.Zones {
		for _, r := range hz.Rooms {
			if r.Name == name {
				z = &Zone{UDN: hz.UDN, Room: r}
			}
		}
	}
	if z == nil {
		for _, r := range zc.UnassignedRooms {
			if r.Name == name {
				return nil, &NoZoneError{Room: r}
			}
		}
		return nil, fmt.Errorf("zone: no room %q", name)
	}
	dl, err := c.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range dl.Devices {
		if d.UDN != z.UDN {
			continue
		}
		if z.Renderer, err = upnp.FetchDevice(ctx, hc, d.Location); err != nil {
			return nil, err
		}
		var ok1, ok2 bool
		z.Transport, ok1 = z.Renderer.Service(upnp.ServiceAVTransport)
		z.Rendering, ok2 = z.Renderer.Service(upnp.ServiceRenderingControl)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("zone: renderer %s lacks AVTransport or RenderingControl", z.UDN)
		}
		return z, nil
	}
	return nil, fmt.Errorf("zone: renderer %s not in listDevices", z.UDN)
}
