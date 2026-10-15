// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package stream

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"
)

// tenth is 100 ms of CD audio.
const tenth = 17640

func TestLiveFillsGapsWithSilence(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	l := NewLive(pr, CD, time.Second)
	if _, err := pw.Write(bytes.Repeat([]byte{1}, tenth)); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	p := make([]byte, tenth)
	for i := range 3 {
		n, err := l.Read(p)
		if err != nil || n != tenth {
			t.Fatalf("read %d: n=%d err=%v", i, n, err)
		}
		want := byte(0)
		if i == 0 {
			want = 1
		}
		if !bytes.Equal(p, bytes.Repeat([]byte{want}, tenth)) {
			t.Errorf("read %d: want all %d", i, want)
		}
	}
	if d := time.Since(start); d < 280*time.Millisecond {
		t.Errorf("300 ms of audio took %v, want playback pace", d)
	}
}

func TestLiveDropsOldestBeyondMaxBuffer(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()
	l := NewLive(pr, CD, 100*time.Millisecond)
	for b := byte(1); b <= 3; b++ {
		if _, err := pw.Write(bytes.Repeat([]byte{b}, tenth)); err != nil {
			t.Fatal(err)
		}
	}
	_ = pw.Close()
	time.Sleep(20 * time.Millisecond) // let fill take the EOF

	p := make([]byte, tenth)
	if n, err := l.Read(p); err != nil || n != tenth || !bytes.Equal(p, bytes.Repeat([]byte{3}, tenth)) {
		t.Fatalf("n=%d err=%v first=%d, want the newest 100 ms", n, err, p[0])
	}
	if _, err := l.Read(p); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want EOF after the producer ended", err)
	}
}

func TestLiveRestartsClockAfterReaderLag(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	l := NewLive(pr, CD, time.Second)
	p := make([]byte, tenth)
	if _, err := l.Read(p); err != nil {
		t.Fatal(err)
	}
	l.start = l.start.Add(-2 * maxLag) // the reader fell behind

	start := time.Now()
	if _, err := l.Read(p); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 80*time.Millisecond {
		t.Errorf("read after lag took %v, want paced, not a catch-up burst", d)
	}
}

func TestLiveStats(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	l := NewLive(pr, CD, time.Second)
	data := make([]byte, tenth/2)
	binary.LittleEndian.PutUint16(data[4:], uint16(0xF000)) // -4096
	if _, err := pw.Write(data); err != nil {
		t.Fatal(err)
	}
	p := make([]byte, tenth)
	if _, err := l.Read(p); err != nil {
		t.Fatal(err)
	}
	s := l.Stats()
	if s.In != tenth/2 || s.Out != tenth || s.Silence != tenth/2 || s.Dropped != 0 || s.Peak != 4096 {
		t.Fatalf("stats = %+v", s)
	}
}
