// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package airplay runs shairport-sync as a subprocess and exposes the PCM
// it writes to stdout as a stream.Source.
//
// Measured with shairport-sync 5.6 (AirPlay 2, stdout backend) and nqptp
// 1.2.9 on 2026-10-15: stdout arrives at playback pace, is silent while
// paused and stays open after playback stops; two instances with distinct
// names and ports ran concurrently on one nqptp. The nqptp daemon must
// already run — it needs UDP 319/320 and is shared by all instances.
package airplay

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/SukramJ/go-raumfeld-connect/internal/stream"
)

// config selects the stdout backend in the CD format the zone plays; the
// name and port come from the command line, so one file fits every
// instance. shairport-sync transcodes AirPlay 2's AAC/48000 to it.
// Volume stays at full scale: with its default software volume the
// measured output peaked near -39 dBFS (2026-10-15), inaudible at zone
// volume 27; loudness belongs to the zone. shairport-sync still runs
// run_this_when_volume_is_set with the AirPlay volume appended
// (player.c:5345 calls it before checking ignore_volume_control), and the
// hook forwards it to Receiver.Volume.
const config = `general = {
	output_backend = "stdout";
	ignore_volume_control = "yes";%s
};
stdout = {
	output_rate = 44100;
	output_format = "S16_LE";
	output_channels = 2;
};
`

// Config describes one receiver.
type Config struct {
	Bin  string // shairport-sync binary, built with --with-airplay-2 --with-stdout
	Name string // AirPlay name; shairport-sync derives the AirPlay identity and nqptp clock from it
	Port int    // RTSP port, distinct per instance on one address

	// Buffer bounds the audio held between shairport-sync and the renderer.
	Buffer time.Duration

	// Hook, if set, is the command shairport-sync runs on a volume change,
	// followed by the socket path, "--" and the volume; it must call
	// SendVolume. Quote paths with spaces: shairport-sync splits it with
	// popt.
	Hook string
}

// Receiver is a running shairport-sync instance.
type Receiver struct {
	Source *stream.Live
	// Volume carries the latest AirPlay volume when Config.Hook is set.
	Volume <-chan float64

	ln   net.Listener // volume hook socket, nil without Config.Hook
	cmd  *exec.Cmd
	dir  string
	done chan struct{} // closed when stderr is drained
}

// Start launches shairport-sync; it stops when ctx is cancelled.
func Start(ctx context.Context, c Config, log *slog.Logger) (*Receiver, error) {
	dir, err := os.MkdirTemp("", "raumfeld-airplay-")
	if err != nil {
		return nil, err
	}
	var (
		hook string
		vol  chan float64
		ln   net.Listener
	)
	if c.Hook != "" {
		sock := filepath.Join(dir, "volume.sock")
		var lc net.ListenConfig
		if ln, err = lc.Listen(ctx, "unix", sock); err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
		hook = fmt.Sprintf("\n\trun_this_when_volume_is_set = %q;", c.Hook+" "+strconv.Quote(sock)+" --")
		vol = make(chan float64, 1)
		go acceptVolume(ctx, ln, vol, log)
	}
	conf := filepath.Join(dir, "shairport-sync.conf")
	if err := os.WriteFile(conf, fmt.Appendf(nil, config, hook), 0o600); err != nil {
		closeListener(ln)
		_ = os.RemoveAll(dir)
		return nil, err
	}

	cmd := exec.CommandContext(ctx, c.Bin, "-c", conf, "-a", c.Name, "--port="+strconv.Itoa(c.Port), "-v") //nolint:gosec // the binary and its arguments come from the operator's flags
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		closeListener(ln)
		_ = os.RemoveAll(dir)
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		closeListener(ln)
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		closeListener(ln)
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("start %s: %w", c.Bin, err)
	}
	log.Info("airplay.started", slog.String("name", c.Name), slog.Int("port", c.Port), slog.Int("pid", cmd.Process.Pid))

	r := &Receiver{
		Source: stream.NewLive(stdout, stream.CD, c.Buffer),
		Volume: vol,
		ln:     ln,
		cmd:    cmd,
		dir:    dir,
		done:   make(chan struct{}),
	}
	go r.logStderr(stderr, log.With(slog.String("name", c.Name)))
	return r, nil
}

// Wait waits for shairport-sync to exit and removes its config.
func (r *Receiver) Wait() error {
	<-r.done
	err := r.cmd.Wait()
	closeListener(r.ln)
	_ = os.RemoveAll(r.dir)
	var ee *exec.ExitError
	if errors.Is(err, context.Canceled) || errors.As(err, &ee) && !ee.Exited() {
		return nil // stopped by the SIGTERM from ctx
	}
	return err
}

// logStderr forwards shairport-sync's log: session changes at Info, the
// rest at Debug.
func (r *Receiver) logStderr(stderr io.Reader, log *slog.Logger) {
	defer close(r.done)
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		line := sessionLine(sc.Text())
		if strings.Contains(line, "Connection ") || strings.Contains(line, "Playback stopped") {
			log.Info("airplay.session", slog.String("msg", line))
		} else {
			log.Debug("airplay.log", slog.String("msg", line))
		}
	}
}

// sessionLine drops the elapsed-time and source-location prefix of a
// shairport-sync -v line, e.g. `  0.000462 "player.c:4254" Connection 1: …`.
func sessionLine(s string) string {
	s = strings.TrimSpace(s)
	if _, rest, ok := strings.Cut(s, `" `); ok && strings.Contains(s[:len(s)-len(rest)], `.c:`) {
		return rest
	}
	return s
}

// SendVolume is the hook side: it passes shairport-sync's volume argument
// to the receiver listening on sock.
func SendVolume(ctx context.Context, sock, volume string) error {
	if _, err := strconv.ParseFloat(volume, 64); err != nil {
		return fmt.Errorf("volume %q: %w", volume, err)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sock)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_, err = io.WriteString(conn, volume+"\n")
	return err
}

// acceptVolume reads one volume per hook connection and keeps only the
// latest in vol.
func acceptVolume(ctx context.Context, ln net.Listener, vol chan float64, log *slog.Logger) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		line, err := bufio.NewReader(conn).ReadString('\n')
		_ = conn.Close()
		v, perr := strconv.ParseFloat(strings.TrimSpace(line), 64)
		if err != nil || perr != nil {
			log.Warn("airplay.volume_hook", slog.Any("err", errors.Join(err, perr)))
			continue
		}
		log.Debug("airplay.volume", slog.Float64("airplay", v))
		select {
		case <-vol: // drop the stale value
		default:
		}
		select {
		case vol <- v:
		case <-ctx.Done():
			return
		}
	}
}

func closeListener(ln net.Listener) {
	if ln != nil {
		_ = ln.Close()
	}
}

// ZoneVolume maps an AirPlay volume — -30 (quietest) to 0 dB, -144 for
// mute — linearly onto 0..maxVol, the way the sender's slider moves. How
// Raumfeld's 0..100 scale relates to loudness is not measured.
func ZoneVolume(airplay float64, maxVol int) int {
	if airplay <= -30 {
		return 0
	}
	f := min(1, (airplay+30)/30)
	return int(f*float64(maxVol) + 0.5)
}
