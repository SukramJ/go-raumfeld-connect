#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# go-raumfeld-connect — Linux installer (Debian 13, e.g. a Proxmox LXC).
#
# Builds nqptp and shairport-sync (AirPlay 2, stdout backend) from pinned
# release tags — Debian's shairport-sync package lacks AirPlay 2 — and runs
# nqptp as its own service. Then downloads a raumfeld-connect release,
# verifies its SHA-256 checksum, installs it under /opt/go-raumfeld-connect,
# creates a dedicated `raumfeld` system user, asks for the Raumfeld host and
# the rooms to offer over AirPlay, and registers a hardened systemd service.
#
# Designed for the curl|bash idiom:
#
#   curl -sSfL https://raw.githubusercontent.com/SukramJ/go-raumfeld-connect/main/script/install.sh | sudo bash
#
# Pin a specific version:
#
#   curl -sSfL https://raw.githubusercontent.com/SukramJ/go-raumfeld-connect/main/script/install.sh | sudo bash -s -- 0.2.0
#
# Install a locally built binary (`make build`) instead of a release:
#
#   sudo BINARY=/root/raumfeld-connect bash install.sh
#
# The wizard prompts read from /dev/tty so they work even when the script
# itself is being piped through stdin.
#
# Network: the container needs a bridged LAN address that stays fixed. The
# Raumfeld host fetches the audio from it (TCP 59317), AirPlay senders
# reach it on TCP 7000+ and the ports shairport-sync negotiates, and mDNS
# must reach the senders (an mDNS repeater across subnets).

set -euo pipefail

# --- knobs ------------------------------------------------------------------

REPO="${REPO:-SukramJ/go-raumfeld-connect}"
PREFIX="${PREFIX:-/opt/go-raumfeld-connect}"
CONFIG_DIR="${CONFIG_DIR:-/etc/go-raumfeld-connect}"
CONFIG_FILE="${CONFIG_FILE:-${CONFIG_DIR}/raumfeld-connect.env}"
BIN_LINK_DIR="${BIN_LINK_DIR:-/usr/local/bin}"
SRC_DIR="${SRC_DIR:-/usr/local/src/go-raumfeld-connect}"
SERVICE_USER="${SERVICE_USER:-raumfeld}"
SERVICE_GROUP="${SERVICE_GROUP:-raumfeld}"
SERVICE_NAME="${SERVICE_NAME:-go-raumfeld-connect}"
SYSTEMD_UNIT="/etc/systemd/system/${SERVICE_NAME}.service"

# AirPlay 2 dependencies, built from these tags (measured together with
# raumfeld-connect on 2026-10-15).
NQPTP_TAG="${NQPTP_TAG:-1.2.9}"
SHAIRPORT_TAG="${SHAIRPORT_TAG:-5.6}"

VERSION="${1:-}"
BINARY="${BINARY:-}"

# --- ansi helpers -----------------------------------------------------------

if [ -t 1 ]; then
	BOLD=$(printf '\033[1m'); DIM=$(printf '\033[2m')
	RED=$(printf '\033[31m'); GREEN=$(printf '\033[32m')
	YELLOW=$(printf '\033[33m'); CYAN=$(printf '\033[36m'); RESET=$(printf '\033[0m')
else
	BOLD=""; DIM=""; RED=""; GREEN=""; YELLOW=""; CYAN=""; RESET=""
fi

info()  { printf '%s==>%s %s\n'      "${CYAN}${BOLD}" "${RESET}" "$*"; }
ok()    { printf '%s  ✓%s %s\n'      "${GREEN}"     "${RESET}" "$*"; }
warn()  { printf '%s  ! %s%s\n'      "${YELLOW}"    "$*" "${RESET}" >&2; }
die()   { printf '%s  ✗ %s%s\n'      "${RED}${BOLD}" "$*" "${RESET}" >&2; exit 1; }

# --- preconditions ----------------------------------------------------------

[ "$(id -u)" -eq 0 ] || die "run as root (use sudo)"
[ "$(uname -s)" = "Linux" ] || die "this installer only supports Linux"
command -v systemctl >/dev/null || die "systemctl not found — this installer requires systemd"
command -v apt-get >/dev/null || die "apt-get not found — this installer targets Debian"

# Read os-release in a subshell: sourcing it here would overwrite VERSION
# (Debian sets VERSION="13 (trixie)").
os_id=$(. /etc/os-release && printf '%s' "${ID:-}")
os_name=$(. /etc/os-release && printf '%s' "${PRETTY_NAME:-unknown}")
[ "$os_id" = "debian" ] || warn "written for Debian 13, this is ${os_name} — continuing"

