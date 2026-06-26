#!/usr/bin/env bash
# Integration tests for crypt against a live Anka host.
#
# Exercises each command/flag combination that matters in practice. Requires:
#   - anka on PATH with a prepared base VM (Remote Login enabled for interactive)
#   - ./crypt built (or set CRYPT_BIN)
#   - agents installed in the base VM (suites skip when missing)
#
# Tests are grouped into lifecycle suites that reuse one clone per working
# directory (crypt's normal session model) instead of provisioning a fresh VM
# for every assertion.
#
# Usage:
#   make integration-test                   # build + run (recommended)
#   ./scripts/integration-test.sh
#   CRYPT_AGENTS=grok ./scripts/integration-test.sh
#   CRYPT_BIN=./crypt CRYPT_BASE_VM=crypt-base ./scripts/integration-test.sh
#
# Environment:
#   CRYPT_BIN          path to crypt (default: repo-root/crypt)
#   CRYPT_BASE_VM      base VM name (default: crypt-base)
#   CRYPT_AGENTS         comma-separated agents to test (default: grok)
#   CRYPT_SKIP_AGENTS    set to 1 to skip all agent tests
#   CRYPT_SKIP_INTERACTIVE  set to 1 to skip SSH/interactive agent tests
#   --no-local tests disabled (--no-local blocks host-to-VM SSH on current Anka builds)
#   IP filter tests require Anka Enterprise (anka modify network -f-)
#   CRYPT_AGENT_TIMEOUT     seconds per agent task (default: 300)
#   CRYPT_INTERACTIVE_TIMEOUT  seconds for interactive sessions (default: 300)
#   CRYPT_SESSION_DIR      session store for this run (default: $WORK_ROOT/sessions)
#   CRYPT_STREAM_OUTPUT      set to 0 to capture output silently (default: 1, stream live)

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

CRYPT_BIN="${CRYPT_BIN:-$REPO_ROOT/crypt}"
CRYPT_BASE_VM="${CRYPT_BASE_VM:-crypt-base}"
CRYPT_AGENT_TIMEOUT="${CRYPT_AGENT_TIMEOUT:-300}"
CRYPT_INTERACTIVE_TIMEOUT="${CRYPT_INTERACTIVE_TIMEOUT:-300}"
CRYPT_AGENT_PROMPT="${CRYPT_AGENT_PROMPT:-Reply with exactly the word CRYPT_OK and nothing else.}"
CRYPT_STREAM_OUTPUT="${CRYPT_STREAM_OUTPUT:-1}"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

WORK_ROOT=""
CURRENT_TEST_DIR=""
CREATED_VM_NAMES=()
CRYPT_LAST_OUTPUT=""
CRYPT_LAST_EXIT=0
INTERACTIVE_OUTPUT_FILE=""
INTERACTIVE_CRYPT_PID=""
INTERACTIVE_TAIL_PID=""
SUITE_SSH_LINE=""
BASE_VM_FILTER_SAVED=""
BASE_VM_FILTER_HAD_RULES=false

log() { printf '%s\n' "$*"; }
die() { log "ERROR: $*"; exit 1; }

run_test() {
	local name="$1"
	shift
	TESTS_RUN=$((TESTS_RUN + 1))
	log ""
	log "==> TEST [$TESTS_RUN]: $name"
	if "$@"; then
		log "PASS: $name"
		TESTS_PASSED=$((TESTS_PASSED + 1))
		return 0
	fi
	log "FAIL: $name"
	if [[ -n "${CRYPT_LAST_OUTPUT:-}" && -f "$CRYPT_LAST_OUTPUT" ]]; then
		log "--- last output ($CRYPT_LAST_OUTPUT) ---"
		tail -40 "$CRYPT_LAST_OUTPUT" >&2 || true
	fi
	TESTS_FAILED=$((TESTS_FAILED + 1))
	log ""
	log "========================================"
	log "Results: $TESTS_PASSED passed, $TESTS_FAILED failed, $TESTS_SKIPPED skipped ($TESTS_RUN total)"
	log "Stopping on first failure."
	exit 1
}

skip_test() {
	local name="$1"
	local reason="$2"
	TESTS_RUN=$((TESTS_RUN + 1))
	TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
	log ""
	log "==> SKIP [$TESTS_RUN]: $name ($reason)"
}

vm_name_from_output() {
	local name=""
	name="$(grep -oE 'crypt: VM name is [^[:space:]]+' "$CRYPT_LAST_OUTPUT" | tail -1 | sed 's/crypt: VM name is //')"
	if [[ -n "$name" ]]; then
		printf '%s' "$name"
		return 0
	fi
	name="$(grep -oE 'crypt: using existing [^[:space:]]+' "$CRYPT_LAST_OUTPUT" | tail -1 | sed 's/crypt: using existing //')"
	printf '%s' "$name"
}

