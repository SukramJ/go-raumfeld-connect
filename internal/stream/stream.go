// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package stream serves live PCM audio as an endless WAV over HTTP for a
// Raumfeld renderer to fetch.
//
// Measured on a zone renderer (2026-10-15): the host fetches the URL once,
// as soon as SetAVTransportURI is called, and reads an endless WAV without
// Content-Length. Fed faster than real time it reads ~15.5 s ahead, so a
// Source must deliver audio at playback pace.
package stream

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
)

// Format describes interleaved signed little-endian PCM.
type Format struct {
	SampleRate    uint32
	Channels      uint16
	BitsPerSample uint16
}

// CD is 44.1 kHz, stereo, 16 bit — the format the measured live stream used.
var CD = Format{SampleRate: 44100, Channels: 2, BitsPerSample: 16}

// BytesPerSecond is the PCM data rate.
func (f Format) BytesPerSecond() int {
	return int(f.SampleRate) * int(f.Channels) * int(f.BitsPerSample) / 8
}

// Source is live PCM audio. Read blocks until audio is due, so a reader
// receives it at playback pace.
type Source interface {
	io.Reader
	Format() Format
}

// wavHeader returns a 44-byte WAV header with unknown length: both size
// fields carry their maximum, as in the measured live stream.
func wavHeader(f Format) []byte {
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], 0xFFFFFFFF)
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1) // PCM
	binary.LittleEndian.PutUint16(h[22:], f.Channels)
	binary.LittleEndian.PutUint32(h[24:], f.SampleRate)
	binary.LittleEndian.PutUint32(h[28:], f.SampleRate*uint32(f.Channels)*uint32(f.BitsPerSample)/8)
	binary.LittleEndian.PutUint16(h[32:], f.Channels*f.BitsPerSample/8)
	binary.LittleEndian.PutUint16(h[34:], f.BitsPerSample)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], 0xFFFFFFFF-36)
	return h
}

// Handler serves one Source to one client at a time. A new request takes
// over: the previous one is cancelled, so a renderer that re-fetches does
// not split the audio between two connections.
type Handler struct {
	src Source
	log *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{} // closed when the current client's copy returns
}

// NewHandler returns a handler streaming src.
func NewHandler(src Source, log *slog.Logger) *Handler {
	return &Handler{src: src, log: log}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	h.mu.Lock()
	prevCancel, prevDone := h.cancel, h.done
	h.cancel, h.done = cancel, done
	h.mu.Unlock()
	// The source is read by one client at a time: stop the previous one and
	// wait until it has left the read loop.
	if prevCancel != nil {
		prevCancel()
		<-prevDone
	}

	h.log.Info("stream.client", slog.String("remote", r.RemoteAddr), slog.String("path", r.URL.Path),
		slog.String("proto", r.Proto))
	n, err := h.copy(ctx, w)
	h.log.Info("stream.client_done", slog.String("remote", r.RemoteAddr),
		slog.Int64("bytes", n), slog.Any("err", err))
}

func (h *Handler) copy(ctx context.Context, w http.ResponseWriter) (int64, error) {
	flusher, _ := w.(http.Flusher)
	if _, err := w.Write(wavHeader(h.src.Format())); err != nil {
		return 0, err
	}
	// 100 ms of audio per write keeps latency low without tiny packets.
	buf := make([]byte, h.src.Format().BytesPerSecond()/10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, rerr := h.src.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return total, err
			}
			total += int64(n)
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return total, nil
			}
			return total, rerr
		}
	}
}
