# Handover — state of go-raumfeld-connect, 2026-10-15

Written when development moved from a macOS laptop to a Linux (x86-64)
system. The measured protocol facts live in [`CLAUDE.md`](../CLAUDE.md)
("Raumfeld protocol — what has been measured") and are not repeated here.
Installation-specific data (IP addresses, room names, firewall rule,
per-day write permissions, raw captures, test scripts) is deliberately not
in this public repository; it travels separately in the git-ignored
`captures/` directory (`captures/HANDOVER-PRIVATE.md`).

## Goal

A local bridge for Teufel Raumfeld speakers (tested: Cinebar Lux as host,
three One S): AirPlay as audio input, Matter (via `go-fabric`) for control
in Apple Home / Alexa / Google Home. No cloud.

## Decisions

| Decision | Reason |
| --- | --- |
| Go, one static binary, **no CGo**; daemon target **linux/amd64** | owner's choice; CGo would add glibc/C-library coupling, C memory-safety in network-facing decoding, and C licences outside `make licenses` |
| **Apache-2.0** | same licence as `go-fabric`, which will be embedded (#2) |
| **Matter = control only**: zone as Speaker 0x0022 (OnOff + LevelControl) | Matter carries no speaker audio; `go-fabric` leaves the Media clusters out of scope (`docs/concept-matter-1.6.1-and-device-layer.md:533`) |
| **AirPlay phase 1: shairport-sync as a subprocess** behind `stream.Source` | de-facto open-source AirPlay 2 receiver, actively maintained; main code under MIT-style licences (its `LICENSES` file); its AirPlay 2 timing daemon nqptp is GPL-2.0 and stays a separate process |
| Own Go AirPlay 2 receiver only later, ported from shairport-sync's MIT parts (e.g. `pair_ap`) with NOTICE attribution | not from openairplay/airplay2-receiver (no licence); goplay2 is Apache-2.0 but unmaintained since 2023 and needs CGo |
| If audio decoding is ever needed: **`ffmpeg` as a subprocess**, not CGo | keeps C code and its licences out of the daemon process |
| Certified AirPlay 2 is out of reach | only through Apple's MFi programme (authentication IC per Apple Platform Security) |

## What exists (PRs #1–#6)

- `internal/host` — host API client: session from the 307 redirect,
  `getZones`, `listDevices`.
- `internal/upnp` — device descriptions, SOAP with fault decoding; read
  wrappers (`GetVolume`, `GetMute`, `GetTransportInfo`, `GetPositionInfo`,
  `GetMediaInfo`) and measured write wrappers (`SetVolume`,
  `SetAVTransportURI`, `Play`, `Stop`, `LeaveStandby`,
  `EnterAutomaticStandby`). `live_test.go` (`-tags live`,
  `RAUMFELD_LIVE_RENDERER=<zone description URL>`) round-trips a volume.
- `internal/stream` — `Source` (live PCM at playback pace), endless WAV HTTP
  handler (one client at a time), paced test `Tone`.
- `internal/zone` — room name → zone renderer.
- `raumfeld-connect --host <ip>` prints zones and renderer state;
  `--play-tone <room>` streams a test tone to that room's zone and restores
  URI, metadata, volume and standby (`--tone-duration`, `--stream-port`
  default 59317, `--advertise`, `--volume`).

## Requirements on the network

- The speakers' **host must open TCP connections to the daemon's stream
  port**. If it cannot, `SetAVTransportURI` and `Play` still succeed and
  the zone silently stays `TRANSITIONING`.
- AirPlay discovery across subnets needs an mDNS repeater; AirPlay's own
  connections from phones to the daemon need their ports open too.
- SSDP multicast did not cross subnets; discovery goes through the host's
  `listDevices` instead.

## Working rules

- State-changing tests only on rooms the owner releases, per day; always
  restore URI + metadata, volume and standby, and verify via `getZones`.
- Raw captures go to `/captures/` (git-ignored); testdata only scrubbed.
- PRs get squash auto-merge right away; CI flakes on the `actions/setup-go`
  download happened three times on 2026-10-15 — rerun the failed jobs.
- See `CLAUDE.md` for conventions and the token-use rules.

## Open items

1. `Play` → `PLAYING`: ~1.7 s with Go's chunked stream vs ~0.6 s with an
   unchunked Python server — cause not investigated (try an unchunked
   response via a hijacked connection and compare).
2. Audible latency not measured (`PLAYING` is a state, not sound): click +
   microphone measurement in the room.
3. Host API: long-polling with `updateID`, zone membership calls
   (a room outside every zone has no zone renderer).
4. UPnP eventing (GENA `SUBSCRIBE`) instead of polling — callback port must
   be reachable from the speakers like the stream port.
5. shairport-sync: stdout arrives at playback pace, but writes nothing
   during pause and does not end on stop (measured, see `CLAUDE.md`).
   Several AirPlay 2 instances (one per zone) on one host with one nqptp:
   not verified.
6. AAC passthrough (only relevant for an own receiver): does a zone play a
   live AAC stream? Not measured.
7. How Apple Home / Alexa render a Matter Speaker: not measured.

## Next steps on the Linux system

1. Install Go 1.27.2, `gh` (`gh auth login`), clone the repo, `make setup`,
   `make check`; copy `captures/` over and read `captures/HANDOVER-PRIVATE.md`.
2. Move the UniFi rule's destination to the Linux host; check reachability
   with `raumfeld-connect --host <host-ip>` and, on a released room,
   `--play-tone <room>`.
3. Done 2026-10-15: shairport-sync 5.6 built from source in `~/src`
   (Ubuntu's 4.3.7 package lacks AirPlay 2) with `--with-airplay-2
   --with-pipe --with-stdout`, run from the build tree; nqptp 1.2.9
   installed as a systemd service.
4. Implement a `stream.Source` reading from
   shairport-sync, one instance per zone, and map volume / start / stop to
   the zone (`SetVolume`, `Play`, `Stop`). The source must fill gaps with
   silence at playback pace; end of playback comes from shairport-sync's
   log or metadata, not from stdout.
