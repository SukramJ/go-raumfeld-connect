// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/SukramJ/go-raumfeld-connect/internal/airplay"
	"github.com/SukramJ/go-raumfeld-connect/internal/stream"
	"github.com/SukramJ/go-raumfeld-connect/internal/upnp"
)

// runAirPlay runs one AirPlay receiver per room until SIGINT or SIGTERM,
// on consecutive RTSP ports from ac.Port; the streams share one HTTP port
// under per-room paths. Each room's zone is taken over when its session
// becomes active and restored when it ends, so a zone is left alone while
// nobody plays to it. Rooms grouped in one zone share it: the first
// session plays, the others wait until it is released.
func runAirPlay(log *slog.Logger, rooms []string, ac airplay.Config, o playOptions, maxVolume int) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	ac.Hook = strconv.Quote(exe) + " -airplay-hook"

	owners := newZoneOwners()
	mux := http.NewServeMux()
	defer serve(log, o.port, mux)()
	var wg sync.WaitGroup
	errs := make([]error, len(rooms))
	for i, room := range rooms {
		c := ac
		c.Name, c.Port = room, ac.Port+i
		l := log.With(slog.String("room", room))
		rcv, err := airplay.Start(ctx, c, l)
		if err != nil {
			stop()
			wg.Wait()
			return errors.Join(err, errors.Join(errs...))
		}
		ro := o
		ro.room, ro.path = room, fmt.Sprintf("/airplay/%d.wav", i)
		mux.Handle(ro.path, stream.NewHandler(rcv.Source, l))
		wg.Go(func() { errs[i] = runBridge(ctx, l, rcv, ro, maxVolume, owners) })
	}
	wg.Wait()
	return errors.Join(errs...)
}

// runBridge follows one receiver's session events until ctx ends or
// shairport-sync exits.
func runBridge(ctx context.Context, log *slog.Logger, rcv *airplay.Receiver, o playOptions, maxVolume int, owners *zoneOwners) error {
	exited := make(chan error, 1)
	go func() { exited <- rcv.Wait() }()
	b := &bridge{log: log, o: o, volume: o.volume, owners: owners}
	go logStats(ctx, log, rcv, b.playing.Load)

	defer b.end(ctx, true)
	for {
		select {
		case <-ctx.Done():
			b.end(ctx, true)
			return <-exited
		case err := <-exited:
			return err
		case <-owners.released():
			if b.active && b.t == nil {
				b.takeOver(ctx) // a zone we wait for may be free now
			}
		case ev := <-rcv.Events:
			switch ev.Kind {
			case airplay.Active:
				b.active = true
				b.takeOver(ctx)
			case airplay.Inactive:
				b.active = false
				b.end(ctx, false)
			case airplay.Volume:
				b.setVolume(ctx, airplay.ZoneVolume(ev.Volume, maxVolume))
			}
		}
	}
}

// bridge is the zone side of one receiver.
type bridge struct {
	log    *slog.Logger
	o      playOptions
	volume int // zone volume to apply, -1 = leave unchanged
	owners *zoneOwners
	active bool // the AirPlay session is active

	t       *target // nil while the zone is not ours
	s       *saved
	playing atomic.Bool // t != nil, for the stats goroutine
}

func (b *bridge) takeOver(ctx context.Context) {
	if b.t != nil {
		return
	}
	t, err := resolve(ctx, b.log, b.o)
	if err != nil {
		b.log.Error("airplay.zone", slog.String("err", err.Error()))
		return
	}
	owner, ho, ok := b.owners.acquire(t.zone.UDN, b.o.room)
	if !ok {
		b.owners.wait(t.zone.UDN, b.o.room)
		b.log.Info("airplay.zone_busy", slog.String("zone", t.zone.UDN), slog.String("owner", owner))
		return
	}
	var s *saved
	if ho != nil {
		// Handed over by another room of this zone: keep the state saved
		// before the first takeover, which is what restore must put back.
		s = ho.s
		b.log.Info("airplay.zone_handed_over", slog.String("zone", t.zone.UDN))
	} else if s, err = t.save(ctx, b.log); err != nil {
		b.log.Error("airplay.zone", slog.String("err", err.Error()))
		b.owners.release(t.zone.UDN, b.o.room)
		return
	}
	start := time.Now()
	if err := t.start(ctx, b.log, b.volume); err != nil {
		b.log.Error("airplay.zone", slog.String("err", err.Error()))
		t.restore(ctx, b.log, s)
		b.owners.release(t.zone.UDN, b.o.room)
		return
	}
	b.t, b.s = t, s
	b.playing.Store(true)
	if state, err := t.watch(ctx, b.log, start, start.Add(5*time.Second)); err == nil && state != "PLAYING" {
		b.log.Warn("airplay.not_playing", slog.String("state", state), slog.Duration("after", time.Since(start).Round(time.Second)))
	}
}

// end gives the zone up when the session ends: to another room of the zone
// whose session is active, else by restoring it. On shutdown the zone is
// always restored, since the other bridges stop too.
func (b *bridge) end(ctx context.Context, shutdown bool) {
	for _, ho := range b.owners.unwait(b.o.room) {
		ho.t.restore(ctx, b.log, ho.s) // handed to us, but we never took it
	}
	if b.t == nil {
		return
	}
	if shutdown || !b.owners.handOff(b.t.zone.UDN, b.o.room, &handoff{t: b.t, s: b.s}) {
		b.t.restore(ctx, b.log, b.s)
		b.owners.release(b.t.zone.UDN, b.o.room)
	} else {
		b.log.Info("airplay.zone_handed_off", slog.String("zone", b.t.zone.UDN))
	}
	b.t, b.s = nil, nil
	b.playing.Store(false)
}

func (b *bridge) setVolume(ctx context.Context, v int) {
	b.volume = v
	if b.t == nil {
		return
	}
	err := upnp.SetVolume(ctx, b.t.hc, b.t.zone.Rendering, v)
	b.log.Info("play.volume", slog.Int("zone", v), slog.Any("err", err))
}

// logStats logs the stream counters at debug level every 10 s, and audio
// dropped beyond --airplay-buffer at info level while the zone plays it
// (an idle receiver drops by design): dropped audio is an audible gap, and how often it happens is not measured yet.
func logStats(ctx context.Context, log *slog.Logger, rcv *airplay.Receiver, playing func() bool) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	var last stream.LiveStats
	bps := float64(rcv.Source.Format().BytesPerSecond())
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s := rcv.Source.Stats()
			if d := s.Dropped - last.Dropped; d > 0 && playing() {
				log.Info("airplay.dropped", slog.Duration("audio", time.Duration(float64(d)/bps*float64(time.Second)).Round(time.Millisecond)),
					slog.Duration("in_10s", time.Duration(float64(s.In-last.In)/bps*float64(time.Second)).Round(time.Millisecond)))
			}
			last = s
			log.Debug("airplay.stats", slog.Int64("in", s.In), slog.Int64("out", s.Out),
				slog.Int64("silence", s.Silence), slog.Int64("dropped", s.Dropped), slog.Int("peak", int(s.Peak)))
		}
	}
}
