#!/usr/bin/env bash
set -euo pipefail

binary="${1:?binary path required}"
is_snapshot="${2:-false}"

if [[ "$is_snapshot" == "true" ]]; then
  echo "Snapshot build: ad-hoc signing only"
  codesign --sign - --force "$binary"
  exit 0
fi

if [[ -z "${NOTARIZATION_USERNAME:-}" || -z "${NOTARIZATION_PASSWORD:-}" ]]; then
  echo "NOTARIZATION_USERNAME and NOTARIZATION_PASSWORD are required for release builds"
  exit 1
fi

if [[ -z "${KEYCHAIN_PATH:-}" ]]; then
  echo "KEYCHAIN_PATH is required for release builds"
  exit 1
fi

identity="$(
  security find-identity -v -p codesigning "$KEYCHAIN_PATH" \
    | awk -F'"' '/Developer ID Application/ { print $2; exit }'
)"
if [[ -z "$identity" ]]; then
  echo "No Developer ID Application identity found in keychain"
  exit 1
fi

team_id="${NOTARIZATION_TEAM_ID:-}"
if [[ -z "$team_id" ]]; then
  team_id="$(
    security find-certificate -a -c "Developer ID Application" -p "$KEYCHAIN_PATH" \
      | openssl x509 -noout -subject 2>/dev/null \
      | sed -n 's/.*OU=\([^/]*\).*/\1/p' \
      | head -1
  )"
fi
if [[ -z "$team_id" ]]; then
  echo "Could not determine Apple Team ID; set NOTARIZATION_TEAM_ID"
  exit 1
fi

codesign \
  --sign "$identity" \
  --force \
  --options runtime \
  --timestamp \
  --keychain "$KEYCHAIN_PATH" \
  "$binary"

zip_path="${binary}.zip"
(cd "$(dirname "$binary")" && zip -q -j "$zip_path" "$(basename "$binary")")
trap 'rm -f "$zip_path"' EXIT

xcrun notarytool submit "$zip_path" \
  --apple-id "$NOTARIZATION_USERNAME" \
  --password "$NOTARIZATION_PASSWORD" \
  --team-id "$team_id" \
  --wait \
  --timeout 30m

# Stapling only works for .app, .pkg, and .dmg — not bare CLI binaries.
# Notarization is still valid; Gatekeeper verifies the ticket online on first run.
echo "Notarization accepted for $(basename "$binary")"
