# Version 0.1.0 (2026-10-15)

## What's Changed

### Changed

- Licence changed from MIT to Apache-2.0, matching `go-fabric`: `LICENSE`,
  new `NOTICE`, and the SPDX header of every source file;
  `license_header_test.go` enforces the header on every Go file.

### Added

- Project scaffold: Makefile, golangci-lint config, git hooks, CI (lint,
  test matrix, build, audit), CodeQL, Dependabot with auto-merge, release
  workflow. The `raumfeld-connect` binary reports its build info and exits.
- Read-only Raumfeld client: `internal/host` (host API session handling,
  `getZones`, `listDevices`) and `internal/upnp` (device descriptions, SOAP
  calls with UPnP fault decoding, `GetVolume`, `GetMute`,
  `GetTransportInfo`, `GetPositionInfo`, `GetMediaInfo`). Tests run against
  scrubbed captures from real devices.
- Write wrappers in `internal/upnp`, each measured on a zone renderer
  first: `SetVolume` (0..100 per SCPD), `SetAVTransportURI`, `Play`,
  `Stop`, `LeaveStandby`, `EnterAutomaticStandby`. A `live`-tagged test
  round-trips a real renderer's volume (`RAUMFELD_LIVE_RENDERER`).
- `internal/stream`: endless PCM-as-WAV HTTP handler (one client at a time,
  a new request takes over) and a paced sine-tone Source; `internal/zone`
  resolves a room to its zone renderer.
- `raumfeld-connect --host <addr> --play-tone <room>` streams a test tone
  to the room's zone (`--tone-duration`, `--stream-port` default 59317,
  `--advertise`, `--volume`) and restores URI, metadata, volume and
  standby afterwards.
- `raumfeld-connect --host <addr>` prints the zone configuration and each
  zone renderer's volume, mute, transport state and current URI.
- AirPlay 2 input: `raumfeld-connect --host <addr> --airplay "<room>,…"`
  runs one shairport-sync (AirPlay 2, stdout backend) per room and streams
  it to the room's zone (`internal/airplay`, `stream.Live` with silence for
  pauses). The zone is taken over only while an AirPlay session is active
  and restored afterwards (URI, metadata, volume, automatic or manual
  standby); the sender's volume drives the zone (`--max-volume`). A room in
  no zone gets a zone of its own for the session (`connectRoomsToZone`,
  `dropRoomJob`). Flags: `--airplay-port`, `--airplay-idle`,
  `--airplay-buffer`, `--shairport-sync`, `--debug`.
- `script/install.sh`: Debian/LXC installer — builds nqptp 1.2.9 and
  shairport-sync 5.6, installs a release (or `BINARY=`) under
  `/opt/go-raumfeld-connect`, asks host and rooms, and registers a hardened
  systemd service; README quickstart with the one-liner.
