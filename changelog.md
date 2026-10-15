# Version 0.1.0 (unreleased)

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
