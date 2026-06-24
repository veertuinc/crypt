#!/usr/bin/env bash
# Integration tests for crypt against a live Anka host.
#
# Exercises each command/flag combination that matters in practice. Requires:
#   - anka on PATH with a prepared base VM (Remote Login enabled for interactive)
#   - ./crypt built (or set CRYPT_BIN)
#   - agents installed in the base VM (suites skip when missing)
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

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

CRYPT_BIN="${CRYPT_BIN:-$REPO_ROOT/crypt}"
CRYPT_BASE_VM="${CRYPT_BASE_VM:-crypt-base}"
CRYPT_AGENT_TIMEOUT="${CRYPT_AGENT_TIMEOUT:-300}"
CRYPT_INTERACTIVE_TIMEOUT="${CRYPT_INTERACTIVE_TIMEOUT:-300}"
CRYPT_AGENT_PROMPT="${CRYPT_AGENT_PROMPT:-Reply with exactly the word CRYPT_OK and nothing else.}"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

WORK_ROOT=""
CURRENT_TEST_DIR=""
CREATED_VM_NAMES=()
CRYPT_LAST_OUTPUT=""
CRYPT_LAST_EXIT=0
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
	return 1
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

run_crypt() {
	ensure_base_vm_stopped

	local output_file="$CURRENT_TEST_DIR/crypt-output.txt"
	local exit_code=0
	local -a cmd=( "$CRYPT_BIN" "$@" )

	if command -v timeout >/dev/null 2>&1; then
		(
			cd "$CURRENT_TEST_DIR" || exit 1
			timeout "$CRYPT_AGENT_TIMEOUT" "${cmd[@]}"
		) >"$output_file" 2>&1 || exit_code=$?
	elif command -v gtimeout >/dev/null 2>&1; then
		(
			cd "$CURRENT_TEST_DIR" || exit 1
			gtimeout "$CRYPT_AGENT_TIMEOUT" "${cmd[@]}"
		) >"$output_file" 2>&1 || exit_code=$?
	else
		(
			cd "$CURRENT_TEST_DIR" || exit 1
			"${cmd[@]}"
		) >"$output_file" 2>&1 || exit_code=$?
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

test_run_echo() {
	local flags=("$@")
	run_crypt run "${flags[@]}" -- /bin/echo CRYPT_RUN_OK
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "CRYPT_RUN_OK"
}

test_run_default_keeps_vm() {
	run_crypt run -- /bin/echo CRYPT_RUN_OK
	local vm_name
	vm_name="$(vm_name_from_output)"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] &&
		output_contains "CRYPT_RUN_OK" &&
		output_contains "crypt: SSH: ssh " &&
		output_contains "crypt: VNC: open vnc://" &&
		[[ -n "$vm_name" ]] &&
		assert_vm_exists "$vm_name" || return 1
	run_crypt destroy
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && assert_vm_gone "$vm_name"
}

test_run_mount() {
	local marker=".crypt-mount-marker"
	echo mount-ok >"$CURRENT_TEST_DIR/$marker"
	local guest_path="/Volumes/My Shared Files/$(basename "$CURRENT_TEST_DIR")/$marker"
	run_crypt run --mount --destroy -- /bin/cat "$guest_path"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "mount-ok"
}

test_run_mount_unmounts_for_running_vm() {
	local marker=".crypt-temp-mount-marker"
	echo mount-ok >"$CURRENT_TEST_DIR/$marker"
	local guest_dir="/Volumes/My Shared Files/$(basename "$CURRENT_TEST_DIR")"
	local guest_path="$guest_dir/$marker"

	run_crypt run -- /bin/echo keep
	local vm_name
	vm_name="$(vm_name_from_output)"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && [[ -n "$vm_name" ]] && assert_vm_exists "$vm_name" || return 1

	run_crypt run --mount -- /bin/cat "$guest_path"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "mount-ok" || return 1

	wait_for_mount_gone "$vm_name" "$CURRENT_TEST_DIR" || {
		run_crypt destroy
		return 1
	}
	run_crypt destroy
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && assert_vm_gone "$vm_name"
}

