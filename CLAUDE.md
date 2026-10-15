# CLAUDE.md — AI Assistant Guide for go-raumfeld-connect

## Project Overview

**go-raumfeld-connect** is a pure-Go bridge for **Teufel Raumfeld**
multiroom speakers. It controls them locally over UPnP AV and the Raumfeld
host's HTTP API, and is meant to expose them to other ecosystems: AirPlay
(audio in) and Matter via `github.com/SukramJ/go-fabric` (control only —
Matter carries no audio for speakers). The repository is at the scaffold
stage.

## Key Characteristics

- **Language**: Go 1.27+ (see `go.mod` / CI `GO_VERSION`).
- **Module path**: `github.com/SukramJ/go-raumfeld-connect`.
- **License: Apache-2.0**, the same licence as `go-fabric`, which this
  project will embed. Every Go source file starts with the two lines below,
  enforced by `license_header_test.go`; Makefile and scripts carry the same
  lines as `#` comments:
  ```go
  // SPDX-License-Identifier: Apache-2.0
  // Copyright (C) 2026 SukramJ.
  ```
- **Deployment**: one static binary (`CGO_ENABLED=0`), `raumfeld-connect`.

## Repository Structure

```
doc.go                 module doc; license_header_test.go guards the licence header
cmd/raumfeld-connect/  entry point; --host prints zones/renderer state; --play-tone <room> streams a test tone, --airplay <room> bridges AirPlay 2 to the zone; both restore the zone
internal/airplay/      shairport-sync subprocess (stdout PCM) and its volume hook
internal/host/         Raumfeld host HTTP API client (session handling, getZones, listDevices)
internal/upnp/         UPnP device description + SOAP client, measured read and write wrappers
internal/stream/       live PCM-as-WAV HTTP handler (one client at a time), a paced test-tone Source and Live (paced, gap-filling)
internal/zone/         room name → zone renderer (getZones + listDevices + description)
internal/version/      build-info package (Version/Commit/BuildDate, set via ldflags)
script/                extract-release-notes.sh
notes/handover.md      state, decisions, open items and next steps (handover of 2026-10-15)
.github/workflows/     ci.yml (lint/test/build/audit), codeql.yml, release-on-tag.yml, dependabot-auto-merge.yml
```

## Development Commands

All defined in the `Makefile` (`make help` lists them):

```sh
make build          # build bin/raumfeld-connect
make run            # build then run
make test           # go test -race -count=1 -timeout=60s ./... (CGO_ENABLED=1 for race detector)
make test-cover     # tests + coverage report (coverage.out)
make vet            # go vet ./...
make fmt            # gofumpt -w . && goimports -w -local <module> .
make fmt-check      # fail if gofumpt would rewrite anything (CI gate)
make lint           # golangci-lint run ./...
make vuln           # govulncheck ./...
make licenses       # go-licenses check, forbids GPL/AGPL/LGPL(reciprocal)/MPL deps
make check          # vet + fmt-check + lint + test — the pre-commit/pre-push gate
make release        # cross-compile linux/amd64, linux/arm64, linux/arm, darwin/arm64 archives into dist/
make setup          # install gofumpt/goimports/golangci-lint/govulncheck/go-licenses + git hooks
make tidy           # go mod tidy
make clean          # remove bin/, dist/, coverage.out
```

## Code Conventions

- **No CGo** in the default build; CGo is only re-enabled transiently for
  the race detector during `make test`.
- **`golangci-lint` v2** config in `.golangci.yaml`; formatting is
  `gofumpt` + `goimports -local github.com/SukramJ/go-raumfeld-connect`.
- **Structured logging**: `log/slog` (enforced by `sloglint`).
- **Git hooks**: `make setup` (or `make hooks`) points `core.hooksPath` at
  `.githooks/`, which blocks direct commits on `main`/`master`. Work on a
  feature branch and open a PR.
- **Commit style**: Conventional Commits with a scope, e.g.
  `feat(upnp): ...`, `fix(host): ...`, `chore(deps): ...`.
- **Release bookkeeping**: a version bump touches
  `internal/version/version.go` and adds a `# Version X.Y.Z (date)` section
  to `changelog.md` in the same commit; `make release` and the release
  workflow read both.

## Token use

Set by the owner on 2026-10-14 ("Gehe effektiv mit den Token um"),
modelled on go-fabric's rules (SukramJ/go-fabric#116).

- **Read slices, not files.** `sed -n a,bp`, `grep -n` with a tight
  pattern and `head`; a whole file only when the task is the file. Never
  re-read what is already in context; cite the earlier read.
