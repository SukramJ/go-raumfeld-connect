// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command raumfeld-connect bridges Teufel Raumfeld multiroom speakers to
// other ecosystems. It talks to the speakers locally — over UPnP AV and the
// Raumfeld host's HTTP API — and has no cloud dependency.
//
// The bridge itself is not implemented yet. With --host the binary reads
// the zone configuration and every zone renderer's state, prints it, and
// exits; it changes nothing on the devices. With --play-tone it streams a
// paced test tone to one room's zone and restores the zone afterwards.
// With --airplay it runs shairport-sync as an AirPlay 2 receiver named
// after the room and streams what it receives to the room's zone until
// interrupted, then restores the zone.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/SukramJ/go-raumfeld-connect/internal/airplay"
	"github.com/SukramJ/go-raumfeld-connect/internal/host"
	"github.com/SukramJ/go-raumfeld-connect/internal/stream"
	"github.com/SukramJ/go-raumfeld-connect/internal/upnp"
	"github.com/SukramJ/go-raumfeld-connect/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print build info and exit")
	hostAddr := flag.String("host", "", "Raumfeld host address (IP or name); alone: read zones and renderer state, print, exit")
	room := flag.String("play-tone", "", "with --host: stream a test tone to this room's zone, then restore the zone")
	toneFor := flag.Duration("tone-duration", 10*time.Second, "test tone length")
	port := flag.Int("stream-port", 59317, "TCP port the renderer fetches the stream from")
	advertise := flag.String("advertise", "", "address the renderer reaches this machine on (default: route towards --host)")
	volume := flag.Int("volume", -1, "zone volume while streaming, 0..100 (default: unchanged)")
	airplayRoom := flag.String("airplay", "", "with --host: receive AirPlay 2 for this room's zone until interrupted, then restore the zone")
	shairport := flag.String("shairport-sync", "shairport-sync", "shairport-sync binary (built with --with-airplay-2 --with-stdout)")
	airplayPort := flag.Int("airplay-port", 7000, "RTSP port of the AirPlay receiver")
	maxVolume := flag.Int("max-volume", 100, "zone volume at full AirPlay volume, 0..100")
	volumeHook := flag.String("airplay-volume-hook", "", "internal: called by shairport-sync to pass a volume change to this socket")
	debug := flag.Bool("debug", false, "log at debug level, including shairport-sync's own log")
	airplayBuffer := flag.Duration("airplay-buffer", 500*time.Millisecond, "audio held at most between shairport-sync and the zone")
	flag.Parse()

	if *volumeHook != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := airplay.SendVolume(ctx, *volumeHook, flag.Arg(0))
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, "airplay volume hook:", err)
			os.Exit(1)
		}
		return
	}
	if *showVersion {
		fmt.Println(version.String())
		return
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	logger.Info("raumfeld-connect.boot", slog.String("build", version.String()))

	if *hostAddr == "" {
		logger.Warn("raumfeld-connect.not_implemented")
		return
	}
	var err error
	if *room != "" && *airplayRoom != "" {
		logger.Error("raumfeld-connect.flags", slog.String("err", "--play-tone and --airplay exclude each other"))
		os.Exit(2)
	}
	switch {
	case *airplayRoom != "":
		err = runAirPlay(logger, airplay.Config{
			Bin: *shairport, Name: *airplayRoom, Port: *airplayPort, Buffer: *airplayBuffer,
		}, playOptions{
			host: *hostAddr, room: *airplayRoom, port: *port, advertise: *advertise, volume: *volume,
		}, *maxVolume)
	case *room != "":
		ctx, cancel := context.WithTimeout(context.Background(), *toneFor+30*time.Second)
		err = play(ctx, logger, playOptions{
			host: *hostAddr, room: *room, duration: *toneFor,
			port: *port, advertise: *advertise, volume: *volume,
		}, stream.NewTone(440, 0.1, *toneFor))
		cancel()
	default:
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = inspect(ctx, *hostAddr)
		cancel()
	}
	if err != nil {
		logger.Error("raumfeld-connect.inspect", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

// runAirPlay runs an AirPlay receiver and streams it to the room's zone
// until SIGINT or SIGTERM.
func runAirPlay(log *slog.Logger, ac airplay.Config, o playOptions, maxVolume int) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if exe, err := os.Executable(); err == nil {
		ac.Hook = strconv.Quote(exe) + " -airplay-volume-hook"
	} else {
		log.Warn("airplay.no_volume_hook", slog.String("err", err.Error()))
	}
	rcv, err := airplay.Start(ctx, ac, log)
	if err != nil {
		return err
	}
	o.volumes = rcv.Volume
	o.maxVolume = maxVolume
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s := rcv.Source.Stats()
				log.Debug("airplay.stats", slog.Int64("in", s.In), slog.Int64("out", s.Out),
					slog.Int64("silence", s.Silence), slog.Int64("dropped", s.Dropped), slog.Int("peak", int(s.Peak)))
			}
		}
	}()
	err = play(ctx, log, o, rcv.Source)
	stop()
	return errors.Join(err, rcv.Wait())
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