track_vm_name() {
	local name="$1"
	local existing
	[[ -z "$name" || "$name" == "$CRYPT_BASE_VM" ]] && return 0
	for existing in "${CREATED_VM_NAMES[@]:-}"; do
		[[ "$existing" == "$name" ]] && return 0
	done
	CREATED_VM_NAMES+=("$name")
}

track_vm_from_output() {
	track_vm_name "$(vm_name_from_output)"
}

collect_vm_names_from_logs() {
	local log_file name saved_output="${CRYPT_LAST_OUTPUT:-}"
	for log_file in "$WORK_ROOT"/*/crypt-output.txt; do
		[[ -f "$log_file" ]] || continue
		CRYPT_LAST_OUTPUT="$log_file"
		name="$(vm_name_from_output)"
		[[ -n "$name" ]] && track_vm_name "$name"
	done
	CRYPT_LAST_OUTPUT="$saved_output"
}

collect_vm_names_from_sessions() {
	local session_file name
	[[ -n "${CRYPT_SESSION_DIR:-}" && -d "$CRYPT_SESSION_DIR" ]] || return 0
	for session_file in "$CRYPT_SESSION_DIR"/*.name; do
		[[ -f "$session_file" ]] || continue
		name="$(tr -d '[:space:]' < "$session_file")"
		track_vm_name "$name"
	done
}

vm_exists() {
	local name="$1"
	anka show "$name" >/dev/null 2>&1
}

destroy_vm_by_name() {
	local name="$1"
	[[ -z "$name" || "$name" == "$CRYPT_BASE_VM" ]] && return 0
	if ! vm_exists "$name"; then
		return 0
	fi
	log "cleanup: stopping and deleting VM $name"
	anka stop --force "$name" >/dev/null 2>&1 || true
	if anka delete --yes "$name" >/dev/null 2>&1; then
		return 0
	fi
	log "cleanup: warning: failed to delete VM $name"
	return 1
}

base_vm_running() {
	anka show "$CRYPT_BASE_VM" 2>/dev/null | grep -Ei '^\|[[:space:]]*status[[:space:]]+\|' | grep -qi running
}

ensure_base_vm_stopped() {
	if base_vm_running; then
		log "stopping $CRYPT_BASE_VM (Anka cannot clone a running base VM)"
		anka stop --force "$CRYPT_BASE_VM" >/dev/null
	fi
}

save_base_vm_network_filter() {
	BASE_VM_FILTER_SAVED=""
	BASE_VM_FILTER_HAD_RULES=false
	if anka show "$CRYPT_BASE_VM" network -f >/dev/null 2>&1; then
		BASE_VM_FILTER_SAVED="$(anka show "$CRYPT_BASE_VM" network -f 2>/dev/null)"
		BASE_VM_FILTER_HAD_RULES=true
	fi
}

restore_base_vm_network_filter() {
	if [[ "${BASE_VM_FILTER_HAD_RULES:-}" == true && -n "${BASE_VM_FILTER_SAVED:-}" ]]; then
		printf '%s\n' "$BASE_VM_FILTER_SAVED" | anka modify "$CRYPT_BASE_VM" network -f- >/dev/null 2>&1 || true
	else
		anka modify "$CRYPT_BASE_VM" network --filter off >/dev/null 2>&1 || true
	fi
}

ip_filter_supported() {
	ensure_base_vm_stopped
	if ! printf 'pass in from any port 22\n' | anka modify "$CRYPT_BASE_VM" network -f- >/dev/null 2>&1; then
		return 1
	fi
	anka modify "$CRYPT_BASE_VM" network --filter off >/dev/null 2>&1 || true
	return 0
}

# host_unix_nc_supported reports whether the host nc can listen on a UNIX socket,
# which the --socket forwarding test relies on to serve a token to the guest.
host_unix_nc_supported() {
	command -v nc >/dev/null 2>&1 || return 1
	local probe="${WORK_ROOT:-${TMPDIR:-/tmp}}/.nc-probe.sock"
	rm -f "$probe"
	( printf 'x' | nc -lU "$probe" >/dev/null 2>&1 ) &
	local pid=$! waited=0 ok=1
	while [[ ! -S "$probe" && $waited -lt 6 ]]; do
		sleep 0.5
		waited=$((waited + 1))
	done
	[[ -S "$probe" ]] && ok=0
	kill "$pid" 2>/dev/null || true
	wait "$pid" 2>/dev/null || true
	rm -f "$probe"
	return $ok
}

assert_vm_exists() {
	local name="$1"
	vm_exists "$name"
}

assert_vm_gone() {
	local name="$1"
	! vm_exists "$name"
}

wait_for_mount_gone() {
	local vm_name="$1"
	local mount_ref="$2"
	local timeout_sec="${3:-30}"
	local waited=0

	while (( waited < timeout_sec )); do
		if ! anka mount "$vm_name" 2>/dev/null | grep -Fq "$mount_ref"; then
			return 0
		fi
		sleep 1
		waited=$((waited + 1))
	done
	return 1
}

crypt_command_streams_to_terminal() {
	case "${1:-}" in
	claude | codex | codex-fugu | grok | agent)
		case "${2:-}" in
		-h | --help) return 1 ;;
		esac
		return 0
		;;
	esac
	return 1
}

# Interactive agents use ssh -t; run under script(1) so background test jobs still
# have a pseudo-TTY. Background jobs inherit /dev/null on stdin, so keep it open.
run_crypt_interactive() {
	local -a crypt_args=( "$@" )

	if command -v script >/dev/null 2>&1; then
		script -q /dev/null "$CRYPT_BIN" "${crypt_args[@]}" </dev/zero
	else
		"$CRYPT_BIN" "${crypt_args[@]}" </dev/zero
	fi
}

ssh_with_tty() {
	local ssh_line="$1"

	if [[ "$ssh_line" == ssh\ * ]]; then
		printf 'ssh -t %s' "${ssh_line#ssh }"
	else
		printf '%s' "$ssh_line"
	fi
}

# Launch an interactive agent directly over SSH on a kept VM. Crypt suppresses
# lifecycle logs on reuse, so drive the same guest command crypt would run.
launch_guest_agent_interactive() {
	local agent="$1"
	shift
	local -a agent_flags=( "$@" )
	local guest_dir="/Volumes/My Shared Files/$(basename "$CURRENT_TEST_DIR")"
	local ssh_line tty_ssh inner remote_cmd timeout_sec="$CRYPT_INTERACTIVE_TIMEOUT"

	ssh_line="$(ssh_line_for_guest_checks "$CRYPT_LAST_OUTPUT")"
	[[ -n "$ssh_line" ]] || return 1
	tty_ssh="$(ssh_with_tty "$ssh_line")"

	inner="export IS_SANDBOX=1; cd $(printf '%q' "$guest_dir") && exec $(printf '%q' "$agent") --always-approve"
	local flag
	for flag in "${agent_flags[@]}"; do
		inner+=" $(printf '%q' "$flag")"
	done
	remote_cmd="zsh -lc $(printf '%q' "$inner")"

	INTERACTIVE_OUTPUT_FILE="$CURRENT_TEST_DIR/crypt-interactive.txt"
	: >"$INTERACTIVE_OUTPUT_FILE"
	log_live_output_hint
	INTERACTIVE_TAIL_PID="$(start_output_stream "$INTERACTIVE_OUTPUT_FILE")"

	# shellcheck disable=SC2086
	( eval "$tty_ssh $(printf '%q' "$remote_cmd")" </dev/zero ) >>"$INTERACTIVE_OUTPUT_FILE" 2>&1 &
	INTERACTIVE_CRYPT_PID=$!

	if ! wait_for_interactive_agent "$INTERACTIVE_OUTPUT_FILE" "$INTERACTIVE_CRYPT_PID" "$timeout_sec" "$agent"; then
		kill "$INTERACTIVE_CRYPT_PID" 2>/dev/null || true
		wait "$INTERACTIVE_CRYPT_PID" 2>/dev/null || true
		stop_output_stream "${INTERACTIVE_TAIL_PID:-}"
		INTERACTIVE_TAIL_PID=""
		log "interactive output (agent did not launch):"
		tail -40 "$INTERACTIVE_OUTPUT_FILE" >&2 || true
		return 1
	fi
	return 0
}

remember_suite_ssh_line() {
	local ssh_line
	ssh_line="$(ssh_line_from_output "${1:-$CRYPT_LAST_OUTPUT}")"
	if [[ -n "$ssh_line" ]]; then
		SUITE_SSH_LINE="$ssh_line"
	fi
}

log_live_output_hint() {
	log "  ↳ live output below — respond here if the VM or agent asks you to log in"
}

start_output_stream() {
	local output_file="$1"

	[[ "${CRYPT_STREAM_OUTPUT:-1}" == "1" ]] || return 0
	touch "$output_file"
	# Stream new log lines to the terminal while crypt runs in the background.
	tail -n 0 -f "$output_file" >&2 &
	echo $!
}

stop_output_stream() {
	local tail_pid="${1:-}"

	[[ -n "$tail_pid" ]] || return 0
	kill "$tail_pid" 2>/dev/null || true
	wait "$tail_pid" 2>/dev/null || true
}

run_crypt_inner() {
	local -a cmd=( "$@" )

	cd "$CURRENT_TEST_DIR" || exit 1
	if command -v timeout >/dev/null 2>&1; then
		timeout "$CRYPT_AGENT_TIMEOUT" "${cmd[@]}"
	elif command -v gtimeout >/dev/null 2>&1; then
		gtimeout "$CRYPT_AGENT_TIMEOUT" "${cmd[@]}"
	else
		"${cmd[@]}"
	fi
}

run_crypt() {
	ensure_base_vm_stopped

	local output_file="$CURRENT_TEST_DIR/crypt-output.txt"
	local exit_code=0
	local -a cmd=( "$CRYPT_BIN" "$@" )

	if crypt_command_streams_to_terminal "${1:-}"; then
		log_live_output_hint
	fi

	if [[ "${CRYPT_STREAM_OUTPUT:-1}" == "1" ]]; then
		run_crypt_inner "${cmd[@]}" 2>&1 | tee "$output_file"
		exit_code=${PIPESTATUS[0]}
	else
		run_crypt_inner "${cmd[@]}" >"$output_file" 2>&1 || exit_code=$?
	fi

	CRYPT_LAST_OUTPUT="$output_file"
	CRYPT_LAST_EXIT=$exit_code
	track_vm_from_output
	return 0
}

output_contains() {
	local pattern="$1"
	grep -Fq "$pattern" "$CRYPT_LAST_OUTPUT"
}

output_not_contains() {
	local pattern="$1"
	! grep -Fq "$pattern" "$CRYPT_LAST_OUTPUT"
}

output_line_equals() {
	local line="$1"
	tr -d '\r' < "$CRYPT_LAST_OUTPUT" | grep -Fxq "$line"
}

new_test_dir() {
	local suffix="$1"
	CURRENT_TEST_DIR="$WORK_ROOT/$suffix"
	mkdir -p "$CURRENT_TEST_DIR"
}

CLEANUP_DONE=0
INTERRUPTED=0

# Kill background jobs (interactive crypt runs, etc.) then destroy VMs left by tests.
cleanup_on_exit() {
	[[ "$CLEANUP_DONE" == 1 ]] && return 0
	CLEANUP_DONE=1

	if [[ "$INTERRUPTED" == 1 ]]; then
		log ""
		log "Interrupted — cleaning up test VMs..."
	fi

	local job_pid
	for job_pid in $(jobs -p 2>/dev/null); do
		kill -INT "$job_pid" 2>/dev/null || kill -TERM "$job_pid" 2>/dev/null || true
	done
	stop_output_stream "${INTERACTIVE_TAIL_PID:-}"
	INTERACTIVE_TAIL_PID=""
	wait 2>/dev/null || true

	if [[ -n "${WORK_ROOT:-}" && -d "$WORK_ROOT" ]]; then
		cleanup_vms
		rm -rf "$WORK_ROOT"
	fi
}

on_interrupt() {
	INTERRUPTED=1
	cleanup_on_exit
	exit 130
}

install_cleanup_trap() {
	trap cleanup_on_exit EXIT
	trap on_interrupt INT TERM
}

cleanup_vms() {
	local test_dir name

	collect_vm_names_from_logs
	collect_vm_names_from_sessions

	# crypt destroy reads the per-directory session file when available.
	for test_dir in "$WORK_ROOT"/*/; do
		[[ -d "$test_dir" ]] || continue
		( cd "$test_dir" && "$CRYPT_BIN" destroy ) >/dev/null 2>&1 || true
	done

	for name in "${CREATED_VM_NAMES[@]:-}"; do
		destroy_vm_by_name "$name"
	done
}