test_run_named() {
	local vm_name="crypt-it-named-$$"
	run_crypt --name "$vm_name" run --destroy -- /bin/echo CRYPT_NAMED_OK
	track_vm_name "$vm_name"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "CRYPT_NAMED_OK" && assert_vm_gone "$vm_name"
}

test_agent_task() {
	local agent="$1"
	shift
	local flags=("$@")
	if [[ " ${flags[*]} " == *" --mount "* ]]; then
		echo CRYPT_OK >"$CURRENT_TEST_DIR/marker.txt"
		run_crypt "$agent" --mount "Read marker.txt in the current directory and reply with its exact contents only."
	else
		run_crypt "$agent" "${flags[@]}" "$CRYPT_AGENT_PROMPT"
	fi
	local vm_name
	vm_name="$(vm_name_from_output)"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "CRYPT_OK" && [[ -n "$vm_name" ]] || return 1
	run_crypt destroy
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && assert_vm_gone "$vm_name"
}

test_agent_task_keeps_vm() {
	local agent="$1"
	run_crypt "$agent" "$CRYPT_AGENT_PROMPT"
	local vm_name
	vm_name="$(vm_name_from_output)"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "CRYPT_OK" && [[ -n "$vm_name" ]] && assert_vm_exists "$vm_name" || return 1
	run_crypt destroy
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && assert_vm_gone "$vm_name"
}

test_agent_task_destroy() {
	local agent="$1"
	run_crypt "$agent" --destroy "$CRYPT_AGENT_PROMPT"
	local vm_name
	vm_name="$(vm_name_from_output)"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "CRYPT_OK" && [[ -n "$vm_name" ]] && assert_vm_gone "$vm_name"
}

test_agent_reuse_vm() {
	local agent="$1"
	run_crypt "$agent" "$CRYPT_AGENT_PROMPT"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] || return 1
	local vm_name
	vm_name="$(vm_name_from_output)"
	[[ -n "$vm_name" ]] || return 1
	run_crypt "$agent" "$CRYPT_AGENT_PROMPT"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] &&
		output_contains "CRYPT_OK" &&
		output_not_contains "reusing existing clone" &&
		output_not_contains "already running" &&
		output_not_contains "launching " &&
		output_not_contains "kept VM" &&
		assert_vm_exists "$vm_name" || return 1
	run_crypt destroy
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && assert_vm_gone "$vm_name"
}

test_agent_interactive() {
	local agent="$1"
	local output_file="$CURRENT_TEST_DIR/crypt-output.txt"
	local timeout_sec="$CRYPT_INTERACTIVE_TIMEOUT"
	local waited=0

	ensure_base_vm_stopped
	: >"$output_file"

	( cd "$CURRENT_TEST_DIR" && "$CRYPT_BIN" "$agent" ) >>"$output_file" 2>&1 &
	local crypt_pid=$!

	while (( waited < timeout_sec )); do
		if grep -qE 'launching ' "$output_file" 2>/dev/null; then
			break
		fi
		if ! kill -0 "$crypt_pid" 2>/dev/null; then
			break
		fi
		sleep 2
		waited=$((waited + 2))
	done

	if ! grep -qE 'launching ' "$output_file" 2>/dev/null; then
		kill "$crypt_pid" 2>/dev/null || true
		wait "$crypt_pid" 2>/dev/null || true
		log "interactive output (agent did not launch):"
		tail -40 "$output_file" >&2 || true
		return 1
	fi

	# Let the SSH session attach, then interrupt. Interactive runs are kept until
	# the user explicitly runs crypt destroy.
	sleep 5
	kill -INT "$crypt_pid" 2>/dev/null || true

	waited=0
	while (( waited < 120 )); do
		if ! kill -0 "$crypt_pid" 2>/dev/null; then
			break
		fi
		sleep 2
		waited=$((waited + 2))
	done

	wait "$crypt_pid" 2>/dev/null || true

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

test_destroy_command() {
	local agent="$1"
	run_crypt "$agent" "$CRYPT_AGENT_PROMPT"
	local vm_name
	vm_name="$(vm_name_from_output)"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && [[ -n "$vm_name" ]] && assert_vm_exists "$vm_name" || return 1
	run_crypt destroy
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "destroying" && assert_vm_gone "$vm_name"
}

test_destroy_named() {
	local vm_name="crypt-it-destroy-$$"
	run_crypt --name "$vm_name" run -- /bin/echo keep
	track_vm_name "$vm_name"
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && assert_vm_exists "$vm_name" || return 1
	(
		cd "$CURRENT_TEST_DIR"
		"$CRYPT_BIN" --name "$vm_name" destroy
	) >"$CURRENT_TEST_DIR/destroy.log" 2>&1
	[[ "$?" -eq 0 ]] && assert_vm_gone "$vm_name"
}

test_run_cpu_memory() {
	run_crypt run --cpu 2 --memory 4096 --destroy -- /bin/echo CRYPT_RESOURCES_OK
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "CRYPT_RESOURCES_OK"
}

# Disabled: --no-local blocks host-to-VM SSH on current Anka builds.
# test_no_local() {
# 	run_crypt run --no-local --destroy -- /bin/echo CRYPT_NOLOCAL_OK
# 	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && output_contains "CRYPT_NOLOCAL_OK" && output_contains "no-local"
# }

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

test_task_failure_shows_stderr() {
	run_crypt run --destroy -- /bin/sh -c 'echo CRYPT_STDERR_TEST >&2; exit 1'
	# crypt exits 0 when the guest command fails; stderr must still reach the host.
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] &&
		output_contains "CRYPT_STDERR_TEST" &&
		output_contains "exited:" || return 1
}

