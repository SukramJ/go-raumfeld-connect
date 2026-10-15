// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package upnp is a minimal UPnP client: it fetches device descriptions and
// invokes SOAP actions. It knows nothing Raumfeld-specific beyond what the
// devices themselves advertise.
package upnp

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Service types used by the read-only wrappers.
const (
	ServiceAVTransport      = "urn:schemas-upnp-org:service:AVTransport:1"
	ServiceRenderingControl = "urn:schemas-upnp-org:service:RenderingControl:1"
)

// maxBody bounds every response body read from a device.
const maxBody = 1 << 20

// Device is the part of a UPnP device description this client uses.
type Device struct {
	Location         string
	UDN              string
	DeviceType       string
	FriendlyName     string
	ModelName        string
	ModelDescription string
	Services         []Service
}

// Service is one entry of a device's serviceList. The URLs are absolute,
// resolved against the description's location.
type Service struct {
	Type        string
	ID          string
	SCPDURL     string
	ControlURL  string
	EventSubURL string
}

// Service returns the service with the given type, or false.
func (d *Device) Service(serviceType string) (Service, bool) {
	for _, s := range d.Services {
		if s.Type == serviceType {
			return s, true
		}
	}
	return Service{}, false
}

type xmlRoot struct {
	Device struct {
		DeviceType       string `xml:"deviceType"`
		FriendlyName     string `xml:"friendlyName"`
		ModelName        string `xml:"modelName"`
		ModelDescription string `xml:"modelDescription"`
		UDN              string `xml:"UDN"`
		Services         []struct {
			Type        string `xml:"serviceType"`
			ID          string `xml:"serviceId"`
			SCPDURL     string `xml:"SCPDURL"`
			ControlURL  string `xml:"controlURL"`
			EventSubURL string `xml:"eventSubURL"`
		} `xml:"serviceList>service"`
	} `xml:"device"`
}

// FetchDevice downloads and parses the device description at location.
func FetchDevice(ctx context.Context, hc *http.Client, location string) (*Device, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upnp: GET %s: %s", location, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	return ParseDevice(location, body)
}

// ParseDevice parses a device description. Relative service URLs are
// resolved against location: the measured descriptions carry no URLBase and
// mix "/RenderingService.xml" with "TransportService.xml".
func ParseDevice(location string, body []byte) (*Device, error) {
	base, err := url.Parse(location)
	if err != nil {
		return nil, fmt.Errorf("upnp: location %q: %w", location, err)
	}
	var root xmlRoot
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("upnp: description %s: %w", location, err)
	}
	d := root.Device
	dev := &Device{
		Location:         location,
		UDN:              d.UDN,
		DeviceType:       d.DeviceType,
		FriendlyName:     d.FriendlyName,
		ModelName:        d.ModelName,
		ModelDescription: d.ModelDescription,
	}
	for _, s := range d.Services {
		svc := Service{Type: s.Type, ID: s.ID}
		for _, p := range []struct {
			dst *string
			ref string
		}{{&svc.SCPDURL, s.SCPDURL}, {&svc.ControlURL, s.ControlURL}, {&svc.EventSubURL, s.EventSubURL}} {
			if p.ref == "" {
				continue
			}
			u, err := base.Parse(p.ref)
			if err != nil {
				return nil, fmt.Errorf("upnp: service %s url %q: %w", s.Type, p.ref, err)
			}
			*p.dst = u.String()
		}
		dev.Services = append(dev.Services, svc)
	}
	return dev, nil
}