- **Reference repos are read by name.** go-mtec2mqtt (project setup) and
  go-fabric (licence, Matter) are the specification for what they cover:
  read the named file or the `grep` hit, not the tree.
- **Probe devices once, then work from captures.** A measurement goes to
  the git-ignored `/captures/` with one scripted call and is cited from
  there; probe again only when the question is new or the device state
  may have changed. Read-only probes need no confirmation; anything that
  changes device state needs the owner's go-ahead for a named room.
- **Logs by their summary.** `make check` and CI are read through a
  filter (`grep -v` the known noise, `tail`); open a full log only on
  failure, with `grep` for `FAIL`, `DATA RACE`, `_test.go:` and the
  error strings — never `cat`. A CI job log is fetched once to a file
  and grepped.
- **One turn, every independent call.** Reads, checks and background
  starts that do not depend on each other go out together; a wait is
  `gh pr checks --watch` or an `until` loop, not polling by hand.
- **Short artefacts.** Commit messages and PR bodies name what changed
  and how it was verified, not prose; status messages to the owner are
  short, outcome first, in German.
- **Skip what cannot have changed.** No `make check` rerun for a
  docs-only change, no device probe for a code change that does not
  touch the wire — say what was skipped and why.
- **Edit by script.** Several replacements in several files are one
  scripted call with asserted anchors, not a chain of interactive edits.
- **Agents read what the brief names.** A delegated agent gets the file
  list and the sources in the brief, reads those, and reports in a fixed
  shape; it does not survey the tree. Two agents only on disjoint paths.

## Raumfeld protocol — what has been measured

Read-only probes on 2026-10-14 against a Cinebar Lux acting as host, with
three One S on the network. Everything below was read from the devices;
anything not listed here is unverified and must be measured or read from the
device before code depends on it.

- **Host HTTP API on tcp/47365** (only open on the host device).
  `GET /` answers `307` to `/<session-uuid>/`; calls live under that prefix.
  `getZones` returns `<zoneConfig>` XML (zones → rooms → renderers, with
  `powerState` and `spotifyConnect`), `listDevices` returns every UPnP device
  with its `location` URL. Every response carries an `updateID` header.
  A request without the prefix answers `307` to an absolute URL
  `/<new-session>/<path>`; an arbitrary, never-issued UUID as prefix is
  accepted (200).
  Not yet tested: long-polling with `updateID`, and any state-changing host
  API call (e.g. zone membership).
- **UPnP ports are dynamic.** Discover via `listDevices` or SSDP; never
  hard-code a port. Unicast `M-SEARCH` to the host answered; the One S did
  not answer unicast `M-SEARCH`.
- **Two renderer kinds** (`urn:schemas-upnp-org:device:MediaRenderer:1`):
  - *Zone renderer* (`modelDescription` "Virtual Media Player"), hosted on
    the host. AVTransport includes `SetAVTransportURI`, `Play`, `Pause`,
    `Stop`, `Seek`, `Next`, `Previous`, sleep timer, `EnterManualStandby`,
    `LeaveStandby`; RenderingControl includes `SetVolume`, `SetMute`,
    `SetRoomVolume`, `SetRoomMute`, `ChangeVolume`.
  - *Room renderer* ("Digital Media Player"), on each device. Adds
    `SetNextAVTransportURI`, `SetVolumeDB`, balance, filter,
    `GetLineInStreamURL`, `SetDeviceSetting`, and a
    `urn:schemas-raumfeld-com:service:RaumfeldGenerator:1` service.
- **Control URLs differ per renderer kind** (zone: `/RenderingService/Control`,
  `/TransportService/Control`; room: `/RenderingControl/ctrl`,
  `/AVTransport/ctrl`) and descriptions carry no `URLBase`, with SCPD paths
  both with and without a leading slash. Always resolve URLs from the
  description; a guessed room control URL answered `501`.
- **Room renderer `GetPositionInfo`** declares only `TrackDuration` and
  `RelTime` in its SCPD and answers `NOT_IMPLEMENTED` for both.
  `GetMediaInfo` exists only on the zone renderer.
- **SOAP faults** arrive as HTTP 500 with `s:Fault` / `UPnPError`
  (unknown action: `401 Invalid Action`). `CurrentMute` measured as `0`
  only.
- **Write actions on the zone renderer** (measured 2026-10-15, zone with one
  One S): `SetVolume`, `SetAVTransportURI`, `Play`, `Stop` answer 200 and
  take effect. Empty `CurrentURIMetaData` is accepted; the renderer then
  builds its own metadata. `LeaveStandby` / `EnterAutomaticStandby` take
  `Room` = the room UDN; verified through `powerState` in `getZones`
  (`EnterAutomaticStandby` showed after ~10 s).