check_prerequisites() {
	command -v anka >/dev/null 2>&1 || die "anka not found on PATH"
	command -v ssh-keygen >/dev/null 2>&1 || die "ssh-keygen not found on PATH"
	[[ -x "$CRYPT_BIN" || -n "$(command -v "$CRYPT_BIN" 2>/dev/null)" ]] || die "crypt binary not found: $CRYPT_BIN"
	anka list 2>/dev/null | grep -Fq "$CRYPT_BASE_VM" || die "base VM $CRYPT_BASE_VM not found (anka list)"
	ensure_base_vm_stopped
}

agent_installed() {
	local agent="$1"
	local started_base=false

	if ! base_vm_running; then
		anka start "$CRYPT_BASE_VM" >/dev/null 2>&1 || return 1
		started_base=true
	fi

	if ! anka run "$CRYPT_BASE_VM" zsh -lc "command -v $(printf '%q' "$agent")" >/dev/null 2>&1; then
		[[ "$started_base" == true ]] && anka stop --force "$CRYPT_BASE_VM" >/dev/null 2>&1 || true
		return 1
	fi

	if [[ "$started_base" == true ]]; then
		anka stop --force "$CRYPT_BASE_VM" >/dev/null 2>&1 || true
	fi
	return 0
}

# --- individual tests ---

