# go-raumfeld-connect

A pure-Go bridge for **Teufel Raumfeld** multiroom speakers (Raumfeld One S,
Cinebar Lux and other devices of the Raumfeld platform). It talks to the
speakers locally — over UPnP AV and the Raumfeld host's HTTP API — with no
cloud dependency.

> **Status: early development.** AirPlay 2 to Raumfeld zones works
> (`--airplay`, tested with One S and Cinebar Lux); Matter is not started yet.

## Goals

- **Local control** of Raumfeld zones and rooms: transport (play, pause,
  stop), volume, mute, standby, zone membership.
- **AirPlay** input: expose each Raumfeld zone as an AirPlay target and feed
  the received audio to the zone renderer as an HTTP stream.
- **Matter** exposure via [`go-fabric`](https://github.com/SukramJ/go-fabric):
  each zone as a Matter Speaker (on/off, volume) for Apple Home, Alexa and
  Google Home. Matter carries control only, no audio.

## Quickstart

### Linux (Debian 13, e.g. a Proxmox LXC container)

One-liner that builds the AirPlay 2 dependencies (nqptp, shairport-sync)
from pinned release tags, downloads the latest release and verifies its
checksum, installs it under `/opt/go-raumfeld-connect`, creates a dedicated
`raumfeld` service user, asks for the Raumfeld host and the rooms to offer
over AirPlay, and registers a hardened systemd unit:

```bash
curl -sSfL https://raw.githubusercontent.com/SukramJ/go-raumfeld-connect/main/script/install.sh | sudo bash
```

Pin a specific version:

```bash
curl -sSfL https://raw.githubusercontent.com/SukramJ/go-raumfeld-connect/main/script/install.sh | sudo bash -s -- 0.2.0
```

No release is published yet; until then install a binary built with
`make build` (for a container: `pct push <id> bin/raumfeld-connect /root/raumfeld-connect`):

```bash
curl -sSfL https://raw.githubusercontent.com/SukramJ/go-raumfeld-connect/main/script/install.sh | sudo BINARY=/root/raumfeld-connect bash
```

The wizard prompts read from `/dev/tty`, so they work over the
`curl | bash` pipe. An existing `/etc/go-raumfeld-connect/raumfeld-connect.env`
is never touched. Supports `linux/amd64`, `linux/arm64` and `linux/arm`.

Network: the machine needs a fixed LAN address. The Raumfeld host fetches
the audio from it on TCP 59317, AirPlay senders reach it on TCP 7000 and
upwards, and mDNS must reach the senders (an mDNS repeater across subnets).

After install:

```bash
sudo systemctl status go-raumfeld-connect                 # check it stayed up
journalctl -u go-raumfeld-connect -f                      # follow the logs
sudo nano /etc/go-raumfeld-connect/raumfeld-connect.env   # rooms, max volume, …
raumfeld-connect --host <ip>                              # read-only: zones, rooms, state
```

## Building

```sh
make setup   # install developer tooling + git hooks
make build   # build bin/raumfeld-connect
make check   # vet + fmt-check + lint + test
```

`make help` lists every target.

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE), the same
licence as [`go-fabric`](https://github.com/SukramJ/go-fabric). Contributions follow the
[AI contribution policy](AI_POLICY.md).

Raumfeld and Teufel are trademarks of Lautsprecher Teufel GmbH. This project
is not affiliated with or endorsed by Teufel.
