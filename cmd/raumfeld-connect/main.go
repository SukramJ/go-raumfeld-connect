// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command raumfeld-connect bridges Teufel Raumfeld multiroom speakers to
// other ecosystems. It talks to the speakers locally — over UPnP AV and the
// Raumfeld host's HTTP API — and has no cloud dependency.
//
// The bridge itself is not implemented yet. With --host the binary reads
// the zone configuration and every zone renderer's state, prints it, and
// exits; it changes nothing on the devices.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/SukramJ/go-raumfeld-connect/internal/host"
	"github.com/SukramJ/go-raumfeld-connect/internal/upnp"
	"github.com/SukramJ/go-raumfeld-connect/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print build info and exit")
	hostAddr := flag.String("host", "", "Raumfeld host address (IP or name); read zones and renderer state, print, exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.String())
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	logger.Info("raumfeld-connect.boot", slog.String("build", version.String()))

	if *hostAddr == "" {
		logger.Warn("raumfeld-connect.not_implemented")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := inspect(ctx, *hostAddr)
	cancel()
	if err != nil {
		logger.Error("raumfeld-connect.inspect", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

func inspect(ctx context.Context, addr string) error {
	hc := &http.Client{Timeout: 5 * time.Second}
	hostURL := "http://" + net.JoinHostPort(addr, strconv.Itoa(host.DefaultPort))
	c, err := host.New(hostURL, hc)
	if err != nil {
		return err
	}
	zc, err := c.GetZones(ctx)
	if err != nil {
		return err
	}
	dl, err := c.ListDevices(ctx)
	if err != nil {
		return err
	}
	locations := make(map[string]string, len(dl.Devices))
	for _, d := range dl.Devices {
		locations[d.UDN] = d.Location
	}

	fmt.Printf("host %s  rooms=%d spotifyMode=%s updateID=%s\n", hostURL, zc.NumRooms, zc.SpotifyMode, zc.UpdateID)
	for _, z := range zc.Zones {
		fmt.Printf("zone %s\n", z.UDN)
		printRooms(z.Rooms)
		loc, ok := locations[z.UDN]
		if !ok {
			fmt.Println("  renderer: not in listDevices")
			continue
		}
		printRenderer(ctx, hc, loc)
	}
	if len(zc.UnassignedRooms) > 0 {
		fmt.Println("unassigned")
		printRooms(zc.UnassignedRooms)
	}
	return nil
}

func printRooms(rooms []host.Room) {
	for _, r := range rooms {
		fmt.Printf("  room %q power=%s\n", r.Name, r.PowerState)
	}
}

func printRenderer(ctx context.Context, hc *http.Client, location string) {
	dev, err := upnp.FetchDevice(ctx, hc, location)
	if err != nil {
		fmt.Printf("  renderer: %v\n", err)
		return
	}
	fmt.Printf("  renderer %q (%s)\n", dev.FriendlyName, dev.ModelDescription)
	if rc, ok := dev.Service(upnp.ServiceRenderingControl); ok {
		vol, verr := upnp.GetVolume(ctx, hc, rc)
		mute, merr := upnp.GetMute(ctx, hc, rc)
		fmt.Printf("    volume=%s mute=%s\n", valueOr(vol, verr), valueOr(mute, merr))
	}
	if av, ok := dev.Service(upnp.ServiceAVTransport); ok {
		ti, err := upnp.GetTransportInfo(ctx, hc, av)
		fmt.Printf("    transport=%s\n", valueOr(ti.State+"/"+ti.Status, err))
		mi, err := upnp.GetMediaInfo(ctx, hc, av)
		fmt.Printf("    uri=%s\n", valueOr(mi.CurrentURI, err))
	}
}

func valueOr[T any](v T, err error) string {
	if err != nil {
		return "error(" + err.Error() + ")"
	}
	return fmt.Sprint(v)
}