test_version() {
	run_crypt --version
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "crypt"
}

test_help() {
	run_crypt --help
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "claude" && output_contains "destroy"
}

test_subcommand_help() {
	local sub="$1"
	run_crypt "$sub" --help
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]]
}

test_run_lifecycle() {
	local marker=".crypt-mount-marker"
	local guest_path="/Volumes/My Shared Files/$(basename "$CURRENT_TEST_DIR")/$marker"
	local vm_name

	echo mount-ok >"$CURRENT_TEST_DIR/$marker"

	run_crypt run --cpu 2 --memory 4096 -- /bin/echo CRYPT_RESOURCES_OK
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] &&
		output_contains "CRYPT_RESOURCES_OK" &&
		output_contains "crypt: SSH: ssh " &&
		output_contains "crypt: VNC: open vnc://" || return 1
	vm_name="$(vm_name_from_output)"
	[[ -n "$vm_name" ]] && assert_vm_exists "$vm_name" || return 1

	run_crypt run -- /bin/echo CRYPT_RUN_OK
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "CRYPT_RUN_OK" || return 1

	run_crypt run --mount . -- /bin/cat "$guest_path"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "mount-ok" || return 1
	wait_for_mount_gone "$vm_name" "$CURRENT_TEST_DIR" || return 1

	run_crypt destroy
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "destroying" && assert_vm_gone "$vm_name"
}