# Architecture mapping — only the targets the release pipeline builds.
case "$(uname -m)" in
	x86_64|amd64) ARCH=amd64 ;;
	aarch64|arm64) ARCH=arm64 ;;
	armv7l|armhf) ARCH=arm ;;
	*) die "unsupported architecture: $(uname -m) (need x86_64, aarch64, or armv7)" ;;
esac

# --- interactive prompts ----------------------------------------------------

# read-from-tty so curl|bash still gets keystrokes; falls back to stdin
# when no tty is attached (CI dry-runs).
TTY=/dev/tty
[ -r "$TTY" ] || TTY=/dev/stdin

prompt() {
	local question="$1" default="${2:-}" answer
	if [ -n "$default" ]; then
		printf '%s%s%s [%s]: ' "${BOLD}" "$question" "${RESET}" "$default" > /dev/tty 2>/dev/null \
			|| printf '%s [%s]: ' "$question" "$default"
	else
		printf '%s%s%s: ' "${BOLD}" "$question" "${RESET}" > /dev/tty 2>/dev/null \
			|| printf '%s: ' "$question"
	fi
	IFS= read -r answer <"$TTY" || answer=""
	[ -n "$answer" ] || answer="$default"
	printf '%s' "$answer"
}

# --- packages ---------------------------------------------------------------

info "installing packages"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
# AirPlay 2 build dependencies from shairport-sync's BUILD.md
# (Debian/Ubuntu); ALSA is left out, the stdout backend does not use it.
apt-get install -y -qq --no-install-recommends \
	build-essential git autoconf automake libtool pkg-config ca-certificates curl \
	libpopt-dev libconfig-dev avahi-daemon libavahi-client-dev libssl-dev libsoxr-dev \
	libplist-dev libsodium-dev uuid-dev libgcrypt-dev xxd libplist-utils \
	libavutil-dev libavcodec-dev libavformat-dev >/dev/null
systemctl enable --now avahi-daemon >/dev/null
ok "packages installed, avahi-daemon running"

# --- nqptp + shairport-sync -------------------------------------------------

# fetch_tag <url> <dir> <tag>: shallow clone of exactly that tag.
fetch_tag() {
	rm -rf "$2"
	git -c advice.detachedHead=false clone -q --depth 1 --branch "$3" "$1" "$2"
}

install -d -m 0755 "$SRC_DIR"

info "building nqptp ${NQPTP_TAG}"
fetch_tag https://github.com/mikebrady/nqptp.git "${SRC_DIR}/nqptp" "$NQPTP_TAG"
(
	cd "${SRC_DIR}/nqptp"
	autoreconf -fi >/dev/null 2>&1
	./configure --with-systemd-startup >/dev/null
	make -j"$(nproc)" >/dev/null
	make install >/dev/null
)
systemctl daemon-reload
systemctl enable nqptp >/dev/null 2>&1
systemctl restart nqptp
ok "nqptp ${NQPTP_TAG} running (UDP 319/320, shared by every receiver)"

info "building shairport-sync ${SHAIRPORT_TAG} (AirPlay 2, stdout)"
fetch_tag https://github.com/mikebrady/shairport-sync.git "${SRC_DIR}/shairport-sync" "$SHAIRPORT_TAG"
(
	cd "${SRC_DIR}/shairport-sync"
	autoreconf -fi >/dev/null 2>&1
	./configure --sysconfdir=/etc --with-stdout --with-soxr --with-avahi \
		--with-ssl=openssl --with-airplay-2 >/dev/null
	make -j"$(nproc)" >/dev/null
)
# Only the binary: raumfeld-connect starts one instance per room with its
# own generated config, so shairport-sync's service and config are unused.
install -d -m 0755 -o root -g root "$PREFIX"
install -m 0755 -o root -g root "${SRC_DIR}/shairport-sync/shairport-sync" "${PREFIX}/shairport-sync"
ok "$("${PREFIX}/shairport-sync" -V)"

# --- raumfeld-connect -------------------------------------------------------

WORK="$(mktemp -d -t go-raumfeld-connect-install.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

if [ -n "$BINARY" ]; then
	[ -x "$BINARY" ] || die "BINARY=${BINARY} is not an executable file"
	STAGE="$WORK/local"
	install -d "$STAGE"
	cp "$BINARY" "${STAGE}/raumfeld-connect"
	VERSION="$("${STAGE}/raumfeld-connect" --version)"
	ok "using local binary ${BINARY} (${VERSION})"
