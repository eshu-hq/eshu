#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154
# Hermetic proof for the forced-expiry re-claim assertion used by cell 3
# (expire-lease-mid-handler). The parent mirror owns strict mode, fail(),
# repo_root, and the library path variables.
#
# These are behavioural cases, not source pins. The pins in
# test-ifa-fault-injection-cell-pins-cases.sh prove cell 3 CALLS the assertion;
# only these prove the assertion itself fails when the re-claim never happened.
# A pin alone would be satisfied by a helper that always returns 0.

run_ifa_fault_injection_reclaim_assert_cases() {
	test_ifa_reclaim_assert_fails_when_expiry_caused_no_reclaim
	test_ifa_reclaim_assert_passes_on_strict_increase
	test_ifa_reclaim_assert_rejects_equal_count
	test_ifa_reclaim_count_is_domain_agnostic
	test_ifa_expire_reducer_claims_locks_in_ack_order
	test_ifa_drain_gate_fails_fast_when_reducer_exits
	test_ifa_drain_gate_keeps_waiting_while_a_reducer_lives
	test_ifa_drain_gate_keeps_waiting_on_a_replacement_reducer
}

# The case this whole change exists for: the forced expiry ran, nothing was
# re-claimed, and the cell must NOT report success.
test_ifa_reclaim_assert_fails_when_expiry_caused_no_reclaim() (
	# shellcheck source=scripts/lib/ifa_fault_reclaim_assert.sh
	source "${reclaim_assert_lib}"
	local FAULT_COMPOSE_PROJECT=test-project use_compose=0 ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml

	ifa_det_pg() { printf '4'; }

	if ifa_fault_assert_reclaimed_above test-project 0 test-dsn test-compose.yml 4 1 >/dev/null; then
		fail "ifa_fault_assert_reclaimed_above returned success when the re-claimed count never rose above the pre-expiry baseline -- cell 3 would pass without exercising the race"
	fi
)

test_ifa_reclaim_assert_passes_on_strict_increase() (
	# shellcheck source=scripts/lib/ifa_fault_reclaim_assert.sh
	source "${reclaim_assert_lib}"
	local FAULT_COMPOSE_PROJECT=test-project use_compose=0 ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml observed

	ifa_det_pg() { printf '  7\n'; }

	observed="$(ifa_fault_assert_reclaimed_above test-project 0 test-dsn test-compose.yml 4 1)" \
		|| fail "ifa_fault_assert_reclaimed_above rejected a genuine increase (baseline 4, observed 7)"
	[[ "${observed}" == "7" ]] \
		|| fail "ifa_fault_assert_reclaimed_above printed ${observed}, want the whitespace-stripped count 7"
)

# Equality is not evidence. If the count did not move, no row gained a second
# claim while this cell's expiry was in force.
test_ifa_reclaim_assert_rejects_equal_count() (
	# shellcheck source=scripts/lib/ifa_fault_reclaim_assert.sh
	source "${reclaim_assert_lib}"
	local FAULT_COMPOSE_PROJECT=test-project use_compose=0 ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml

	ifa_det_pg() { printf '5'; }

	if ifa_fault_assert_reclaimed_above test-project 0 test-dsn test-compose.yml 5 1 >/dev/null; then
		fail "ifa_fault_assert_reclaimed_above accepted an unchanged count as proof of a re-claim"
	fi
)

# cell 3 expires the lease on EVERY claimed/running reducer row, not on one
# named domain, so the count must not be domain-scoped the way
# ifa_fault_count_retried is. A domain filter here would miss the re-claim
# whenever the reducer happened to be holding a different domain's row.
test_ifa_reclaim_count_is_domain_agnostic() (
	# shellcheck source=scripts/lib/ifa_fault_reclaim_assert.sh
	source "${reclaim_assert_lib}"
	local FAULT_COMPOSE_PROJECT=test-project use_compose=0 ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml seen="" sql_capture
	# The helper calls ifa_det_pg inside $(...), so a variable the stub assigns
	# is lost with the subshell. Capture through a file instead.
	sql_capture="$(mktemp)"

	ifa_det_pg() {
		printf '%s' "$4" >"${sql_capture}"
		printf '1'
	}

	ifa_fault_count_reclaimed test-project 0 test-dsn test-compose.yml >/dev/null
	seen="$(cat "${sql_capture}")"
	rm -f "${sql_capture}"
	[[ "${seen}" == *"stage = 'reducer'"* ]] \
		|| fail "re-claim count query is not scoped to the reducer stage: ${seen}"
	[[ "${seen}" == *"attempt_count > 1"* ]] \
		|| fail "re-claim count query does not count rows claimed more than once: ${seen}"
	# Match the bare word, not "domain =". The spaced form let a no-space
	# `domain='x'` filter through, which is the same silent-zero regression
	# this case exists to catch. Safe to match bare: the query contains no
	# "domain" substring at all.
	[[ "${seen}" != *"domain"* ]] \
		|| fail "re-claim count query filters on a single domain, but cell 3 expires the lease across every domain: ${seen}"
)

