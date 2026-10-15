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

	"github.com/SukramJ/go-raumfeld-connect/internal/airplay"
	"github.com/SukramJ/go-raumfeld-connect/internal/host"
	"github.com/SukramJ/go-raumfeld-connect/internal/stream"
	"github.com/SukramJ/go-raumfeld-connect/internal/upnp"
	"github.com/SukramJ/go-raumfeld-connect/internal/zone"
)

type playOptions struct {
	host      string
	room      string
	duration  time.Duration // 0 = until ctx is cancelled
	port      int
	advertise string
	volume    int // -1 = leave unchanged

	volumes   <-chan float64 // AirPlay volume changes to apply to the zone, may be nil
	maxVolume int            // zone volume at full AirPlay volume
}

// play streams src to the room's zone, for o.duration or until ctx is
// cancelled, and restores the zone's URI, metadata, volume and standby
// state afterwards.
func play(ctx context.Context, log *slog.Logger, o playOptions, src stream.Source) error {
	hc := &http.Client{Timeout: 5 * time.Second}
	c, err := host.New("http://"+net.JoinHostPort(o.host, strconv.Itoa(host.DefaultPort)), hc)
	if err != nil {
		return err
	}
	z, err := zone.ForRoom(ctx, c, hc, o.room)
	if err != nil {
		return err
	}
	addr := o.advertise
	if addr == "" {
		if addr, err = localAddrTowards(ctx, o.host); err != nil {
			return err
		}
	}

	orig, err := upnp.GetMediaInfo(ctx, hc, z.Transport)
	if err != nil {
		return err
	}
	origVol, err := upnp.GetVolume(ctx, hc, z.Rendering)
	if err != nil {
		return err
	}
	origPower := z.Room.PowerState
	log.Info("play.orig", slog.String("uri", orig.CurrentURI), slog.Int("volume", origVol), slog.String("power", origPower))

	srv := &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(o.port)),
		Handler:           stream.NewHandler(src, log),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("play.listen", slog.String("err", err.Error()))
		}
	}()
	defer func() { _ = srv.Close() }()

	defer func() {
		// Restore even when ctx is already cancelled.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		errs := []error{
			upnp.SetAVTransportURI(rctx, hc, z.Transport, orig.CurrentURI, orig.CurrentURIMetaData),
			upnp.Stop(rctx, hc, z.Transport),
			upnp.SetVolume(rctx, hc, z.Rendering, origVol),
		}
		if origPower == "AUTOMATIC_STANDBY" {
			errs = append(errs, upnp.EnterAutomaticStandby(rctx, hc, z.Transport, z.Room.UDN))
		}
		log.Info("play.restored", slog.Any("err", errors.Join(errs...)))
	}()

	if o.volume >= 0 {
		if err := upnp.SetVolume(ctx, hc, z.Rendering, o.volume); err != nil {
			return err
		}
	}
	url := fmt.Sprintf("http://%s/stream.wav", net.JoinHostPort(addr, strconv.Itoa(o.port)))
	start := time.Now()
	if err := upnp.SetAVTransportURI(ctx, hc, z.Transport, url, ""); err != nil {
		return err
	}
	if err := upnp.Play(ctx, hc, z.Transport); err != nil {
		return err
	}
	log.Info("play.started", slog.String("room", o.room), slog.String("url", url))

	var last string
	deadline := start.Add(o.duration + 2*time.Second)
	for o.duration == 0 || time.Now().Before(deadline) {
		ti, err := upnp.GetTransportInfo(ctx, hc, z.Transport)
		if err != nil {
			if o.duration == 0 && ctx.Err() != nil {
				return nil
			}
			return err
		}
		if ti.State != last {
			log.Info("play.state", slog.String("state", ti.State), slog.Duration("after", time.Since(start).Round(10*time.Millisecond)))
			last = ti.State
		}
		select {
		case v := <-o.volumes:
			zv := airplay.ZoneVolume(v, o.maxVolume)
			err := upnp.SetVolume(ctx, hc, z.Rendering, zv)
			log.Info("play.volume", slog.Float64("airplay", v), slog.Int("zone", zv), slog.Any("err", err))
		case <-ctx.Done():
			if o.duration == 0 {
				return nil
			}
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return nil
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