else
	# Release tags carry a leading 'v', asset filenames do not
	# (go-raumfeld-connect-0.2.0-linux-amd64.tar.gz).
	if [ -z "$VERSION" ]; then
		info "resolving latest release from github.com/${REPO}"
		TAG=$(curl -sSfL "https://api.github.com/repos/${REPO}/releases/latest" \
			| awk -F'"' '/"tag_name":/ {print $4; exit}') || true
		[ -n "${TAG:-}" ] || die "no release found at https://github.com/${REPO}/releases — use BINARY=/path/to/raumfeld-connect"
		VERSION="${TAG#v}"
	else
		VERSION="${VERSION#v}"
		TAG="v${VERSION}"
	fi
	ARCHIVE="go-raumfeld-connect-${VERSION}-linux-${ARCH}.tar.gz"
	base="https://github.com/${REPO}/releases/download/${TAG}"
	info "downloading ${ARCHIVE}"
	if ! curl -sSfL --retry 3 -o "${WORK}/${ARCHIVE}" "${base}/${ARCHIVE}" ||
		! curl -sSfL --retry 3 -o "${WORK}/SHA256SUMS" "${base}/SHA256SUMS"; then
		die "could not download ${base}/${ARCHIVE}"
	fi
	(
		cd "$WORK"
		# Only verify our archive — SHA256SUMS lists every arch.
		grep " ${ARCHIVE}\$" SHA256SUMS \
			| sha256sum --check --strict --status \
			|| { echo "checksum mismatch for ${ARCHIVE}" >&2; exit 1; }
	)
	ok "checksum OK"
	tar -xzf "${WORK}/${ARCHIVE}" -C "$WORK"
	STAGE="${WORK}/go-raumfeld-connect-${VERSION}-linux-${ARCH}"
	[ -x "${STAGE}/raumfeld-connect" ] || die "extracted tree is missing the raumfeld-connect binary"
fi

info "installing to ${PREFIX}"
install -m 0755 -o root -g root "${STAGE}/raumfeld-connect" "${PREFIX}/raumfeld-connect"
for doc in README.md LICENSE NOTICE changelog.md; do
	if [ -f "${STAGE}/${doc}" ]; then
		install -m 0644 -o root -g root "${STAGE}/${doc}" "${PREFIX}/${doc}"
	fi
done
ln -sf "${PREFIX}/raumfeld-connect" "${BIN_LINK_DIR}/raumfeld-connect"
ok "raumfeld-connect ${VERSION} installed, symlink in ${BIN_LINK_DIR}"

# --- service user -----------------------------------------------------------

if id -u "$SERVICE_USER" >/dev/null 2>&1; then
	ok "service user '${SERVICE_USER}' already exists"
else
	useradd --system --no-create-home --shell /usr/sbin/nologin \
		--user-group --comment "go-raumfeld-connect daemon" "$SERVICE_USER"
	ok "user '${SERVICE_USER}' created"
fi

# --- config wizard ----------------------------------------------------------

install -d -m 0755 -o root -g root "$CONFIG_DIR"

if [ -f "$CONFIG_FILE" ]; then
	ok "config already present at ${CONFIG_FILE} — leaving it untouched"
else
	info "no config at ${CONFIG_FILE} — running first-time wizard"
	printf '\n%sThe Raumfeld host is the device that answers on TCP 47365 (e.g. the%s\n' "${DIM}" "${RESET}" > /dev/tty 2>/dev/null || true
	printf '%sCinebar or Connector); give it a fixed DHCP lease.%s\n\n' "${DIM}" "${RESET}" > /dev/tty 2>/dev/null || true
	RAUMFELD_HOST="$(prompt 'Raumfeld host IP or hostname')"
	[ -n "$RAUMFELD_HOST" ] || die "a Raumfeld host is required"

	# Read-only: lists zones and rooms, changes nothing on the speakers.
	rooms_found=""
	if "${PREFIX}/raumfeld-connect" --host "$RAUMFELD_HOST" > "${WORK}/zones.txt" 2>/dev/null; then
		rooms_found=$(sed -n 's/^  room "\(.*\)" power=.*/\1/p' "${WORK}/zones.txt" | paste -sd, -)
		ok "rooms on ${RAUMFELD_HOST}: ${rooms_found:-none}"
	else
		warn "could not read rooms from ${RAUMFELD_HOST} — check the address and the network"
	fi
	RAUMFELD_ROOMS="$(prompt 'Rooms to offer over AirPlay (comma-separated)' "$rooms_found")"
	[ -n "$RAUMFELD_ROOMS" ] || die "at least one room is required"

	tmp_cfg="$(mktemp)"
	cat > "$tmp_cfg" <<EOF
# go-raumfeld-connect service settings; after a change:
#   systemctl restart ${SERVICE_NAME}

# Raumfeld host: the device with TCP 47365.
RAUMFELD_HOST=${RAUMFELD_HOST}

