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
- `raumfeld-connect --host <addr>` prints the zone configuration and each
  zone renderer's volume, mute, transport state and current URI.