test_run_destroy_and_stderr() {
	run_crypt run --destroy -- /bin/echo CRYPT_DESTROY_OK
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "CRYPT_DESTROY_OK" || return 1

	run_crypt run --destroy -- /bin/sh -c 'echo CRYPT_STDERR_TEST >&2; exit 1'
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] &&
		output_contains "CRYPT_STDERR_TEST" &&
		output_contains "exited:" || return 1
}

test_run_named_lifecycle() {
	local vm_name="crypt-it-named-$$"

	run_crypt --name "$vm_name" run --destroy -- /bin/echo CRYPT_NAMED_OK
	track_vm_name "$vm_name"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "CRYPT_NAMED_OK" && assert_vm_gone "$vm_name" || return 1

	run_crypt --name "$vm_name" run -- /bin/echo keep
	track_vm_name "$vm_name"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && assert_vm_exists "$vm_name" || return 1
	(
		cd "$CURRENT_TEST_DIR"
		"$CRYPT_BIN" --name "$vm_name" destroy
	) >"$CURRENT_TEST_DIR/destroy.log" 2>&1
	[[ "$?" -eq 0 ]] && assert_vm_gone "$vm_name"
}

test_run_env() {
	run_crypt run --env CRYPT_TEST_ENV=env-ok -- /bin/zsh -lc 'printenv CRYPT_TEST_ENV'
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_line_equals "env-ok" || return 1

	run_crypt run --env A=alpha --env B=beta -- /bin/zsh -lc 'printenv A; printenv B'
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] &&
		output_line_equals "alpha" &&
		output_line_equals "beta" || return 1

	run_crypt run -- /bin/zsh -lc 'printenv IS_SANDBOX'
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_line_equals "1" || return 1

	run_crypt run --env NOTVALID -- /bin/echo fail
	[[ "$CRYPT_LAST_EXIT" -ne 0 ]] && output_contains "invalid --env" || return 1

	run_crypt destroy
}

# Forwards a host UNIX socket into the guest and proves the guest can talk to it
# end to end over the SSH tunnel (ssh -R), then checks a bad spec fails fast.
test_run_socket() {
	local host_sock="$CURRENT_TEST_DIR/host.sock"
	local guest_sock="/tmp/crypt-it-socket-$$.sock"
	local listener_pid=""

	rm -f "$host_sock"
	# One-shot host listener: hand the known token to the relay connection the
	# host-side ssh makes when the guest opens the forwarded socket.
	( printf 'CRYPT_SOCKET_OK\n' | nc -lU "$host_sock" >/dev/null 2>&1 ) &
	listener_pid=$!

	local waited=0
	while [[ ! -S "$host_sock" && $waited -lt 10 ]]; do
		sleep 0.5
		waited=$((waited + 1))
	done
	if [[ ! -S "$host_sock" ]]; then
		kill "$listener_pid" 2>/dev/null || true
		wait "$listener_pid" 2>/dev/null || true
		log "host UNIX socket listener did not start"
		return 1
	fi

	# The guest sees the socket at $CRYPT_SOCK (auto-exported by the ENVVAR field)
	# and reads the token through it. The piped sleep keeps the client open long
	# enough to receive the server's reply before EOF.
	run_crypt run --socket "$host_sock:$guest_sock:CRYPT_SOCK" -- \
		/bin/zsh -lc 'test -S "$CRYPT_SOCK" && printf "GUEST_SOCK_OK %s\n" "$CRYPT_SOCK"; { sleep 1; } | nc -U "$CRYPT_SOCK"'

	kill "$listener_pid" 2>/dev/null || true
	wait "$listener_pid" 2>/dev/null || true

	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] &&
		output_contains "crypt: forwarding host socket" &&
		output_contains "GUEST_SOCK_OK $guest_sock" &&
		output_contains "CRYPT_SOCKET_OK" || return 1

	# A non-absolute guest path is rejected before any VM work happens.
	run_crypt run --socket "$host_sock:relative-guest" -- /bin/echo fail
	[[ "$CRYPT_LAST_EXIT" -ne 0 ]] && output_contains "must be absolute" || return 1

	run_crypt destroy
}

