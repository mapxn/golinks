#!/usr/bin/env bash
# golinks installer for macOS.
#
# Usage:
#   scripts/install.sh [--mode http|both|https]
#
# Modes (requirements v2 — listeners are optional configuration):
#   http   HTTP only on port 80. mkcert is NOT installed or touched.
#   both   HTTP + HTTPS. Installs mkcert, creates a local CA and a
#          certificate for "go". HTTP only redirects to HTTPS.
#   https  HTTPS only on port 443. Installs mkcert like "both".
#
# Without --mode the script asks interactively.
set -euo pipefail

LABEL="local.golinks"
BIN_DST="/usr/local/bin/golinks"
ETC_DIR="/usr/local/etc/golinks"
VAR_DIR="/usr/local/var/golinks"
CERT_DIR="$ETC_DIR/certs"
CONFIG="$ETC_DIR/config.json"
PLIST="/Library/LaunchDaemons/$LABEL.plist"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MODE=""

while [ $# -gt 0 ]; do
  case "$1" in
    --mode) MODE="${2:-}"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ "$(uname -s)" != "Darwin" ]; then
  echo "This installer is for macOS only." >&2
  exit 1
fi

if [ -z "$MODE" ]; then
  echo "Choose a listener mode:"
  echo "  1) http   - HTTP only (port 80), no mkcert, simplest"
  echo "  2) both   - HTTP + HTTPS (mkcert local CA), HTTP redirects to HTTPS"
  echo "  3) https  - HTTPS only (port 443), mkcert local CA"
  printf "Mode [1]: "
  read -r choice
  case "$choice" in
    2) MODE="both" ;;
    3) MODE="https" ;;
    *) MODE="http" ;;
  esac
fi

case "$MODE" in
  http)  HTTP_ENABLED=true;  HTTPS_ENABLED=false ;;
  both)  HTTP_ENABLED=true;  HTTPS_ENABLED=true ;;
  https) HTTP_ENABLED=false; HTTPS_ENABLED=true ;;
  *) echo "invalid --mode: $MODE (want http|both|https)" >&2; exit 2 ;;
esac
echo "==> Mode: $MODE (http=$HTTP_ENABLED https=$HTTPS_ENABLED)"

# --- Locate or build the binary -------------------------------------------
ARCH="$(uname -m)"
case "$ARCH" in
  arm64)  DIST_BIN="$ROOT/dist/golinks-darwin-arm64" ;;
  x86_64) DIST_BIN="$ROOT/dist/golinks-darwin-amd64" ;;
  *) echo "unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

BUILD_TMP=""
if [ -x "$DIST_BIN" ]; then
  SRC_BIN="$DIST_BIN"
elif command -v go >/dev/null 2>&1; then
  BUILD_TMP="$(mktemp -d)"
  SRC_BIN="$BUILD_TMP/golinks"
  echo "==> No prebuilt binary found; building from source with Go"
  (cd "$ROOT" && CGO_ENABLED=0 go build -o "$SRC_BIN" .)
else
  echo "No prebuilt binary at $DIST_BIN and Go is not installed." >&2
  echo "Run 'make dist' on a machine with Go, or install Go and retry." >&2
  exit 1
fi

# --- mkcert (only when HTTPS is enabled) -----------------------------------
if [ "$HTTPS_ENABLED" = "true" ]; then
  if ! command -v mkcert >/dev/null 2>&1; then
    if command -v brew >/dev/null 2>&1; then
      echo "==> Installing mkcert via Homebrew"
      brew install mkcert
    else
      echo "mkcert is required for HTTPS but was not found, and Homebrew is unavailable." >&2
      echo "Install mkcert manually (https://github.com/FiloSottile/mkcert) and retry." >&2
      exit 1
    fi
  fi
  echo "==> Installing the mkcert local CA into the system keychain (per-user step)"
  mkcert -install
  TMP_CERTS="$(mktemp -d)"
  echo "==> Issuing a certificate for host 'go'"
  mkcert -cert-file "$TMP_CERTS/go.pem" -key-file "$TMP_CERTS/go-key.pem" go
fi

# --- Install files (root steps) --------------------------------------------
echo "==> Installing binary and directories (sudo)"
sudo install -m 0755 "$SRC_BIN" "$BIN_DST"
sudo mkdir -p "$ETC_DIR" "$VAR_DIR" "$CERT_DIR"

if [ "$HTTPS_ENABLED" = "true" ]; then
  sudo install -m 0644 "$TMP_CERTS/go.pem" "$CERT_DIR/go.pem"
  sudo install -m 0600 "$TMP_CERTS/go-key.pem" "$CERT_DIR/go-key.pem"
  rm -rf "$TMP_CERTS"
fi

echo "==> Writing $CONFIG"
sudo tee "$CONFIG" >/dev/null <<EOF
{
  "http_enabled": $HTTP_ENABLED,
  "https_enabled": $HTTPS_ENABLED,
  "http_port": 80,
  "https_port": 443,
  "cert_file": "$CERT_DIR/go.pem",
  "key_file": "$CERT_DIR/go-key.pem",
  "data_file": "$VAR_DIR/links.json"
}
EOF

# --- hosts entry -------------------------------------------------------------
if grep -Eq '(^|[[:space:]])go([[:space:]]|$)' /etc/hosts; then
  echo "==> /etc/hosts already has a 'go' entry, leaving it unchanged"
else
  echo "==> Adding 'go' to /etc/hosts (sudo)"
  printf '127.0.0.1 go\n::1 go\n' | sudo tee -a /etc/hosts >/dev/null
fi
sudo dscacheutil -flushcache 2>/dev/null || true
sudo killall -HUP mDNSResponder 2>/dev/null || true

# --- launchd daemon ----------------------------------------------------------
echo "==> Installing launchd daemon $LABEL (sudo)"
sudo tee "$PLIST" >/dev/null <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>$LABEL</string>
	<key>ProgramArguments</key>
	<array>
		<string>$BIN_DST</string>
		<string>-config</string>
		<string>$CONFIG</string>
	</array>
	<key>WorkingDirectory</key>
	<string>$VAR_DIR</string>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>$VAR_DIR/golinks.log</string>
	<key>StandardErrorPath</key>
	<string>$VAR_DIR/golinks.err.log</string>
</dict>
</plist>
EOF

sudo launchctl bootout system "$PLIST" 2>/dev/null || true
sudo launchctl bootstrap system "$PLIST"

# --- Verify ------------------------------------------------------------------
sleep 1
HEALTH_OK=""
if [ "$HTTPS_ENABLED" = "true" ]; then
  if curl -fsS "https://go/_admin/health" >/dev/null 2>&1; then HEALTH_OK="https://go"; fi
fi
if [ -z "$HEALTH_OK" ] && [ "$HTTP_ENABLED" = "true" ] && [ "$HTTPS_ENABLED" = "false" ]; then
  if curl -fsS "http://go/_admin/health" >/dev/null 2>&1; then HEALTH_OK="http://go"; fi
fi
# In "both" mode http redirects to https, which the https check above covers.

if [ -n "$HEALTH_OK" ]; then
  echo ""
  echo "Done. Open $HEALTH_OK/ in your browser to manage links, then try $HEALTH_OK/<key>."
else
  echo ""
  echo "Installed, but the health check did not pass yet. Check the logs:" >&2
  echo "  tail -n 50 $VAR_DIR/golinks.err.log" >&2
  echo "If you run a TUN proxy (e.g. Clash), 'go' may be intercepted by fake-ip;" >&2
  echo "see the Clash note in README.md." >&2
  exit 1
fi
