// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/SukramJ/go-raumfeld-connect/internal/airplay"
	"github.com/SukramJ/go-raumfeld-connect/internal/upnp"
)

// runAirPlay runs an AirPlay receiver until SIGINT or SIGTERM. The zone is
// taken over when a session becomes active and restored when it ends, so
// the zone is left alone while nobody plays.
func runAirPlay(log *slog.Logger, ac airplay.Config, o playOptions, maxVolume int) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	ac.Hook = strconv.Quote(exe) + " -airplay-hook"
	rcv, err := airplay.Start(ctx, ac, log)
	if err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- rcv.Wait() }()
	defer serve(log, o.port, rcv.Source)()
	go logStats(ctx, log, rcv)

	b := &bridge{log: log, o: o, volume: o.volume}
	defer b.release(ctx)
	for {
		select {
		case <-ctx.Done():
			b.release(ctx)
			return <-exited
		case err := <-exited:
			return err
		case ev := <-rcv.Events:
			switch ev.Kind {
			case airplay.Active:
				b.takeOver(ctx)
			case airplay.Inactive:
				b.release(ctx)
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

	t *target // nil while the zone is not ours
	s *saved
}

func (b *bridge) takeOver(ctx context.Context) {
	if b.t != nil {
		return
	}
	t, err := resolve(ctx, b.o)
	if err != nil {
		b.log.Error("airplay.zone", slog.String("err", err.Error()))
		return
	}
	s, err := t.save(ctx, b.log)
	if err != nil {
		b.log.Error("airplay.zone", slog.String("err", err.Error()))
		return
	}
	start := time.Now()
	if err := t.start(ctx, b.log, b.volume); err != nil {
		b.log.Error("airplay.zone", slog.String("err", err.Error()))
		t.restore(ctx, b.log, s)
		return
	}
	b.t, b.s = t, s
	_ = t.watch(ctx, b.log, start, start.Add(3*time.Second))
}

func (b *bridge) release(ctx context.Context) {
	if b.t == nil {
		return
	}
	b.t.restore(ctx, b.log, b.s)
	b.t, b.s = nil, nil
}

func (b *bridge) setVolume(ctx context.Context, v int) {
	b.volume = v
	if b.t == nil {
		return
	}
	err := upnp.SetVolume(ctx, b.t.hc, b.t.zone.Rendering, v)
	b.log.Info("play.volume", slog.Int("zone", v), slog.Any("err", err))
}

func logStats(ctx context.Context, log *slog.Logger, rcv *airplay.Receiver) {
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
}
