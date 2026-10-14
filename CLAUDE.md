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
- **License: MIT.** Every Go source file starts with:
  ```go
  // SPDX-License-Identifier: MIT
  // Copyright (C) 2026 SukramJ
  ```
- **Deployment**: one static binary (`CGO_ENABLED=0`), `raumfeld-connect`.

## Repository Structure

```
cmd/raumfeld-connect/  entry point (main.go)
internal/version/      build-info package (Version/Commit/BuildDate, set via ldflags)
script/                extract-release-notes.sh
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
  Not yet tested: long-polling with `updateID`, and any state-changing call.
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
- **Vendor devices**: `RaumfeldDevice:1` (SetupService: info, network,
  firmware update), `ConfigDevice:1` (ConfigService: preferences), and a
  `MediaServer:1` with an extended ContentDirectory (queues, station
  buttons).
- **Privacy**: device descriptions carry room names, IP addresses and the
  MAC address (`serialNumber`). Scrub captures before they go into
  `testdata/`; raw captures belong in the git-ignored `/captures/`.
