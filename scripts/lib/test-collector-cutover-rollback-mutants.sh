#!/usr/bin/env bash
# Mutation proof for scripts/test-collector-cutover-rollback.sh (#8895).
#
# Each mutant is one deliberate break of the step, gate, or option logic. The
# runner copies the scripts into a scratch directory, applies the mutation, and
# runs the stub test there with ROLLBACK_TEST_FAIL_FAST=1. A mutant is KILLED when
# the test exits non-zero, SURVIVED when it passes, and STALE when its anchor text
# no longer appears exactly once (the mutant must be updated, never skipped).
#
#   bash scripts/lib/test-collector-cutover-rollback-mutants.sh [--jobs N] [ID ...]
#
# Exit 0 only when every selected mutant is KILLED. SQL predicates are proven
# against real Postgres by scripts/test-collector-cutover-rollback-sql.sh; the stub
# test only pins the SQL digests. M76 mutates the direct-commit SQL to show that a
# dropped predicate cannot pass the stub test with the pin left unchanged.
#
# Mutant table (ID, file, what the break does):
#   M01 steps  step 3 does not check the drain
#   M02 steps  step 4 does not check the grants
#   M03 steps  step 4 does not confirm step 1
#   M04 steps  step 4 does not check the drain
#   M05 steps  step 2 does not confirm step 1
#   M06 steps  step 3 does not confirm step 1
#   M07 gates  the drain ignores active claims
#   M08 gates  the drain ignores claimed work items
#   M09 gates  an unreadable claim table counts as drained
#   M10 gates  a missing grant row counts as revoked
#   M11 gates  a live grant is ignored
#   M12 steps  a wait that times out reports success
#   M13 gates  step 1 confirmation skips the coordinator rollout
#   M14 truth  the overlap count is ignored by the authority check
#   M15 gates  a failing pod listing counts as no pods
#   M16 steps  step 5 checks none of the four earlier gates (M16a-d: one each)
#   M17 steps  the grant is revoked before the drain gate
#   M18 steps  the restore runs before the gates
#   M19 steps  a failed helm restore is ignored
#   M20 gates  the in-tree rollout gate never refuses
#   M21 gates  the grants gate never refuses
#   M22 steps  the drain timeout exits 0
#   M23 steps  step 1 does not read the registry after disabling
#   M24 gates  the step 1 gate does not read the registry
#   M25 gates  an unreadable registry listing counts as clear
#   M26 gates  the registry count includes other components
#   M27 steps  a NotFound worker scale fails step 1
#   M28 steps  any worker scale error counts as NotFound
#   M29 gates  the worker Deployment replicas are not read
#   M30 gates  a failing Deployment read counts as stopped
#   M31 gates  NotFound on the worker Deployment read fails the gate
#   M32 gates  the step 3 gate does not run the dual-authority check
#   M33 main   instance lists glob against the working directory
#   M34 main   an instance named both external and in-tree is accepted
#   M35 steps  run does not check the baseline format before changing anything
#   M36 truth  verify does not check the baseline format
#   M37 truth  a truth mismatch exits 0
#   M38 truth  the drift report always runs
#   M39 truth  the drift key bound is ignored
#   M40 steps  the replay window ignores the clock-skew margin
#   M41 main   the default kubectl request timeout changes
#   M42 main   the default PGCONNECT_TIMEOUT is dropped
#   M43 steps  --from-step 5 does not need --since
#   M44 steps  terminal replay failures are ignored
#   M45 steps  the replay gate ignores retryable items
#   M46 steps  the replay gate does not need a completed item
#   M47 steps  the replay gate ignores claimed items
#   M48 truth  --keys-only compares payload digests
#   M49 steps  step 6 does not run the verification
#   M50 truth  verify does not check the worker
#   M51 truth  verify does not check the registry
#   M52 truth  verify does not check the grants
#   M53 gates  any output line, not only a pod/ line, counts as a worker pod
#   M54 gates  the registry count keeps only the last version row
#   M55 gates  a malformed activations column counts as clear
#   M56 truth  a recapture keeps the older key sidecar
#   M57 main   an instance list may contain control characters
#   M58 gates  any "not found" text, not only NotFound, reads as an absent worker
#   M59 truth  a baseline header of any format version is accepted
#   M60 gates  a component ID is matched by prefix in the registry listing
#   M61 gates  an empty replicas answer counts as stopped
#   M62 main   the script writes files with the caller's umask
#   M63 main   temp files are not removed on exit
#   M64 main   temp files ignore TMPDIR
#   M65 truth  a recapture keeps the mode of the baseline file it overwrites
#   M66 main   --in-tree-direct is accepted beside --in-tree-instance
#   M67 main   direct mode still requires a non-empty in-tree list
#   M68 steps  step 4 restarts the coordinator for a direct producer
#   M69 steps  step 4 keeps the clock-skew margin for a direct producer
#   M70 steps  step 5 replays work items for a direct producer
#   M71 steps  the direct signal passes with no generation committed
#   M72 steps  an unreadable direct signal reads as no progress, not a failure
#   M73 steps  the direct step 5 timeout exits 0
#   M74 truth  a claim beside the restored direct producer is not dual authority
#   M75 steps  the direct signal is read for the wrong collector kind
#   M76 dcsql  the direct signal drops the observed-after-window predicate
#   M77 truth  an unreadable direct Deployment counts as not running
#   M78 gates  the direct Deployment read defaults to not running on failure
#   M79 gates  status.replicas is ignored (a stopping pod does not count)
#   M80 gates  spec.replicas is ignored (a scaled-up Deployment with no pod yet)
#   M81 main   verify in direct mode never needs the Deployment name
#   M82 main   --capture in direct mode needs the Deployment name
#   M83 truth  an unreadable direct Deployment is not named in the message
#   M84 gates  a malformed replica answer is accepted
#   M85 main   verify in the default mode needs the Deployment name
#   M86 truth  the direct Deployment is not read at all
#   M87 truth  the default mode reads the direct Deployment too
#   M88 main   the direct Deployment read drops -n <namespace>
#   M89 main   the direct Deployment read drops the request timeout
#   M90 truth  the passing evidence line drops the unreadable-Deployment note
#   M91 gates  the unreadable-Deployment note drops the kubectl exit code
#   M92 plan   plan omits the Deployment read in direct mode

