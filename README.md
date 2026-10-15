# go-raumfeld-connect

A pure-Go bridge for **Teufel Raumfeld** multiroom speakers (Raumfeld One S,
Cinebar Lux and other devices of the Raumfeld platform). It talks to the
speakers locally — over UPnP AV and the Raumfeld host's HTTP API — with no
cloud dependency.

> **Status: early development.** The repository currently holds the project
> scaffold only; the binary prints its build info and exits.

## Goals

- **Local control** of Raumfeld zones and rooms: transport (play, pause,
  stop), volume, mute, standby, zone membership.
- **AirPlay** input: expose each Raumfeld zone as an AirPlay target and feed
  the received audio to the zone renderer as an HTTP stream.
- **Matter** exposure via [`go-fabric`](https://github.com/SukramJ/go-fabric):
  each zone as a Matter Speaker (on/off, volume) for Apple Home, Alexa and
  Google Home. Matter carries control only, no audio.

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