# Comma-separated room names as the Raumfeld app shows them; each becomes
# an AirPlay 2 receiver of the same name (RTSP ports from 7000 upwards).
RAUMFELD_ROOMS=${RAUMFELD_ROOMS}

# Further flags (see: raumfeld-connect --help), e.g.
#   --max-volume 60       zone volume at full AirPlay volume (default 100)
#   --stream-port 59317   TCP port the Raumfeld host fetches audio from
#   --airplay-idle 10s    keep the zone this long after AirPlay stops
#   --advertise <ip>      this machine's address as the speakers reach it
#   --debug
RAUMFELD_ARGS=
EOF
	install -m 0640 -o root -g "$SERVICE_GROUP" "$tmp_cfg" "$CONFIG_FILE"
	rm -f "$tmp_cfg"
	ok "wrote ${CONFIG_FILE} (mode 0640, group ${SERVICE_GROUP})"
fi

# --- systemd unit -----------------------------------------------------------

if [ -f "$SYSTEMD_UNIT" ]; then
	warn "${SYSTEMD_UNIT} exists — backing up to ${SYSTEMD_UNIT}.bak before it is overwritten"
	cp -p "$SYSTEMD_UNIT" "${SYSTEMD_UNIT}.bak"
fi

info "writing systemd unit ${SYSTEMD_UNIT}"
cat > "$SYSTEMD_UNIT" <<EOF
# Generated by go-raumfeld-connect install.sh ${VERSION}
# This file is regenerated on every install/upgrade run and is
# unconditionally overwritten — the previous version was just saved to
# ${SYSTEMD_UNIT}.bak. Put local customizations in a drop-in instead:
# systemctl edit ${SERVICE_NAME}

[Unit]
Description=Raumfeld AirPlay 2 bridge (${SERVICE_NAME})
Documentation=https://github.com/${REPO}
After=network-online.target nqptp.service avahi-daemon.service
Wants=network-online.target
Requires=nqptp.service avahi-daemon.service

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_GROUP}
EnvironmentFile=${CONFIG_FILE}
# \${VAR} keeps a room list with spaces as one argument; \$RAUMFELD_ARGS is
# split into words.
ExecStart=${PREFIX}/raumfeld-connect --host=\${RAUMFELD_HOST} --airplay=\${RAUMFELD_ROOMS} --shairport-sync=${PREFIX}/shairport-sync \$RAUMFELD_ARGS
Restart=on-failure
RestartSec=5
TimeoutStopSec=20
StateDirectory=${SERVICE_NAME}
Environment=HOME=/var/lib/${SERVICE_NAME}

# Hardening — the bridge needs the LAN (UPnP, HTTP, AirPlay), the avahi
# D-Bus socket and nqptp's shared memory in /dev/shm.
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
ProtectClock=yes
LockPersonality=yes
RestrictSUIDSGID=yes
SystemCallArchitectures=native
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
CapabilityBoundingSet=
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
EOF
chmod 0644 "$SYSTEMD_UNIT"
ok "unit installed"

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}" >/dev/null
ok "service enabled"

if systemctl is-active --quiet "${SERVICE_NAME}"; then
	info "restarting ${SERVICE_NAME}"
	systemctl restart "${SERVICE_NAME}"
else
	info "starting ${SERVICE_NAME}"
	systemctl start "${SERVICE_NAME}"
fi

# Give the daemon a moment to either come up or die loudly.
sleep 3

if systemctl is-active --quiet "${SERVICE_NAME}"; then
	ok "${SERVICE_NAME} is running"
else
	warn "${SERVICE_NAME} did not stay up — see: journalctl -u ${SERVICE_NAME} -n 50"
	systemctl status --no-pager "${SERVICE_NAME}" || true
	exit 1
fi

# --- done -------------------------------------------------------------------

cat <<EOF

${GREEN}${BOLD}install complete${RESET} — go-raumfeld-connect ${VERSION} (linux/${ARCH})

  binaries     ${PREFIX}/{raumfeld-connect,shairport-sync}
  nqptp        /usr/local/bin/nqptp (service nqptp)
  config       ${CONFIG_FILE}
  unit         ${SYSTEMD_UNIT}
  service user ${SERVICE_USER}

useful commands:
  systemctl status   ${SERVICE_NAME}
  journalctl -u      ${SERVICE_NAME} -f
  raumfeld-connect --host <ip>               # read-only: zones, rooms, state
  sudo nano ${CONFIG_FILE}                   # rooms, max volume, …
  sudo systemctl restart ${SERVICE_NAME}     # after editing config

network: allow TCP 59317 from the Raumfeld host to this machine.
EOF