test_agent_lifecycle() {
	local agent="$1"
	local vm_name

	if [[ "$agent" == "grok" ]]; then
		run_crypt grok 'who are you?'
		[[ "$CRYPT_LAST_EXIT" -eq 0 ]] || return 1
		output_not_contains "exited:" || return 1
		output_not_contains "no output from grok" || return 1
		grep -qvE '^crypt:' "$CRYPT_LAST_OUTPUT" || return 1
	else
		run_crypt "$agent" "$CRYPT_AGENT_PROMPT"
		[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "CRYPT_OK" || return 1
	fi
	vm_name="$(vm_name_from_output)"
	[[ -n "$vm_name" ]] && assert_vm_exists "$vm_name" || return 1
	remember_suite_ssh_line

	run_crypt "$agent" "$CRYPT_AGENT_PROMPT"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] &&
		output_contains "CRYPT_OK" &&
		output_not_contains "reusing existing clone" &&
		output_not_contains "already running" &&
		output_not_contains "launching " &&
		output_not_contains "kept VM" &&
		assert_vm_exists "$vm_name" || return 1

	echo CRYPT_OK >"$CURRENT_TEST_DIR/marker.txt"
	run_crypt "$agent" --mount . "Read marker.txt in the current directory and reply with its exact contents only."
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "CRYPT_OK" || return 1

	if [[ "$agent" == "grok" && "${CRYPT_SKIP_INTERACTIVE:-}" != "1" ]]; then
		verify_grok_interactive_mount_flags_on_kept_vm || return 1
	fi

	run_crypt destroy
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "destroying" && assert_vm_gone "$vm_name" || return 1

	run_crypt "$agent" --destroy "$CRYPT_AGENT_PROMPT"
	vm_name="$(vm_name_from_output)"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] &&
		output_contains "CRYPT_OK" &&
		[[ -n "$vm_name" ]] &&
		assert_vm_gone "$vm_name"
}

ssh_line_from_output() {
	local output_file="$1"
	grep 'crypt: SSH: ' "$output_file" | tail -1 | sed 's/^crypt: SSH: //'
}

assert_no_interactive_ssh_failures() {
	local output_file="$1"
	local pattern

	for pattern in \
		'Identity file -t not accessible' \
		'hostname contains invalid characters' \
		'exited: exit status 255'
	do
		if grep -Fq "$pattern" "$output_file"; then
			return 1
		fi
	done
	if grep -qE 'crypt: [^[:space:]]+ exited:' "$output_file"; then
		return 1
	fi
	return 0
}

ssh_line_for_guest_checks() {
	local output_file="${1:-$INTERACTIVE_OUTPUT_FILE}"
	local ssh_line

	ssh_line="$(ssh_line_from_output "$output_file")"
	if [[ -n "$ssh_line" ]]; then
		printf '%s' "$ssh_line"
		return 0
	fi
	if [[ -n "${SUITE_SSH_LINE:-}" ]]; then
		printf '%s' "$SUITE_SSH_LINE"
	fi
}

guest_command_via_crypt_ssh() {
	local output_file="$1"
	local remote_cmd="$2"
	local ssh_line

	ssh_line="$(ssh_line_for_guest_checks "$output_file")"
	[[ -n "$ssh_line" ]] || return 1
	# shellcheck disable=SC2086
	eval "$ssh_line $(printf '%q' "$remote_cmd")"
}

wait_for_interactive_agent() {
	local output_file="$1"
	local crypt_pid="$2"
	local timeout_sec="$3"
	local agent="$4"
	local waited=0

	while (( waited < timeout_sec )); do
		if grep -qE 'launching ' "$output_file" 2>/dev/null; then
			return 0
		fi
		if kill -0 "$crypt_pid" 2>/dev/null; then
			if guest_command_via_crypt_ssh "$output_file" "pgrep -fl $(printf '%q' "$agent")" >/dev/null 2>&1; then
				return 0
			fi
		else
			return 1
		fi
		sleep 1
		waited=$((waited + 1))
	done
	return 1
}

stop_interactive_crypt() {
	local crypt_pid="$1"
	local waited=0

	stop_output_stream "${INTERACTIVE_TAIL_PID:-}"
	INTERACTIVE_TAIL_PID=""

	kill -INT "$crypt_pid" 2>/dev/null || true
	while (( waited < 120 )); do
		if ! kill -0 "$crypt_pid" 2>/dev/null; then
			break
		fi
		sleep 2
		waited=$((waited + 2))
	done
	wait "$crypt_pid" 2>/dev/null || true
}

assert_guest_agent_running() {
	local output_file="$1"
	local agent="$2"
	local proc

	proc="$(guest_command_via_crypt_ssh "$output_file" "pgrep -fl $(printf '%q' "$agent")")" || return 1
	[[ -n "$proc" ]]
}

assert_grok_interactive_flags() {
	local output_file="$1"
	shift
	local -a expected_args=("$@")
	local proc arg

	proc="$(guest_command_via_crypt_ssh "$output_file" 'pgrep -fl grok')" || return 1
	[[ -n "$proc" ]] || return 1

	for arg in "${expected_args[@]}"; do
		[[ "$proc" == *"$arg"* ]] || return 1
	done

	# Flag-only interactive runs must not pick up task-mode injection.
	[[ "$proc" != *"--output-format plain"* ]] || return 1
	echo "$proc" | grep -qE '(^|[[:space:]])-p[[:space:]]' && return 1
	return 0
}