test_grok_who_are_you() {
	run_crypt grok 'who are you?'
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] || return 1
	output_not_contains "exited:" || return 1
	output_not_contains "no output from grok" || return 1
	grep -qvE '^crypt:' "$CRYPT_LAST_OUTPUT" || return 1
	local vm_name
	vm_name="$(vm_name_from_output)"
	[[ -n "$vm_name" ]] || return 1
	run_crypt destroy
	[[ "$CRYPT_LAST_EXIT" -eq 0 ]] && assert_vm_gone "$vm_name"
}

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

	if [[ "$agent" == "grok" ]]; then
		new_test_dir "grok-who"
		run_test "grok 'who are you?' task prompt" test_grok_who_are_you
	fi

	new_test_dir "task-${agent}"
	run_test "$agent task (keep VM)" test_agent_task_keeps_vm "$agent"

	new_test_dir "destroy-${agent}"
	run_test "$agent task --destroy" test_agent_task_destroy "$agent"

	new_test_dir "mount-${agent}"
	run_test "$agent task --mount" test_agent_task "$agent" --mount

	new_test_dir "reuse-${agent}"
	run_test "$agent task reuse VM" test_agent_reuse_vm "$agent"

	new_test_dir "destroy-cmd-${agent}"
	run_test "destroy after $agent task" test_destroy_command "$agent"

	if [[ "${CRYPT_SKIP_INTERACTIVE:-}" == "1" ]]; then
		skip_test "$agent interactive (SSH)" "CRYPT_SKIP_INTERACTIVE=1"
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

	# crypt run combinations
	new_test_dir "run-echo"
	run_test "run (default keeps VM)" test_run_default_keeps_vm

	new_test_dir "run-destroy"
	run_test "run --destroy" test_run_echo --destroy

	new_test_dir "run-mount"
	run_test "run --mount" test_run_mount

	new_test_dir "run-mount-temp"
	run_test "run --mount unmounts after running VM" test_run_mount_unmounts_for_running_vm

	new_test_dir "run-named"
	run_test "run --name" test_run_named

	new_test_dir "run-resources"
	run_test "run --cpu --memory" test_run_cpu_memory

	# new_test_dir "run-nolocal"
	# run_test "run --no-local" test_no_local

	if ip_filter_supported; then
		new_test_dir "run-ipfilter"
		run_test "run with IP filter rules (SSH allowed)" test_ip_filter_ssh_allowed
	else
		skip_test "run with IP filter rules" "Anka Enterprise required or anka modify network -f- unavailable"
	fi

	new_test_dir "run-failure-stderr"
	run_test "task failure shows guest stderr" test_task_failure_shows_stderr

	new_test_dir "destroy-named"
	run_test "destroy --name" test_destroy_named

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
