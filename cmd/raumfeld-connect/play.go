// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/SukramJ/go-raumfeld-connect/internal/host"
	"github.com/SukramJ/go-raumfeld-connect/internal/stream"
	"github.com/SukramJ/go-raumfeld-connect/internal/upnp"
	"github.com/SukramJ/go-raumfeld-connect/internal/zone"
)

type playOptions struct {
	host      string
	room      string
	duration  time.Duration
	port      int
	advertise string
	volume    int // -1 = leave unchanged
}

// target is a room's zone and the address its renderer fetches our stream
// from.
type target struct {
	hc   *http.Client
	host *host.Client
	zone *zone.Zone
	url  string
}

// resolve looks up the room's zone now; zone membership can change while
// the bridge runs.
func resolve(ctx context.Context, o playOptions) (*target, error) {
	hc := &http.Client{Timeout: 5 * time.Second}
	c, err := host.New("http://"+net.JoinHostPort(o.host, strconv.Itoa(host.DefaultPort)), hc)
	if err != nil {
		return nil, err
	}
	z, err := zone.ForRoom(ctx, c, hc, o.room)
	if err != nil {
		return nil, err
	}
	addr := o.advertise
	if addr == "" {
		if addr, err = localAddrTowards(ctx, o.host); err != nil {
			return nil, err
		}
	}
	url := fmt.Sprintf("http://%s/stream.wav", net.JoinHostPort(addr, strconv.Itoa(o.port)))
	return &target{hc: hc, host: c, zone: z, url: url}, nil
}

// saved is a zone's state before we took it over.
type saved struct {
	media  upnp.MediaInfo
	volume int
	power  string
}

func (t *target) save(ctx context.Context, log *slog.Logger) (*saved, error) {
	media, err := upnp.GetMediaInfo(ctx, t.hc, t.zone.Transport)
	if err != nil {
		return nil, err
	}
	vol, err := upnp.GetVolume(ctx, t.hc, t.zone.Rendering)
	if err != nil {
		return nil, err
	}
	s := &saved{media: media, volume: vol, power: t.zone.Room.PowerState}
	log.Info("play.orig", slog.String("uri", s.media.CurrentURI), slog.Int("volume", s.volume), slog.String("power", s.power))
	return s, nil
}

// restore puts back URI, metadata, volume and standby, even when ctx is
// already cancelled.
func (t *target) restore(ctx context.Context, log *slog.Logger, s *saved) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	errs := []error{
		upnp.SetAVTransportURI(rctx, t.hc, t.zone.Transport, s.media.CurrentURI, s.media.CurrentURIMetaData),
		upnp.Stop(rctx, t.hc, t.zone.Transport),
		upnp.SetVolume(rctx, t.hc, t.zone.Rendering, s.volume),
	}
	switch s.power {
	case "AUTOMATIC_STANDBY":
		errs = append(errs, upnp.EnterAutomaticStandby(rctx, t.hc, t.zone.Transport, t.zone.Room.UDN))
	case "MANUAL_STANDBY":
		errs = append(errs, upnp.EnterManualStandby(rctx, t.hc, t.zone.Transport, t.zone.Room.UDN))
	}
	log.Info("play.restored", slog.Any("err", errors.Join(errs...)))
}

// start points the zone at our stream, at volume unless it is negative.
// A room in manual standby is woken first and must reach ACTIVE before
// Play: Play alone, or right after LeaveStandby, leaves the transport
// STOPPED; ACTIVE came ~0.9 s after LeaveStandby (measured 2026-10-15).
// Automatic standby wakes on Play.
func (t *target) start(ctx context.Context, log *slog.Logger, volume int) error {
	if t.zone.Room.PowerState == "MANUAL_STANDBY" {
		if err := t.wake(ctx, log); err != nil {
			return err
		}
	}
	if volume >= 0 {
		if err := upnp.SetVolume(ctx, t.hc, t.zone.Rendering, volume); err != nil {
			return err
		}
	}
	if err := upnp.SetAVTransportURI(ctx, t.hc, t.zone.Transport, t.url, ""); err != nil {
		return err
	}
	if err := upnp.Play(ctx, t.hc, t.zone.Transport); err != nil {
		return err
	}
	log.Info("play.started", slog.String("room", t.zone.Room.Name), slog.String("url", t.url))
	return nil
}

// wake leaves manual standby and waits until getZones reports the room
// ACTIVE.
func (t *target) wake(ctx context.Context, log *slog.Logger) error {
	start := time.Now()
	if err := upnp.LeaveStandby(ctx, t.hc, t.zone.Transport, t.zone.Room.UDN); err != nil {
		return err
	}
	for time.Since(start) < 5*time.Second {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		zc, err := t.host.GetZones(ctx)
		if err != nil {
			return err
		}
		for _, z := range zc.Zones {
			for _, r := range z.Rooms {
				if r.UDN == t.zone.Room.UDN && r.PowerState == "ACTIVE" {
					log.Info("play.awake", slog.Duration("after", time.Since(start).Round(10*time.Millisecond)))
					return nil
				}
			}
		}
	}
	return fmt.Errorf("room %q not ACTIVE 5 s after LeaveStandby", t.zone.Room.Name)
}

// watch logs transport state changes until deadline.
func (t *target) watch(ctx context.Context, log *slog.Logger, start, deadline time.Time) error {
	var last string
	for time.Now().Before(deadline) {
		ti, err := upnp.GetTransportInfo(ctx, t.hc, t.zone.Transport)
		if err != nil {
			return err
		}
		if ti.State != last {
			log.Info("play.state", slog.String("state", ti.State), slog.Duration("after", time.Since(start).Round(10*time.Millisecond)))
			last = ti.State
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return nil
}

// serve runs the stream server for src until the returned stop is called.
func serve(log *slog.Logger, port int, src stream.Source) (stop func()) {
	srv := &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(port)),
		Handler:           stream.NewHandler(src, log),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("play.listen", slog.String("err", err.Error()))
		}
	}()
	return func() { _ = srv.Close() }
}

// playTone streams a paced test tone to the room's zone and restores the
// zone afterwards.
func playTone(ctx context.Context, log *slog.Logger, o playOptions) error {
	t, err := resolve(ctx, o)
	if err != nil {
		return err
	}
	s, err := t.save(ctx, log)
	if err != nil {
		return err
	}
	stop := serve(log, o.port, stream.NewTone(440, 0.1, o.duration))
	defer stop()
	defer t.restore(ctx, log, s)

	start := time.Now()
	if err := t.start(ctx, log, o.volume); err != nil {
		return err
	}
	return t.watch(ctx, log, start, start.Add(o.duration+2*time.Second))
}

// localAddrTowards returns this machine's address on the route to target,
// the address a renderer reaching back to us must use.
func localAddrTowards(ctx context.Context, target string) (string, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", net.JoinHostPort(target, strconv.Itoa(host.DefaultPort)))
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	ua, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", fmt.Errorf("local address %v", conn.LocalAddr())
	}
	return ua.IP.String(), nil
}