# Runs an interactive agent in the background until the guest agent process is
# up. Reused VMs suppress the "launching" log line, so also probe over SSH.
# Sets INTERACTIVE_OUTPUT_FILE and INTERACTIVE_CRYPT_PID.
start_interactive_agent() {
	local agent="$1"
	shift
	local -a crypt_args=( "$agent" "$@" )
	local timeout_sec="$CRYPT_INTERACTIVE_TIMEOUT"

	INTERACTIVE_OUTPUT_FILE="$CURRENT_TEST_DIR/crypt-output.txt"
	ensure_base_vm_stopped
	: >"$INTERACTIVE_OUTPUT_FILE"

	log_live_output_hint
	INTERACTIVE_TAIL_PID="$(start_output_stream "$INTERACTIVE_OUTPUT_FILE")"

	( cd "$CURRENT_TEST_DIR" && run_crypt_interactive "${crypt_args[@]}" ) >>"$INTERACTIVE_OUTPUT_FILE" 2>&1 &
	INTERACTIVE_CRYPT_PID=$!

	if ! wait_for_interactive_agent "$INTERACTIVE_OUTPUT_FILE" "$INTERACTIVE_CRYPT_PID" "$timeout_sec" "$agent"; then
		kill "$INTERACTIVE_CRYPT_PID" 2>/dev/null || true
		wait "$INTERACTIVE_CRYPT_PID" 2>/dev/null || true
		stop_output_stream "${INTERACTIVE_TAIL_PID:-}"
		INTERACTIVE_TAIL_PID=""
		log "interactive output (agent did not launch):"
		tail -40 "$INTERACTIVE_OUTPUT_FILE" >&2 || true
		return 1
	fi
	return 0
}

# Verifies SSH connected and the agent process is running in the guest.
verify_interactive_agent_ssh() {
	local agent="$1"
	local output_file="$INTERACTIVE_OUTPUT_FILE"

	if ! assert_no_interactive_ssh_failures "$output_file"; then
		log "interactive output (SSH or agent failed after launch):"
		tail -40 "$output_file" >&2 || true
		return 1
	fi
	if ! assert_guest_agent_running "$output_file" "$agent"; then
		log "interactive output (agent process not running in guest):"
		tail -40 "$output_file" >&2 || true
		return 1
	fi
	return 0
}

# Stops crypt and destroys the kept clone from an interactive run.
finish_interactive_agent_test() {
	local output_file="$INTERACTIVE_OUTPUT_FILE"
	local crypt_pid="$INTERACTIVE_CRYPT_PID"

	stop_interactive_crypt "$crypt_pid"

	CRYPT_LAST_OUTPUT="$output_file"
	track_vm_from_output
	local vm_name
	vm_name="$(vm_name_from_output)"
	if [[ -z "$vm_name" ]]; then
		log "interactive output (missing VM name):"
		tail -40 "$output_file" >&2 || true
		return 1
	fi
	if ! assert_vm_exists "$vm_name"; then
		log "interactive output (VM missing):"
		tail -40 "$output_file" >&2 || true
		return 1
	fi
	(
		cd "$CURRENT_TEST_DIR" || exit 1
		"$CRYPT_BIN" destroy
	) >/dev/null 2>&1
	assert_vm_gone "$vm_name"
}

run_interactive_agent_test() {
	local agent="$1"
	shift

	start_interactive_agent "$agent" "$@" || return 1
	verify_interactive_agent_ssh "$agent" || {
		stop_interactive_crypt "$INTERACTIVE_CRYPT_PID"
		return 1
	}
	finish_interactive_agent_test
}

test_agent_interactive() {
	run_interactive_agent_test "$1"
}

verify_grok_interactive_mount_flags_on_kept_vm() {
	launch_guest_agent_interactive grok --reasoning-effort high || return 1
	assert_grok_interactive_flags "$INTERACTIVE_OUTPUT_FILE" --reasoning-effort high || {
		stop_interactive_crypt "$INTERACTIVE_CRYPT_PID"
		return 1
	}
	stop_interactive_crypt "$INTERACTIVE_CRYPT_PID"
}

test_ip_filter_ssh_allowed() {
	save_base_vm_network_filter
	trap restore_base_vm_network_filter RETURN

	cat <<'EOF' | anka modify "$CRYPT_BASE_VM" network -f-
pass in from any port 22
pass out to any
block in from any port 80
EOF

	run_crypt run --destroy -- /bin/echo CRYPT_IPFILTER_OK
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] &&
		output_contains "CRYPT_IPFILTER_OK" &&
		output_contains "IP filtering rules are enabled"
}

