#!/usr/bin/env bash
# golinks uninstaller for macOS.
#
# Usage:
#   scripts/uninstall.sh [--purge] [--remove-ca]
#
# Default: stops and removes the daemon and binary, but KEEPS your config
# and links data (/usr/local/etc/golinks, /usr/local/var/golinks) and the
# /etc/hosts entry, so a reinstall picks everything back up.
#   --purge      also delete config, certificates and links data, and remove
#                the 'go' lines from /etc/hosts.
#   --remove-ca  also run `mkcert -uninstall` (removes the local CA from the
#                keychain). Only relevant if you installed with HTTPS.
set -euo pipefail

LABEL="local.golinks"
PLIST="/Library/LaunchDaemons/$LABEL.plist"
PURGE=false
REMOVE_CA=false

while [ $# -gt 0 ]; do
  case "$1" in
    --purge) PURGE=true; shift ;;
    --remove-ca) REMOVE_CA=true; shift ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ "$(uname -s)" != "Darwin" ]; then
  echo "This uninstaller is for macOS only." >&2
  exit 1
fi

echo "==> Stopping daemon (sudo)"
sudo launchctl bootout system "$PLIST" 2>/dev/null || true
sudo rm -f "$PLIST" /usr/local/bin/golinks

if [ "$PURGE" = "true" ]; then
  echo "==> Purging config and data (sudo)"
  sudo rm -rf /usr/local/etc/golinks /usr/local/var/golinks
  echo "==> Removing 'go' lines from /etc/hosts (sudo)"
  sudo sed -i '' -E '/(^|[[:space:]])go([[:space:]]|$)/d' /etc/hosts
  sudo dscacheutil -flushcache 2>/dev/null || true
  sudo killall -HUP mDNSResponder 2>/dev/null || true
else
  echo "Kept: /usr/local/etc/golinks, /usr/local/var/golinks, /etc/hosts entry."
  echo "Run with --purge to remove those too."
fi

if [ "$REMOVE_CA" = "true" ]; then
  if command -v mkcert >/dev/null 2>&1; then
    echo "==> Removing mkcert local CA"
    mkcert -uninstall
  else
    echo "mkcert not found; remove the CA manually from Keychain Access if needed."
  fi
fi

echo "Done."