- **The host fetches stream URLs**, not the room's device: connections came
  from the host's address for a zone playing on a One S. The fetch starts at
  `SetAVTransportURI`, before `Play`. Requests send `Icy-MetaData: 1` and no
  User-Agent; MP4/AAC is read with `Range` requests (file end first).
- **Unreachable stream URL fails silently**: with the daemon's port blocked,
  `SetAVTransportURI` and `Play` still answered 200 and the transport stayed
  `TRANSITIONING` with no error. The speakers' network must be able to open
  TCP connections to the daemon's (configurable) stream port.
- **Formats**: MP3, WAV (PCM 16 bit), FLAC, AAC/m4a files and an endless
  live WAV stream (no Content-Length) all reached `PLAYING`; `Play` →
  `PLAYING` took ~1.5 s for MP3 and ~0.6 s for the others. The zone
  renderer's `GetProtocolInfo` sink list includes `audio/mpeg`, `audio/wav`,
  `audio/flac`, `audio/mp4`. Files end in `STOPPED` on their own.
- **Chunked live stream works**: the host requests with HTTP/1.1, so Go's
  `net/http` answers an endless stream with `Transfer-Encoding: chunked`;
  the zone plays it. `Play` → `PLAYING` took ~1.7 s (two runs) against
  ~0.6 s for the unchunked Python test server — cause not investigated.
- **Read-ahead**: fed as fast as it reads, the renderer pulled audio until it
  held ~15.5 s ahead of real time, then read at real-time rate; `PLAYING`
  after ~1.5 s. Fed at real-time pace it cannot read ahead, and `PLAYING`
  came after ~0.6 s. The audible delay was not measured — `PLAYING` is a
  transport state, not sound.
- **shairport-sync stdout** (measured 2026-10-15, shairport-sync 5.6 built
  with `--with-airplay-2 --with-stdout`, nqptp 1.2.9, iPhone as source):
  configured `output_rate = 44100; output_format = "S16_LE";
  output_channels = 2;`, it transcoded AirPlay 2 input `AAC/48000/F24/2` to
  that format. stdout arrives at playback pace (0.97–1.03 × 176400 B/s per
  second) after a 1.8× first second; resuming from pause gave one 1.6×
  second. During pause it writes **nothing** (no silence), and after
  "Playback stopped" stdout stays open with no EOF. Two instances with
  distinct names and ports ran concurrently on one nqptp, each with its own
  clock `/nqptp-<name>`.
- **shairport-sync volume**: by default it attenuates in software; with
  `default airplay volume is -24` the output peaked at 352/32767
  (~-39 dBFS), inaudible at zone volume 27. With
  `ignore_volume_control = "yes"` the output stays full scale and
  `run_this_when_volume_is_set` still runs with the AirPlay volume
  (-30..0, -144 mute; called in `player.c` before the ignore check).
- **AirPlay → zone end to end** (`--airplay`, 2026-10-15, iPhone → One S):
  music audible; delay from a track change on the iPhone to sound in the
  room ~2 s by ear (owner's estimate, not measured); volume changes via the
  hook → `SetVolume` heard "immediately". The zone is taken over on
  shairport-sync's active state and restored after `active_state_timeout`
  (10 s) without play; `Play` → `PLAYING` 1.6–1.9 s per takeover.
- **Manual standby** (measured 2026-10-15, zone renderer): in
  `MANUAL_STANDBY`, `Play` leaves the transport `STOPPED` with status
  "Please turn on a device." and the stream is never fetched — also when
  sent ~50 ms after `LeaveStandby`. `getZones` showed `ACTIVE` ~0.8–0.9 s
  after `LeaveStandby`; `Play` after that works. `EnterManualStandby`
  (`InstanceID`, `Room` = room UDN) answers 200 and restores
  `MANUAL_STANDBY`. `AUTOMATIC_STANDBY` wakes on `Play` alone.
- **Zone UDNs change**: re-forming a room's zone in the app gave it a new
  zone UDN; resolve the zone per use, never cache it.
- **Vendor devices**: `RaumfeldDevice:1` (SetupService: info, network,
  firmware update), `ConfigDevice:1` (ConfigService: preferences), and a
  `MediaServer:1` with an extended ContentDirectory (queues, station
  buttons).
- **Privacy**: device descriptions carry room names, IP addresses and the
  MAC address (`serialNumber`); TuneIn stream URIs carry the MAC as
  `serial=`, and transport metadata reflects listening history. Scrub captures before they go into
  `testdata/`; raw captures belong in the git-ignored `/captures/`.
