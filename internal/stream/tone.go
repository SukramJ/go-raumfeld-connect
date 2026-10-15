// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package stream

import (
	"encoding/binary"
	"io"
	"math"
	"time"
)

// Tone is a paced sine-wave Source in CD format, for testing a renderer
// without an audio input.
type Tone struct {
	freq      float64
	amplitude float64 // 0..1 of full scale
	limit     int64   // total frames, 0 = endless

	start  time.Time
	frames int64
}

// NewTone returns a tone of freq Hz at amplitude (0..1 of full scale),
// lasting d, or endless when d is 0.
func NewTone(freq, amplitude float64, d time.Duration) *Tone {
	return &Tone{freq: freq, amplitude: amplitude, limit: int64(d.Seconds() * float64(CD.SampleRate))}
}

// Format returns CD.
func (*Tone) Format() Format { return CD }

// Read fills p with whole frames and blocks until they are due, so audio
// leaves at playback pace from the first Read on.
func (t *Tone) Read(p []byte) (int, error) {
	const frameSize = 4 // 2 channels × 16 bit
	if t.start.IsZero() {
		t.start = time.Now()
	}
	n := int64(len(p) / frameSize)
	if t.limit > 0 {
		if t.frames >= t.limit {
			return 0, io.EOF
		}
		n = min(n, t.limit-t.frames)
	}
	for i := range n {
		phase := 2 * math.Pi * t.freq * float64(t.frames+i) / float64(CD.SampleRate)
		s := int16(t.amplitude * math.MaxInt16 * math.Sin(phase))
		v := uint16(s) //nolint:gosec // PCM is two's complement: reinterpreting the bits is intended
		binary.LittleEndian.PutUint16(p[i*frameSize:], v)
		binary.LittleEndian.PutUint16(p[i*frameSize+2:], v)
	}
	t.frames += n
	due := t.start.Add(time.Duration(float64(t.frames) / float64(CD.SampleRate) * float64(time.Second)))
	if d := time.Until(due); d > 0 {
		time.Sleep(d)
	}
	return int(n * frameSize), nil
}
