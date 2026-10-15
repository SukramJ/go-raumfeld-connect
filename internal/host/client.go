// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package host is a read-only client for the Raumfeld host's HTTP API
// (tcp/47365 on the device acting as host).
//
// Measured behaviour this client relies on: a request without a session
// prefix answers 307 to /<session-uuid>/<path>; calls under that prefix
// answer 200 with XML and an updateID header.
package host

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// DefaultPort is the host API port, measured on a Cinebar Lux host.
const DefaultPort = 47365

const maxBody = 1 << 20

// Client talks to one Raumfeld host.
type Client struct {
	base *url.URL
	hc   *http.Client

	mu      sync.Mutex
	session string
}

// New returns a client for the host API at baseURL (e.g.
// "http://192.0.2.65:47365"). Redirects are handled by the client itself
// to learn the session, so hc's CheckRedirect is not used.
func New(baseURL string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("host: base url %q: %w", baseURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("host: base url %q lacks scheme or host", baseURL)
	}
	noFollow := *hc
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: u, hc: &noFollow}, nil
}

// get fetches path under the current session. A 307 carries the session
// the host assigned; it is adopted and the request is repeated once.
func (c *Client) get(ctx context.Context, path string) (body []byte, updateID string, err error) {
	for range 2 {
		c.mu.Lock()
		session := c.session
		c.mu.Unlock()

		ref := path
		if session != "" {
			ref = "/" + session + "/" + path
		}
		target, err := c.base.Parse(ref)
		if err != nil {
			return nil, "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), http.NoBody)
		if err != nil {
			return nil, "", err
		}
		resp, err := c.hc.Do(req)
		if err != nil {
			return nil, "", err
		}
		body, err = io.ReadAll(io.LimitReader(resp.Body, maxBody))
		_ = resp.Body.Close()
		if err != nil {
			return nil, "", err
		}
		switch resp.StatusCode {
		case http.StatusOK:
			return body, resp.Header.Get("updateID"), nil
		case http.StatusTemporaryRedirect:
			s, err := sessionFromLocation(target, resp.Header.Get("Location"))
			if err != nil {
				return nil, "", err
			}
			c.mu.Lock()
			c.session = s
			c.mu.Unlock()
		default:
			return nil, "", fmt.Errorf("host: GET %s: %s", target, resp.Status)
		}
	}
	return nil, "", fmt.Errorf("host: GET %s: redirected twice", path)
}

// sessionFromLocation returns the first path segment of a redirect target.
func sessionFromLocation(from *url.URL, location string) (string, error) {
	if location == "" {
		return "", errors.New("host: 307 without Location")
	}
	u, err := from.Parse(location)
	if err != nil {
		return "", fmt.Errorf("host: Location %q: %w", location, err)
	}
	seg, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if seg == "" {
		return "", fmt.Errorf("host: Location %q carries no session", location)
	}
	return seg, nil
}

// ZoneConfig is the getZones document.
type ZoneConfig struct {
	UpdateID        string `xml:"-"`
	NumRooms        int    `xml:"numRooms,attr"`
	SpotifyMode     string `xml:"spotifyMode,attr"`
	Zones           []Zone `xml:"zones>zone"`
	UnassignedRooms []Room `xml:"unassignedRooms>room"`
}

// Zone is a group of rooms played by one zone renderer, addressed by UDN.
type Zone struct {
	UDN   string `xml:"udn,attr"`
	Rooms []Room `xml:"room"`
}

// Room is a room with its renderers.
type Room struct {
	Name       string     `xml:"name,attr"`
	UDN        string     `xml:"udn,attr"`
	PowerState string     `xml:"powerState,attr"`
	Renderers  []Renderer `xml:"renderer"`
}

// Renderer is a room renderer. SpotifyConnect is empty unless the host
// reports it (measured value: "active").
type Renderer struct {
	UDN            string `xml:"udn,attr"`
	Name           string `xml:"name,attr"`
	SpotifyConnect string `xml:"spotifyConnect,attr"`
}

// GetZones fetches the zone configuration.
func (c *Client) GetZones(ctx context.Context) (*ZoneConfig, error) {
	body, updateID, err := c.get(ctx, "getZones")
	if err != nil {
		return nil, err
	}
	var zc ZoneConfig
	if err := xml.Unmarshal(body, &zc); err != nil {
		return nil, fmt.Errorf("host: getZones: %w", err)
	}
	zc.UpdateID = updateID
	return &zc, nil
}

// Device is one listDevices entry.
type Device struct {
	UDN      string `xml:"udn,attr"`
	Type     string `xml:"type,attr"`
	Location string `xml:"location,attr"`
	Name     string `xml:",chardata"`
}

// DeviceList is the listDevices document.
type DeviceList struct {
	UpdateID string   `xml:"-"`
	Devices  []Device `xml:"device"`
}

// ListDevices fetches every UPnP device the host knows.
func (c *Client) ListDevices(ctx context.Context) (*DeviceList, error) {
	body, updateID, err := c.get(ctx, "listDevices")
	if err != nil {
		return nil, err
	}
	var dl DeviceList
	if err := xml.Unmarshal(body, &dl); err != nil {
		return nil, fmt.Errorf("host: listDevices: %w", err)
	}
	dl.UpdateID = updateID
	return &dl, nil
}