# Disabled: --no-local blocks host-to-VM SSH on current Anka builds.
# test_no_local() {
# 	run_crypt run --no-local --destroy -- /bin/echo CRYPT_NOLOCAL_OK
# 	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "CRYPT_NOLOCAL_OK" && output_contains "no-local"
# }

run_agent_suite() {
	local agent="$1"
	if [[ "${CRYPT_SKIP_AGENTS:-}" == "1" ]]; then
		skip_test "$agent/*" "CRYPT_SKIP_AGENTS=1"
		return 0
	fi
	if ! agent_installed "$agent"; then
		skip_test "$agent/*" "$agent not installed in $CRYPT_BASE_VM"
		return 0
	fi

	new_test_dir "agent-${agent}"
	SUITE_SSH_LINE=""
	if [[ "${CRYPT_SKIP_INTERACTIVE:-}" == "1" || "$agent" != "grok" ]]; then
		run_test "$agent lifecycle (task, reuse, mount, destroy)" test_agent_lifecycle "$agent"
	else
		run_test "$agent lifecycle (task, reuse, mount, interactive, destroy)" test_agent_lifecycle "$agent"
	fi

	if [[ "${CRYPT_SKIP_INTERACTIVE:-}" == "1" ]]; then
		skip_test "$agent interactive (SSH)" "CRYPT_SKIP_INTERACTIVE=1"
	elif [[ "$agent" == "grok" ]]; then
		: # covered by lifecycle on the kept VM
	else
		new_test_dir "interactive-${agent}"
		run_test "$agent interactive (SSH, kept)" test_agent_interactive "$agent"
	fi
}

main() {
	check_prerequisites

	if [[ ! -x "$CRYPT_BIN" ]]; then
		log "building $CRYPT_BIN (make build)"
		( cd "$REPO_ROOT" && make build ) || die "make build failed"
	fi

	WORK_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/crypt-it.XXXXXX")"
	CURRENT_TEST_DIR="$WORK_ROOT/_cli"
	export CRYPT_SESSION_DIR="${CRYPT_SESSION_DIR:-$WORK_ROOT/sessions}"
	mkdir -p "$CURRENT_TEST_DIR" "$CRYPT_SESSION_DIR"
	install_cleanup_trap

	log "crypt integration tests"
	log "  CRYPT_BIN=$CRYPT_BIN"
	log "  CRYPT_BASE_VM=$CRYPT_BASE_VM"
	log "  CRYPT_AGENTS=${CRYPT_AGENTS:-grok}"
	log "  CRYPT_SESSION_DIR=$CRYPT_SESSION_DIR"
	log "  WORK_ROOT=$WORK_ROOT"

	# CLI smoke tests (no VM run)
	run_test "crypt --version" test_version
	run_test "crypt --help" test_help
	run_test "crypt claude --help" test_subcommand_help claude
	run_test "crypt grok --help" test_subcommand_help grok
	run_test "crypt run --help" test_subcommand_help run
	run_test "crypt destroy --help" test_subcommand_help destroy

	# crypt run combinations (each test reuses one clone where possible)
	new_test_dir "run-lifecycle"
	run_test "run lifecycle (resources, keep, mount, destroy)" test_run_lifecycle

	new_test_dir "run-destroy"
	run_test "run --destroy and guest stderr" test_run_destroy_and_stderr

	new_test_dir "run-named"
	run_test "run --name and destroy --name" test_run_named_lifecycle

	new_test_dir "run-env"
	run_test "run --env exports guest variables" test_run_env

	if host_unix_nc_supported; then
		new_test_dir "run-socket"
		run_test "run --socket forwards a host UNIX socket" test_run_socket
	else
		skip_test "run --socket forwards a host UNIX socket" "host nc lacks UNIX socket support"
	fi

	if ip_filter_supported; then
		new_test_dir "run-ipfilter"
		run_test "run with IP filter rules (SSH allowed)" test_ip_filter_ssh_allowed
	else
		skip_test "run with IP filter rules" "Anka Enterprise required or anka modify network -f- unavailable"
	fi

	# new_test_dir "run-nolocal"
	# run_test "run --no-local" test_no_local

	# Agent suites (installed agents in CRYPT_BASE_VM; override with CRYPT_AGENTS)
	local agents=()
	if [[ -n "${CRYPT_AGENTS:-}" ]]; then
		IFS=',' read -r -a agents <<< "$CRYPT_AGENTS"
	else
		agents=(grok)
	fi
	local agent
	for agent in "${agents[@]}"; do
		agent="${agent#"${agent%%[![:space:]]*}"}"
		agent="${agent%"${agent##*[![:space:]]}"}"
		[[ -n "$agent" ]] || continue
		run_agent_suite "$agent"
	done

	log ""
	log "========================================"
	log "Results: $TESTS_PASSED passed, $TESTS_FAILED failed, $TESTS_SKIPPED skipped ($TESTS_RUN total)"
	if [[ "$TESTS_FAILED" -gt 0 ]]; then
		exit 1
	fi
}

main "$@"
