#!/usr/bin/env bash
# shellcheck disable=SC2154
# Drain-failure dump (#11228) case module for
# scripts/test-verify-ifa-determinism.sh, split out so that mirror stays
# under the repository's 500-line cap. `fail`, `require_code`,
# `require_code_count`, `require_lib`, `lib`, and `script` come from the
# parent.
run_ifa_determinism_drain_dump_cases() {
	# The N-loop drain failure must dump state, not tail 30 lines: the
	# 30-line tail hid the projector Ack of the stuck scope in run
	# 44992661362, so the lost wake was inferred instead of observed.
	require_code "drain failure dumps queue state and full logs" \
		'ifa_det_dump_drain_failure "N=${n}" "${DETERMINISM_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${log_dir}" "reducer-n${n}" "projector-n${n}"'
	require_code_count "drain failure no longer tails the reducer log" \
		'tail -30 "${log_dir}/reducer-n${n}.log"' 0
	require_code_count "drain failure no longer tails the projector log" \
		'tail -30 "${log_dir}/projector-n${n}.log"' 0
	require_lib "dump helper exists" 'ifa_det_dump_drain_failure() {'

	# Behaviour: run the real helper against a fake psql and 100-line logs.
	# Every section prints, the first and last log lines both appear (the
	# full log, not a tail), and a failing query neither hides the other
	# sections nor fails the helper.
	local tmp out rc
	tmp="$(mktemp -d)"
	seq -f 'reducer line %g' 1 100 >"${tmp}/reducer-n2.log"
	seq -f 'projector line %g' 1 100 >"${tmp}/projector-n2.log"
	out="$(
		# shellcheck source=scripts/lib/ifa_determinism_common.sh
		source "${lib}"
		ifa_det_pg() {
			case "$4" in
			*reducer_waiting_items*) return 1 ;;
			*) printf 'ROWS FOR: %s\n' "$4" ;;
			esac
		}
		ifa_det_dump_drain_failure "N=2" proj 0 dsn compose.yml "${tmp}" reducer-n2 projector-n2 2>&1
		printf 'RC=%s\n' "$?"
	)"
	rm -rf "${tmp}"
	local needle
	for needle in \
		"=== N=2: drain failure dump: scope_generations ===" \
		"ROWS FOR: SELECT scope_id, generation_id, status" \
		"=== N=2: drain failure dump: reducer_waiting_items ===" \
		"N=2: query for reducer_waiting_items failed" \
		"=== N=2: drain failure dump: fact_work_items (non-terminal) ===" \
		"WHERE status NOT IN ('succeeded', 'superseded')" \
		"full log reducer-n2.log" "reducer line 1" "reducer line 100" \
		"full log projector-n2.log" "projector line 1" "projector line 100" \
		"RC=0"; do
		[[ "${out}" == *"${needle}"* ]] \
			|| fail "drain failure dump output is missing: ${needle}"
	done
	rc=0
	[[ "${out}" == *"reducer line 1"$'\n'"reducer line 2"* ]] || rc=1
	[[ "${rc}" -eq 0 ]] || fail "drain failure dump must print the reducer log from its first line"
}
