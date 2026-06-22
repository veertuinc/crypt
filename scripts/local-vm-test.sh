#!/usr/bin/env bash
# Build crypt, run unit tests, then exercise a live Anka VM.
#
# Requires a prepared base VM (default: crypt-base) with Remote Login enabled.
# Grok is included in the default agent suite when installed and logged in.
#
# Usage:
#   ./scripts/local-vm-test.sh
#   CRYPT_BASE_VM=crypt-base CRYPT_AGENTS=grok ./scripts/local-vm-test.sh
#   ./scripts/local-vm-test.sh --skip-unit-tests
#
# Environment (passed through to integration-test.sh):
#   CRYPT_BASE_VM        base VM name (default: crypt-base)
#   CRYPT_AGENTS         agents to test (default: claude,grok)
#   CRYPT_SKIP_AGENTS    set to 1 to skip agent suites
#   CRYPT_SKIP_INTERACTIVE  set to 1 to skip SSH/interactive tests
#   CRYPT_AGENT_TIMEOUT  seconds per agent task (default: 300)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

SKIP_UNIT_TESTS=0
INTEGRATION_ARGS=()

while [[ $# -gt 0 ]]; do
	case "$1" in
		--skip-unit-tests)
			SKIP_UNIT_TESTS=1
			shift
			;;
		-h | --help)
			sed -n '2,20p' "$0" | sed 's/^# \?//'
			exit 0
			;;
		*)
			INTEGRATION_ARGS+=("$1")
			shift
			;;
	esac
done

log() { printf '%s\n' "$*"; }

cd "$REPO_ROOT"

log "==> make build"
make build

if [[ "$SKIP_UNIT_TESTS" -eq 0 ]]; then
	log ""
	log "==> make test"
	make test
fi

export CRYPT_BIN="$REPO_ROOT/crypt"
export CRYPT_BASE_VM="${CRYPT_BASE_VM:-crypt-base}"
export CRYPT_AGENTS="${CRYPT_AGENTS:-claude,grok}"

log ""
log "==> live VM integration tests"
log "    CRYPT_BIN=$CRYPT_BIN"
log "    CRYPT_BASE_VM=$CRYPT_BASE_VM"
log "    CRYPT_AGENTS=$CRYPT_AGENTS"

exec "$SCRIPT_DIR/integration-test.sh" "${INTEGRATION_ARGS[@]}"