# The single-quoted anchors are literal source text to find, not expansions.
# shellcheck disable=SC2016
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
jobs=4
selected=()
while [[ $# -gt 0 ]]; do
	case "$1" in
	--jobs)
		jobs="$2"
		shift 2
		;;
	*)
		selected+=("$1")
		shift
		;;
	esac
done

declare_file() { # alias -> path relative to scripts/
	case "$1" in
	main) printf 'collector-cutover-rollback.sh' ;;
	steps) printf 'lib/collector-cutover-rollback-steps.sh' ;;
	gates) printf 'lib/collector-cutover-rollback-gates.sh' ;;
	truth) printf 'lib/collector-cutover-rollback-truth.sh' ;;
	dcsql) printf 'lib/collector-cutover-rollback-direct-commit.sql' ;;
	plan) printf 'lib/collector-cutover-rollback-plan.sh' ;;
	esac
}

# Each mutant is a file alias plus packed old/new anchor pairs; apply_mutant
# applies them to the scratch copy.
define_mutants() {
	NL=$'\n'
	T=$'\t'
	MUT_IDS=()
	MUT_FILE=()
	MUT_EDITS=()
	add() { # id file then old/new pairs via MUT_PAIR array
		MUT_IDS+=("$1")
		MUT_FILE+=("$2")
		MUT_EDITS+=("$3")
	}
	pairs() { # old new [old new ...] -> a unit-separator joined string
		local out="" sep=$'\x1f'
		while [[ $# -gt 0 ]]; do
			out+="${1}${sep}${2}${sep}"
			shift 2
		done
		printf '%s' "$out"
	}
	add M01 steps "$(pairs "${T}gate_step1 3${NL}${T}gate_step2 3${NL}" "${T}gate_step1 3${NL}")"
	add M02 steps "$(pairs "${T}gate_step3 4${NL}" "")"
	add M03 steps "$(pairs "${T}gate_step1 4${NL}" "")"
	add M04 steps "$(pairs "${T}gate_step2 4${NL}" "")"
	add M05 steps "$(pairs "${T}gate_step1 2${NL}" "")"
	add M06 steps "$(pairs "${T}gate_step1 3${NL}${T}gate_step2 3${NL}" "${T}gate_step2 3${NL}")"
	add M07 gates "$(pairs 'DRAIN_ACTIVE="${XOUT##*|}"' 'DRAIN_ACTIVE="0"')"
	add M08 gates "$(pairs 'DRAIN_CLAIMED="${XOUT%%|*}"' 'DRAIN_CLAIMED="0"')"
	add M09 gates "$(pairs "${T}${T}step_failed \"\$1\" \"could not read the claim table (exit \${XRC}); treating the drain as unknown\"${NL}" "${T}${T}DRAIN_CLAIMED=0${NL}${T}${T}DRAIN_ACTIVE=0${NL}${T}${T}return 0${NL}")"
	add M10 gates "$(pairs 'if (!seen) print "missing"' 'if (!seen) print "revoked"')"
	add M11 gates "$(pairs 'if ($NF != "revoked=true") live = 1' 'live = 0')"
	add M12 steps "$(pairs "${T}${T}if ((SECONDS >= deadline)); then${NL}${T}${T}${T}return 1" "${T}${T}if ((SECONDS >= deadline)); then${NL}${T}${T}${T}return 0")"
	add M13 gates "$(pairs "${T}${T}CONFIRM_NOTE=\"coordinator deployment/\${coordinator_deployment} rollout did not complete: \${XOUT}\"${NL}${T}${T}return 1${NL}" "${T}${T}CONFIRM_NOTE=\"skipped\"${NL}")"
	add M14 truth "$(pairs 'if ((ext > 0 && tree > 0)) || ((overlaps > 0)); then' 'if ((ext > 0 && tree > 0)); then')"
	add M15 gates "$(pairs "${T}${T}POD_NOTE=\"listing worker pods failed with exit \${XRC}: \${XOUT}\"${NL}${T}${T}return 1" "${T}${T}POD_NOTE=\"listing worker pods failed with exit \${XRC}: \${XOUT}\"${NL}${T}${T}return 0")"
	add M16 steps "$(pairs "${T}gate_step1 5${NL}${T}gate_step2 5${NL}${T}gate_step3 5${NL}${T}gate_step4 5${NL}" "")"
	add M16a steps "$(pairs "${T}gate_step1 5${NL}" "")"
	add M16b steps "$(pairs "${T}gate_step2 5${NL}" "")"
	add M16c steps "$(pairs "${T}gate_step3 5${NL}" "")"
	add M16d steps "$(pairs "${T}gate_step4 5${NL}" "")"
	add M17 steps "$(pairs "${T}gate_step1 3${NL}${T}gate_step2 3${NL}${T}local kind${NL}${T}for kind in \"\${grant_kinds[@]}\"; do${NL}${T}${T}build_revoke \"\$kind\"${NL}${T}${T}must_exec 3 \"revoking the grant for kind \${kind}\"${NL}${T}done${NL}" "${T}gate_step1 3${NL}${T}local kind${NL}${T}for kind in \"\${grant_kinds[@]}\"; do${NL}${T}${T}build_revoke \"\$kind\"${NL}${T}${T}must_exec 3 \"revoking the grant for kind \${kind}\"${NL}${T}done${NL}${T}gate_step2 3${NL}")"
	add M18 steps "$(pairs "${T}gate_step1 4${NL}${T}gate_step2 4${NL}${T}gate_step3 4${NL}" "" "${T}${T}must_exec 4 \"applying the rollback values with helm\"${NL}${T}fi${NL}" "${T}${T}must_exec 4 \"applying the rollback values with helm\"${NL}${T}fi${NL}${T}gate_step1 4${NL}${T}gate_step2 4${NL}${T}gate_step3 4${NL}")"
	add M19 steps "$(pairs 'must_exec 4 "applying the rollback values with helm"' 'exec_cmd')"
	add M20 gates "$(pairs 'in_tree_rolled_out || refuse "$1"' 'in_tree_rolled_out || : "$1"')"
	add M21 gates "$(pairs "${T}if ! grants_revoked; then${NL}${T}${T}refuse \"\$1\" \"\${GRANT_NOTE}. Revoke the external" "${T}if false; then${NL}${T}${T}refuse \"\$1\" \"\${GRANT_NOTE}. Revoke the external")"
	add M22 steps "$(pairs "${T}${T}exit \"\$EX_TIMEOUT\"${NL}${T}fi${NL}${T}evidence 2" "${T}${T}exit 0${NL}${T}fi${NL}${T}evidence 2")"
	add M23 steps "$(pairs "${T}if ! registry_clear; then${NL}${T}${T}[[ \"\$REGISTRY_COUNT\"" "${T}if false; then${NL}${T}${T}[[ \"\$REGISTRY_COUNT\"")"
	add M24 gates "$(pairs "${T}if ! registry_clear; then${NL}${T}${T}CONFIRM_NOTE=\"\$REGISTRY_NOTE\"${NL}${T}${T}return 1${NL}${T}fi${NL}" "")"
	add M25 gates "$(pairs 'REGISTRY_COUNT="unknown"
		return 0' 'REGISTRY_COUNT="0"
		return 0')"
	add M26 gates "$(pairs '$1 == id {' '{')"
	add M27 steps "$(pairs 'if is_not_found "$XOUT"; then' 'if false; then')"
	add M28 steps "$(pairs 'if is_not_found "$XOUT"; then' 'if true; then')"
	add M29 gates "$(pairs 'if [[ "$XOUT" != "0" ]]; then' 'if false; then')"
	add M30 gates "$(pairs 'elif ! is_not_found "$XOUT"; then' 'elif false; then')"
	add M31 gates "$(pairs 'elif ! is_not_found "$XOUT"; then' 'elif true; then')"
	add M32 gates "$(pairs "${T}check_authority \"\$1\"${NL}}" "}")"
	add M33 main "$(pairs '	IFS=, read -r -a parts <<<"$value"' '	local IFS=,
	# shellcheck disable=SC2206
	parts=($value)')"
	add M34 main "$(pairs "${T}${T}check_disjoint${NL}" "")"
	add M35 steps "$(pairs "${T}${T}if problem=\"\$(baseline_format_problem)\"; then${NL}${T}${T}${T}die" "${T}${T}if false; then${NL}${T}${T}${T}die")"
	add M36 truth "$(pairs "${T}check_baseline_format \"\$1\"${NL}${T}read_truth" "${T}read_truth")"
	add M37 truth "$(pairs 'exit "$EX_TRUTH_MISMATCH"' 'exit 0')"
	add M38 truth "$(pairs '[[ "$report_drift" != "true" ]] || report_drift "$1"' 'report_drift "$1"')"
	add M39 truth "$(pairs 'if (shown[class SUBSEP kind]++ < max)' 'if (1)')"
	add M40 steps "$(pairs 'local skew="$ROLLBACK_CLOCK_SKEW_SECONDS"' 'local skew=0')"
	add M41 main "$(pairs 'ROLLBACK_KUBECTL_REQUEST_TIMEOUT:-30s' 'ROLLBACK_KUBECTL_REQUEST_TIMEOUT:-5s')"
	add M42 main "$(pairs 'PGCONNECT_TIMEOUT:-10' 'PGCONNECT_TIMEOUT:-')"
	add M43 steps "$(pairs 'if ((from_step == 5)) && [[ -z "$since" ]]; then' 'if false; then')"
	add M44 steps "$(pairs 'if ((r_terminal > 0)); then' 'if false; then')"
	add M45 steps "$(pairs '((r_pending == 0 && r_claimed == 0 && r_retryable == 0 && r_completed >= 1))' '((r_pending == 0 && r_claimed == 0 && r_completed >= 1))')"
	add M46 steps "$(pairs '((r_pending == 0 && r_claimed == 0 && r_retryable == 0 && r_completed >= 1))' '((r_pending == 0 && r_claimed == 0 && r_retryable == 0))')"
	add M47 steps "$(pairs '((r_pending == 0 && r_claimed == 0 && r_retryable == 0 && r_completed >= 1))' '((r_pending == 0 && r_retryable == 0 && r_completed >= 1))')"
	add M48 truth "$(pairs "awk -F'|' 'NF && !/^#/ { print \$1 \"|\" \$2 \"|\" \$3 }' \"\$1\"" "awk 'NF && !/^#/' \"\$1\"")"
	add M49 steps "$(pairs "${T}do_verify 6${NL}${T}printf 'Now compare" "${T}printf 'Now compare")"
	add M50 truth "$(pairs "${T}${T}if worker_pods_gone; then" "${T}${T}if true; then")"
	add M51 truth "$(pairs "${T}${T}if registry_clear; then${NL}${T}${T}${T}evidence" "${T}${T}if true; then${NL}${T}${T}${T}evidence")"
	add M53 gates "$(pairs "awk '/^pod\\//'" "awk 'NF'")"
	add M52 truth "$(pairs "${T}${T}if grants_revoked; then${NL}${T}${T}${T}evidence" "${T}${T}if true; then${NL}${T}${T}${T}evidence")"
	add M54 gates "$(pairs 'total += $NF' 'total = $NF')"
	add M55 gates "$(pairs 'else { bad = 1 }' 'else { }')"
	add M56 truth "$(pairs "${T}rm -f \"\${capture_file}\" \"\${capture_file}.keys\"${NL}" "${T}rm -f \"\${capture_file}\"${NL}")"
	add M57 main "$(pairs "${T}[[ \"\$value\" != *[[:cntrl:]]* ]] ||${NL}${T}${T}die \"\$EX_USAGE\" \"\$name must not contain control characters or newlines\"${NL}" "")"
	add M58 gates "$(pairs 'is_not_found() { [[ "$1" == *"(NotFound)"* ]]; }' 'is_not_found() { [[ "$1" == *"not found"* ]]; }')"
	add M59 truth "$(pairs '[[ "$first" != "$TRUTH_HEADER" ]] || return 1' '[[ "$first" != "# eshu-rollback-truth format="* ]] || return 1')"
	add M60 gates "$(pairs '$1 == id {' 'index($1, id) == 1 {')"
	add M61 gates "$(pairs 'if [[ "$XOUT" != "0" ]]; then' 'if [[ -n "$XOUT" && "$XOUT" != "0" ]]; then')"
	add M62 main "$(pairs "${NL}umask 077${NL}" "${NL}")"
	add M63 main "$(pairs "trap cleanup_tmp EXIT${NL}" "")"
	add M65 truth "$(pairs "${T}rm -f \"\${capture_file}\" \"\${capture_file}.keys\"${NL}" "${T}rm -f \"\${capture_file}.keys\"${NL}")"
	add M66 main "$(pairs "${T}${T}[[ -z \"\$in_tree_instances\" ]] ||${NL}" "${T}${T}true ||${NL}")"
	add M67 main "$(pairs "${T}if [[ \"\$in_tree_direct\" == \"true\" ]]; then${NL}${T}${T}# A direct collector has no instance ID" "${T}if false; then${NL}${T}${T}# A direct collector has no instance ID")"
	add M68 steps "$(pairs "${T}if [[ \"\$in_tree_direct\" != \"true\" ]]; then${NL}${T}${T}build_restart_coordinator" "${T}if true; then${NL}${T}${T}build_restart_coordinator")"
	add M69 steps "$(pairs "${T}${T}[[ \"\$in_tree_direct\" != \"true\" ]] || skew=0${NL}" "")"
	add M70 steps "$(pairs "${T}if [[ \"\$in_tree_direct\" == \"true\" ]]; then${NL}${T}${T}step_5_direct" "${T}if false; then${NL}${T}${T}step_5_direct")"
	add M71 steps "$(pairs '((DIRECT_COMMITS >= 1))' '((DIRECT_COMMITS >= 0))')"
	add M72 steps "$(pairs 'step_failed 5 "could not read the observed-generation signal (exit ${XRC})"' 'return 1')"
	add M73 steps "$(pairs "check the collector logs.\\n' >&2${NL}${T}${T}exit \"\$EX_TIMEOUT\"" "check the collector logs.\\n' >&2${NL}${T}${T}exit 0")"
	add M74 truth "$(pairs '[[ "$DIRECT_LIVE" == "no" ]] || tree=1' 'true')"
	add M75 steps "$(pairs 'psql_run direct-commit -v "kind=${collector_kind}"' 'psql_run direct-commit -v "kind=other"')"
	add M76 dcsql "$(pairs "  AND generation.ingested_at >= :'since'::timestamptz${NL}" "")"
	add M77 truth "$(pairs '[[ "$DIRECT_LIVE" == "no" ]] || tree=1' 'if [[ "$DIRECT_LIVE" == "yes" ]]; then tree=1; fi')"
	add M78 gates "$(pairs "${T}${T}DIRECT_LIVE=\"unknown\"${NL}${T}${T}DIRECT_NOTE=\"deployment/" "${T}${T}DIRECT_LIVE=\"no\"${NL}${T}${T}DIRECT_NOTE=\"deployment/")"
	add M79 gates "$(pairs '10#${desired} > 0 || 10#${observed:-0} > 0' '10#${desired} > 0')"
	add M80 gates "$(pairs '10#${desired} > 0 || 10#${observed:-0} > 0' '10#${observed:-0} > 0')"
	add M81 main "$(pairs 'if [[ "$in_tree_direct" == "true" && -z "$capture_file" ]]; then' 'if false; then')"
	add M82 main "$(pairs 'if [[ "$in_tree_direct" == "true" && -z "$capture_file" ]]; then' 'if [[ "$in_tree_direct" == "true" ]]; then')"
	add M83 truth "$(pairs '[[ "$DIRECT_LIVE" != "unknown" ]] || direct_note=" The direct Deployment ${DIRECT_NOTE}; it is treated as running."' ':')"
	add M84 gates "$(pairs 'if [[ "$XRC" -ne 0 || ! "$XOUT" =~ ^[0-9]+\|[0-9]*$ ]]; then' 'if [[ "$XRC" -ne 0 ]]; then')"
	add M85 main "$(pairs 'if [[ "$in_tree_direct" == "true" && -z "$capture_file" ]]; then' 'if [[ -z "$capture_file" ]]; then')"
	add M86 truth "$(pairs "${T}${T}read_direct_running${NL}" "${T}${T}DIRECT_LIVE=no${NL}")"
	add M87 truth "$(pairs "${T}local direct_note=\"\"${NL}" "${T}local direct_note=\"\"${NL}${T}read_direct_running${NL}")"
	add M88 main "$(pairs '-n "$namespace" get "deployment/${in_tree_deployment}"' 'get "deployment/${in_tree_deployment}"')"
	add M89 main "$(pairs "{.status.replicas}'${NL}${T}${T}\"--request-timeout=\${ROLLBACK_KUBECTL_REQUEST_TIMEOUT}\")" "{.status.replicas}')")"
	add M90 truth "$(pairs 'overlapping_claim_pairs=0${direct_note}"' 'overlapping_claim_pairs=0"')"
	add M91 gates "$(pairs 'could not be read (exit ${XRC}): ' 'could not be read: ')"
	add M92 plan "$(pairs "${T}[[ \"\$in_tree_direct\" == \"true\" ]] || return 0${NL}${T}build_in_tree_replicas" "${T}return 0${NL}${T}build_in_tree_replicas")"
	add M64 main "$(pairs '"${TMPDIR:-/tmp}/eshu-rollback.XXXXXX"' '"/tmp/eshu-rollback.XXXXXX"')"
}

# apply_mutant <scratch-scripts-dir> <file-alias> <packed-edits>; prints a reason
# and returns 1 when an anchor does not appear exactly once.
apply_mutant() {
	local dir="$1" alias="$2" packed="$3" path content old new rest count stripped
	path="${dir}/$(declare_file "$alias")"
	content="$(
		cat "$path"
		printf x
	)"
	content="${content%x}"
	rest="$packed"
	local sep=$'\x1f'
	while [[ -n "$rest" ]]; do
		old="${rest%%"$sep"*}"
		rest="${rest#*"$sep"}"
		new="${rest%%"$sep"*}"
		rest="${rest#*"$sep"}"
		stripped="${content//"$old"/}"
		count=$(((${#content} - ${#stripped}) / ${#old}))
		if [[ "$count" -ne 1 ]]; then
			printf 'anchor appears %s times: %s' "$count" "$(printf '%s' "$old" | head -c 80 | tr '\n\t' '  ')"
			return 1
		fi
		content="${content/"$old"/"$new"}"
	done
	printf '%s' "$content" >"$path"
	[[ "$path" == *.sql ]] || bash -n "$path" 2>/dev/null || {
		printf 'the mutated file is not valid bash'
		return 1
	}
}

run_mutant() { # index
	local idx="$1" id="${MUT_IDS[$1]}" scratch result_file reason rc=0
	scratch="$(mktemp -d "${TMPDIR:-/tmp}/rbmut.XXXXXX")"
	mkdir -p "${scratch}/scripts/lib" "${scratch}/go/internal/storage/postgres"
	cp "${repo_root}/scripts/collector-cutover-rollback.sh" "${repo_root}/scripts/test-collector-cutover-rollback.sh" "${scratch}/scripts/"
	cp "${repo_root}"/scripts/lib/collector-cutover-rollback-* "${repo_root}"/scripts/lib/test-collector-cutover-rollback-* "${scratch}/scripts/lib/"
	result_file="${results_dir}/${id}"
	if ! reason="$(apply_mutant "${scratch}/scripts" "${MUT_FILE[$idx]}" "${MUT_EDITS[$idx]}")"; then
		printf 'STALE|%s\n' "$reason" >"$result_file"
		rm -rf "$scratch"
		return 0
	fi
	ROLLBACK_TEST_FAIL_FAST=1 bash "${scratch}/scripts/test-collector-cutover-rollback.sh" >"${scratch}/out.log" 2>&1 || rc=$?
	if [[ "$rc" -ne 0 ]]; then
		printf 'KILLED|%s\n' "$(awk '/^FAIL/ { print; exit }' "${scratch}/out.log")" >"$result_file"
	else
		printf 'SURVIVED|\n' >"$result_file"
	fi
	rm -rf "$scratch"
}

define_mutants
results_dir="$(mktemp -d "${TMPDIR:-/tmp}/rbmut-results.XXXXXX")"
trap 'rm -rf "${results_dir}"' EXIT

# The unmutated tree must pass first, or every mutant would look killed.
printf 'baseline: running the unmutated test\n'
ROLLBACK_TEST_FAIL_FAST=1 bash "${repo_root}/scripts/test-collector-cutover-rollback.sh" >/dev/null 2>&1 || {
	printf 'the unmutated test fails; fix it before judging mutants\n' >&2
	exit 2
}

running=0
for idx in "${!MUT_IDS[@]}"; do
	if [[ ${#selected[@]} -gt 0 ]]; then
		want=0
		for id in "${selected[@]}"; do [[ "$id" != "${MUT_IDS[$idx]}" ]] || want=1; done
		[[ "$want" == 1 ]] || continue
	fi
	run_mutant "$idx" &
	running=$((running + 1))
	if ((running >= jobs)); then
		wait -n 2>/dev/null || wait
		running=$((running - 1))
	fi
done
wait

killed=0
bad=0
printf '\n%-6s %-9s %s\n' ID RESULT DETAIL
for idx in "${!MUT_IDS[@]}"; do
	id="${MUT_IDS[$idx]}"
	[[ -f "${results_dir}/${id}" ]] || continue
	verdict="$(cut -d'|' -f1 "${results_dir}/${id}")"
	detail="$(cut -d'|' -f2- "${results_dir}/${id}")"
	printf '%-6s %-9s %s\n' "$id" "$verdict" "$detail"
	if [[ "$verdict" == "KILLED" ]]; then killed=$((killed + 1)); else bad=$((bad + 1)); fi
done
printf '\nKILLED %d, NOT KILLED %d\n' "$killed" "$bad"
[[ "$bad" -eq 0 ]]
