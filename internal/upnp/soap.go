// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ

package upnp

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Arg is one SOAP input argument. Arguments are sent in slice order, which
// must match the order in the service's SCPD.
type Arg struct {
	Name  string
	Value string
}

// Error is a UPnP error returned in a SOAP fault, e.g. 401 "Invalid Action".
type Error struct {
	Code        int
	Description string
}

func (e *Error) Error() string {
	return fmt.Sprintf("upnp error %d: %s", e.Code, e.Description)
}

// Call invokes action on the service and returns its output arguments by
// name. A SOAP fault is returned as *Error.
func Call(ctx context.Context, hc *http.Client, svc Service, action string, args ...Arg) (map[string]string, error) {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="utf-8"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
		`<s:Body><u:` + action + ` xmlns:u="` + svc.Type + `">`)
	for _, a := range args {
		buf.WriteString("<" + a.Name + ">")
		if err := xml.EscapeText(&buf, []byte(a.Value)); err != nil {
			return nil, err
		}
		buf.WriteString("</" + a.Name + ">")
	}
	buf.WriteString(`</u:` + action + `></s:Body></s:Envelope>`)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, svc.ControlURL, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPACTION", `"`+svc.Type+"#"+action+`"`)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	out, err := parseResponse(body)
	if err != nil {
		var upnpErr *Error
		if errors.As(err, &upnpErr) || resp.StatusCode == http.StatusOK {
			return nil, err
		}
		return nil, fmt.Errorf("upnp: %s %s: %s", svc.Type, action, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upnp: %s %s: %s", svc.Type, action, resp.Status)
	}
	return out, nil
}

// parseResponse returns the children of the first element inside the SOAP
// Body as name → text, or *Error when that element is a Fault.
func parseResponse(body []byte) (map[string]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	if err := skipTo(dec, "Body"); err != nil {
		return nil, err
	}
	start, err := nextStart(dec)
	if err != nil {
		return nil, err
	}
	if start.Name.Local == "Fault" {
		var f struct {
			Code        int    `xml:"detail>UPnPError>errorCode"`
			Description string `xml:"detail>UPnPError>errorDescription"`
			FaultString string `xml:"faultstring"`
		}
		if err := dec.DecodeElement(&f, &start); err != nil {
			return nil, fmt.Errorf("upnp: fault: %w", err)
		}
		if f.Description == "" {
			f.Description = f.FaultString
		}
		return nil, &Error{Code: f.Code, Description: f.Description}
	}
	var resp struct {
		Args []struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
		} `xml:",any"`
	}
	if err := dec.DecodeElement(&resp, &start); err != nil {
		return nil, fmt.Errorf("upnp: %s: %w", start.Name.Local, err)
	}
	out := make(map[string]string, len(resp.Args))
	for _, a := range resp.Args {
		out[a.XMLName.Local] = a.Value
	}
	return out, nil
}

func skipTo(dec *xml.Decoder, local string) error {
	for {
		start, err := nextStart(dec)
		if err != nil {
			return err
		}
		if start.Name.Local == local {
			return nil
		}
	}
}

func nextStart(dec *xml.Decoder) (xml.StartElement, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return xml.StartElement{}, errors.New("upnp: malformed SOAP envelope")
			}
			return xml.StartElement{}, err
		}
		if s, ok := tok.(xml.StartElement); ok {
			return s, nil
		}
	}
}

// requireArgs fails when out lacks any of names, so a device that answers
// with a different argument set is reported instead of read as zero values.
func requireArgs(action string, out map[string]string, names ...string) error {
	var missing []string
	for _, n := range names {
		if _, ok := out[n]; !ok {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("upnp: %s response lacks %s", action, strings.Join(missing, ", "))
	}
	return nil
}
