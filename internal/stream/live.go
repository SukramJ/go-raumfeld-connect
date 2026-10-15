// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package stream

import (
	"encoding/binary"
	"io"
	"math"
	"sync"
	"time"
)

// maxLag is how far a reader may fall behind the Live clock before the
// clock restarts; catching up instead would burst audio at the renderer.
const maxLag = time.Second

// Live is a Source fed by a producer that writes PCM at playback pace but
// may stop writing for a while. Measured for shairport-sync's stdout
// (2026-10-15): it writes nothing while AirPlay is paused and does not end
// when playback stops. Read keeps its own playback clock and fills gaps
// with silence, so the renderer never runs dry; audio beyond maxBuffer is
// dropped oldest first, which bounds the added latency.
type Live struct {
	format Format
	max    int // buffered bytes kept at most, whole frames

	mu  sync.Mutex
	buf []byte
	err error // producer's terminal read error

	start time.Time
	sent  int64 // bytes returned since start

	stats LiveStats
}

// LiveStats counts bytes since the Source was created.
type LiveStats struct {
	In      int64 // read from the producer
	Dropped int64 // discarded beyond maxBuffer
	Out     int64 // returned by Read
	Silence int64 // of Out, filled with silence
	Peak    int16 // largest absolute 16-bit sample read from the producer
}

// Stats returns the counters so far.
func (l *Live) Stats() LiveStats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stats
}

// NewLive starts reading PCM in format f from r and returns the Source.
// The read loop ends when r returns an error; Read returns io.EOF once the
// buffered audio is used up.
func NewLive(r io.Reader, f Format, maxBuffer time.Duration) *Live {
	frame := f.frameSize()
	maxBytes := int(maxBuffer.Seconds()*float64(f.BytesPerSecond())) / frame * frame
	l := &Live{format: f, max: max(maxBytes, frame)}
	go l.fill(r)
	return l
}

// Format returns the producer's format.
func (l *Live) Format() Format { return l.format }

func (l *Live) fill(r io.Reader) {
	frame := l.format.frameSize()
	chunk := make([]byte, 16*1024)
	for {
		n, err := r.Read(chunk)
		peak := peak16(chunk[:n], l.format)
		l.mu.Lock()
		l.buf = append(l.buf, chunk[:n]...)
		l.stats.In += int64(n)
		l.stats.Peak = max(l.stats.Peak, peak)
		if over := len(l.buf) - l.max; over > 0 {
			over = (over + frame - 1) / frame * frame
			l.buf = append(l.buf[:0], l.buf[over:]...)
			l.stats.Dropped += int64(over)
		}
		if err != nil {
			l.err = err
		}
		l.mu.Unlock()
		if err != nil {
			return
		}
	}
}

// Read fills p with whole frames once they are due: buffered audio first,
// silence for the rest.
func (l *Live) Read(p []byte) (int, error) {
	frame := l.format.frameSize()
	n := len(p) / frame * frame
	if n == 0 {
		return 0, nil
	}
	now := time.Now()
	if l.start.IsZero() || now.Sub(l.due(l.sent)) > maxLag {
		l.start, l.sent = now, 0
	}
	l.sent += int64(n)
	if d := time.Until(l.due(l.sent)); d > 0 {
		time.Sleep(d)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) == 0 && l.err != nil {
		return 0, io.EOF
	}
	c := copy(p[:n], l.buf)
	l.buf = l.buf[c:]
	clear(p[c:n]) // signed PCM: zero is silence
	l.stats.Out += int64(n)
	l.stats.Silence += int64(n - c)
	return n, nil
}

// due is when the first sent bytes have been played.
func (l *Live) due(sent int64) time.Time {
	return l.start.Add(time.Duration(float64(sent) / float64(l.format.BytesPerSecond()) * float64(time.Second)))
}

// peak16 is the largest absolute sample in p for 16-bit formats, else 0.
func peak16(p []byte, f Format) int16 {
	if f.BitsPerSample != 16 {
		return 0
	}
	var pk int16
	for i := 0; i+1 < len(p); i += 2 {
		s := int16(binary.LittleEndian.Uint16(p[i:])) //nolint:gosec // PCM is two's complement: reinterpreting the bits is intended
		if s == math.MinInt16 {
			return math.MaxInt16
		}
		pk = max(pk, s, -s)
	}
	return pk
}

func (f Format) frameSize() int { return int(f.Channels) * int(f.BitsPerSample) / 8 }