# #7123 shape D (run 35943641519): the cell's unordered UPDATE took row locks in
# heap order and deadlocked (40P01) with AckBatch, which locks the same rows
# ORDER BY work_item_id COLLATE "C". The forced expiry must take its row locks
# in that same order so the two can queue but never cycle.
test_ifa_expire_reducer_claims_locks_in_ack_order() (
	# shellcheck source=scripts/lib/ifa_fault_reclaim_assert.sh
	source "${reclaim_assert_lib}"
	local sql=""
	ifa_det_pg() { sql="$4"; }
	declare -F ifa_fault_expire_reducer_claims >/dev/null \
		|| fail "cell 3 has no lock-ordered forced-expiry helper"
	ifa_fault_expire_reducer_claims test-project 0 test-dsn test-compose.yml >/dev/null \
		|| fail "forced-expiry helper failed on a successful statement"
	local compact="${sql//[[:space:]]/ }"
	[[ "${compact}" == *"ORDER BY work_item_id COLLATE \"C\" FOR NO KEY UPDATE"* &&
		"${compact}" == *"UPDATE fact_work_items SET claim_until = now() WHERE work_item_id IN (SELECT work_item_id FROM"* &&
		"${compact}" == *"stage = 'reducer' AND status IN ('claimed', 'running')"* ]] \
		|| fail "forced expiry does not lock claimed reducer rows in the ack's work_item_id COLLATE \"C\" order: ${compact}"
	[[ "$(_ifa_count_code_matches "UPDATE fact_work_items SET claim_until = now() WHERE stage = 'reducer'" "${cells_lib}")" -eq 0 ]] \
		|| fail "cell 3 still runs the unordered forced-expiry UPDATE that deadlocked with AckBatch"
	[[ "$(_ifa_count_code_matches 'ifa_fault_expire_reducer_claims' "${cells_lib}")" -eq 1 ]] \
		|| fail "cell 3 does not call the lock-ordered forced-expiry helper"
)

# The same run's reducer then exited on the AckBatch error, and nothing noticed
# until the drain timed out 4 minutes later. A drain with no live reducer can
# only time out, so it must fail at once and name the reducer's last line.
_ifa_drain_gate_case() {
	local case_dir="$1" gate_seconds="$2" reducer_cmd="$3"
	# shellcheck source=scripts/lib/ifa_determinism_common.sh
	source "${det_lib}"
	# shellcheck source=scripts/lib/ifa_fault_injection_driver.sh
	source "${driver_lib}"
	bin_dir="${case_dir}/bin" log_dir="${case_dir}" GATE_DRAIN_TIMEOUT=4m bg_pids=()
	mkdir -p "${bin_dir}"
	printf '#!/usr/bin/env bash\nexec sleep %s\n' "${gate_seconds}" >"${bin_dir}/eshu-golden-corpus-gate"
	chmod +x "${bin_dir}/eshu-golden-corpus-gate"
	log() { :; }
	die() { printf '%s\n' "$*" >&2; exit 1; }
	assert_rationale_truth() { :; }
	local reducer_pid killed_pid
	if [[ -n "${4:-}" ]]; then
		# A kill cell's deliberately killed reducer stays tracked in bg_pids.
		ifa_det_start_bg "${log_dir}" "reducer-expirelease-before" killed_pid bash -c 'exit 0'
		wait "${killed_pid}" 2>/dev/null || true
	fi
	ifa_det_start_bg "${log_dir}" "reducer-expirelease" reducer_pid bash -c "${reducer_cmd}"
	local drain_rc=0
	run_drain_gate expirelease || drain_rc=$?
	kill "${reducer_pid}" >/dev/null 2>&1 || true
	return "${drain_rc}"
}

test_ifa_drain_gate_fails_fast_when_reducer_exits() (
	local case_dir output rc=0 started elapsed
	case_dir="$(mktemp -d -t ifa-drain-reducer-exit.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	started="${SECONDS}"
	output="$(_ifa_drain_gate_case "${case_dir}" 6 'echo "ERROR reducer failed error=\"deadlock detected (SQLSTATE 40P01)\""; exit 1' 2>&1)" || rc=$?
	elapsed=$((SECONDS - started))
	[[ "${rc}" -ne 0 && "${output}" == *'expirelease: reducer exited: '*'ERROR reducer failed'*'40P01'* && "${elapsed}" -lt 5 ]] \
		|| fail "drain gate did not fail fast on a dead reducer (rc=${rc}, ${elapsed}s, output=${output})"
)

test_ifa_drain_gate_keeps_waiting_while_a_reducer_lives() (
	local case_dir output rc=0
	case_dir="$(mktemp -d -t ifa-drain-reducer-live.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	output="$(_ifa_drain_gate_case "${case_dir}" 1 'exec sleep 30' 2>&1)" || rc=$?
	[[ "${rc}" -eq 0 ]] || fail "drain gate failed while its reducer was alive (rc=${rc}, output=${output})"
)

test_ifa_drain_gate_keeps_waiting_on_a_replacement_reducer() (
	local case_dir output rc=0
	case_dir="$(mktemp -d -t ifa-drain-reducer-replaced.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	output="$(_ifa_drain_gate_case "${case_dir}" 1 'exec sleep 30' killed-before 2>&1)" || rc=$?
	[[ "${rc}" -eq 0 ]] || fail "drain gate failed on a killed reducer while its replacement was alive (rc=${rc}, output=${output})"
)
