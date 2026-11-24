#!/usr/bin/env bash
# Seeded RED/GREEN contract test for the #11382 failure-diagnostics bundle
# (scripts/lib/golden-corpus-failure-diagnostics.sh). Hermetic: Postgres is a
# stubbed pg() function, the work dir and capture dir are seeded fixtures, and
# the size cap is driven through GOLDEN_CORPUS_DIAG_MAX_KB so no 100MB
# fixture is needed. No live backends, no network, no Docker.
# shellcheck disable=SC2329 # every pg() stub below runs indirectly through stage_failure_diagnostics(), which shellcheck cannot see.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${repo_root}/scripts/lib/golden-corpus-failure-diagnostics.sh"
fixture="$(mktemp -d -t failure-diag-test.XXXXXX)"
trap 'rm -rf "${fixture}"' EXIT

fail() { printf 'test-stage-failure-diagnostics: %s\n' "$*" >&2; exit 1; }

[[ -f "${helper}" ]] || fail "missing helper: ${helper}"
bash -n "${helper}" || fail "golden-corpus-failure-diagnostics.sh has a syntax error"
# shellcheck source=scripts/lib/golden-corpus-failure-diagnostics.sh
. "${helper}"
command -v stage_failure_diagnostics >/dev/null 2>&1 ||
	fail "helper must define stage_failure_diagnostics()"

expect_failure() {
	local name="$1"
	shift
	if "$@" >"${fixture}/${name}.log" 2>&1; then
		echo "FAIL: ${name} unexpectedly passed" >&2
		exit 1
	fi
}

# Canned read-only answers, dispatched per query; every invocation is logged
# so the test can prove the helper only ever issues bounded SELECTs.
pg() {
	printf '%s\n---\n' "$1" >>"${PG_CALL_LOG}"
	case "$1" in
		*diag-q1-terraform-state-scopes*)
			printf '[{"scope_id":"state_snapshot:s3:abc","status":"active","active_generation_id":"g1","generation_count":2,"active_generation_count":1}]'
			;;
		*diag-q2-drift-findings*)
			printf '[{"finding_kind":"drifted","finding_count":3}]'
			;;
		*diag-q3-iac-management-count*)
			printf '5'
			;;
		*)
			printf 'UNEXPECTED-QUERY' >&2
			return 1
			;;
	esac
}

seed_populated() {
	local root="$1" i n
	mkdir -p "${root}/work/logs" "${root}/capture" "${root}/tmp"
	printf '{"snapshot":"repository-runtime","repos":20}\n' >"${root}/work/logs/e2e-20repo-repository-runtime-snapshot.json"
	printf '{"oracle":"persisted-aggregate-counts"}\n' >"${root}/work/logs/persisted-aggregate-counts.json"
	printf '{"phases":{"graph_query":3}}\n' >"${root}/work/logs/phase-timings.json"
	printf '{"leg":"pair1-neo4j","statements":12}\n' >"${root}/capture/recording.jsonl"
	# The Bearer token below is a LIVE seed, not a pre-redacted one: Case 1
	# asserts its absence from the staged bundle, so the Authorization
	# rule is load-bearing (break the rule and the suite goes RED).
	for i in $(seq 1 250); do
		printf 'api log line %03d padding-padding-padding-padding\n' "${i}" >>"${root}/work/logs/api.log"
	done
	{
		printf 'Authorization: Bearer s3cr3t-diag-token-001\n'
		printf 'user password=hunter2-diag-pw authenticated\n'
		printf 'dsn postgresql://eshu:dbpw-diag-9@localhost:15432/eshu selected\n'
		printf -- '-----BEGIN RSA PRIVATE KEY-----\n'
		printf 'MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKdiagkey\n'
		printf -- '-----END RSA PRIVATE KEY-----\n'
		printf 'note: PRIVATE KEY material must never leave the host\n'
		printf 'AUTHORIZATION: Bearer upper-diag-token-007\n'
		printf 'db PASSWORD=UpperDiagPw007 authenticated\n'
	} >>"${root}/work/logs/api.log"
	for n in $(seq 1 10); do
		printf 'short log line %d\n' "${n}" >>"${root}/work/logs/short.log"
	done
	# P1 straddle seed: a 301-line log whose PEM block straddles the
	# 200-line tail cut. BEGIN sits on line 101 (cut off); the Proc-Type /
	# DEK-Info headers, 20 body lines, and the END line fall inside the
	# tail window (lines 102..301).
	for n in $(seq 1 100); do
		printf 'straddle filler %03d\n' "${n}" >>"${root}/work/logs/straddle.log"
	done
	{
		printf -- '-----BEGIN RSA PRIVATE KEY-----\n'
		printf 'Proc-Type: 4,ENCRYPTED\n'
		printf 'DEK-Info: AES-128-CBC,9F8E7D6C\n'
		for n in $(seq 1 20); do
			printf 'c3RyYWRkbGVib2R5ZGlhZ2tleWxpbmUxMjM0NTY3ODkwQUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVowMTIzNDU2Nzg5Ky8vPAD=%s\n' "${n}" >>"${root}/work/logs/straddle.log"
		done
		printf -- '-----END RSA PRIVATE KEY-----\n'
		printf 'straddle post-end marker line\n'
	} >>"${root}/work/logs/straddle.log"
	for n in $(seq 126 301); do
		printf 'straddle filler %03d\n' "${n}" >>"${root}/work/logs/straddle.log"
	done
	# P3b symlink seed: a *.log symlink pointing outside log_dir must be
	# skipped, never followed into the bundle.
	printf 'linked secret marker linked-secret-diag-5\n' >"${root}/outside-secret.log"
	ln -s "${root}/outside-secret.log" "${root}/work/logs/linked.log"
}

# Case 1: a populated work_dir stages (i)+(ii)+(iii) with redacted tails.
seed_populated "${fixture}/pop"
export RUNNER_TEMP="${fixture}/pop/tmp"
export GATE_COMPOSE_PROJECT="diag-test-job"
export ESHU_DIFFERENTIAL_CAPTURE_DIR="${fixture}/pop/capture"
export PG_CALL_LOG="${fixture}/pop/pg-calls.txt"
work_dir="${fixture}/pop/work"
log_dir="${fixture}/pop/work/logs"
stage_failure_diagnostics >"${fixture}/pop/stage.out" 2>"${fixture}/pop/stage.err" ||
	fail "populated work_dir must stage"
dest="${fixture}/pop/tmp/golden-corpus-failure/diag-test-job"
[[ -d "${dest}" ]] || fail "bundle dir missing: ${dest}"
rg -q -F -- "${dest}" "${fixture}/pop/stage.out" || fail "staging must print the bundle dir"
cmp -s "${fixture}/pop/capture/recording.jsonl" "${dest}/capture/recording.jsonl" ||
	fail "capture *.jsonl must be staged as-is"
for f in e2e-20repo-repository-runtime-snapshot.json persisted-aggregate-counts.json phase-timings.json; do
	cmp -s "${log_dir}/${f}" "${dest}/${f}" || fail "${f} must be staged as-is"
done
[[ -f "${dest}/log-tails/api.log.tail" ]] || fail "missing api log tail"
[[ "$(wc -l <"${dest}/log-tails/api.log.tail" | tr -d ' ')" -eq 200 ]] ||
	fail "api tail must hold the last 200 lines"
[[ -f "${dest}/log-tails/short.log.tail" ]] || fail "missing short log tail"
cmp -s "${log_dir}/short.log" "${dest}/log-tails/short.log.tail" ||
	fail "a short log must be tailed whole"
for secret in upper-diag-token-007 UpperDiagPw007; do
	if rg -q -F -- "${secret}" "${dest}"; then
		fail "bundle leaks seeded uppercase secret: ${secret}"
	fi
done
[[ -f "${dest}/log-tails/straddle.log.tail" ]] || fail "missing straddle log tail"
[[ "$(wc -l <"${dest}/log-tails/straddle.log.tail" | tr -d ' ')" -eq 200 ]] ||
	fail "straddle tail must hold the last 200 lines"
for secret in c3RyYWRkbGVib2R5ZGlhZ2tleW 'Proc-Type' 'DEK-Info' 'END RSA PRIVATE KEY'; do
	if rg -q -F -- "${secret}" "${dest}/log-tails/straddle.log.tail"; then
		fail "straddle tail leaks cut key material: ${secret}"
	fi
done
rg -q -F -- 'straddle post-end marker line' "${dest}/log-tails/straddle.log.tail" ||
	fail "straddle redaction must stop at the END line, not nuke the tail"
test ! -e "${dest}/log-tails/linked.log.tail" ||
	fail "a symlinked log must be skipped, never followed"
if rg -q -F -- 'linked-secret-diag-5' "${dest}"; then
	fail "bundle leaks content from outside log_dir via a symlinked log"
fi
for secret in s3cr3t-diag-token-001 hunter2-diag-pw dbpw-diag-9 MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKdiagkey 'BEGIN RSA PRIVATE KEY'; do
	if rg -q -F -- "${secret}" "${dest}"; then
		fail "bundle leaks seeded secret: ${secret}"
	fi
done
rg -q -F -- "[redacted]" "${dest}/log-tails/api.log.tail" || fail "tails must carry redaction markers"
jq -e '.terraform_state_scopes | length == 1' "${dest}/reducer-domain.json" >/dev/null ||
	fail "reducer-domain.json must carry the terraform-state scope counts"
jq -e '.drift_findings_by_finding_kind | length == 1' "${dest}/reducer-domain.json" >/dev/null ||
	fail "reducer-domain.json must carry drift findings by finding_kind"
jq -e '.iac_management_finding_count == 5' "${dest}/reducer-domain.json" >/dev/null ||
	fail "reducer-domain.json must carry the IaC management finding count"
jq -e '.job == "diag-test-job"' "${dest}/manifest.json" >/dev/null ||
	fail "manifest.json must name the job"
[[ "$(rg -c -- '^---$' "${PG_CALL_LOG}")" -eq 3 ]] || fail "staging must issue exactly three Postgres queries"
if rg -qi -- '\binsert\b|\bupdate\b|\bdelete\b|\bdrop\b|\btruncate\b|\bcopy\b|\balter\b|\bcreate\b' "${PG_CALL_LOG}"; then
	fail "diagnostic queries must be read-only SELECTs"
fi

# Case 2: a missing work_dir fails closed with a loud error and no bundle.
export RUNNER_TEMP="${fixture}/missing-tmp"
mkdir -p "${RUNNER_TEMP}"
work_dir="${fixture}/does-not-exist"
log_dir="${fixture}/does-not-exist/logs"
unset ESHU_DIFFERENTIAL_CAPTURE_DIR
expect_failure missing_input stage_failure_diagnostics "missing-job"
rg -q '::error' "${fixture}/missing_input.log" || fail "missing input must fail loud"
test ! -e "${RUNNER_TEMP}/golden-corpus-failure/missing-job" ||
	fail "missing input must leave no bundle behind"

# Case 3: empty logs plus failing Postgres fails closed.
mkdir -p "${fixture}/empty/work/logs" "${fixture}/empty/capture" "${fixture}/empty-tmp"
pg() {
	printf '%s\n' "$1" >>"${PG_CALL_LOG}"
	return 1
}
export RUNNER_TEMP="${fixture}/empty-tmp"
export ESHU_DIFFERENTIAL_CAPTURE_DIR="${fixture}/empty/capture"
export PG_CALL_LOG="${fixture}/empty/pg-calls.txt"
work_dir="${fixture}/empty/work"
log_dir="${fixture}/empty/work/logs"
expect_failure empty_input stage_failure_diagnostics "empty-job"
rg -q '::error' "${fixture}/empty_input.log" || fail "empty input must fail loud"
test ! -e "${RUNNER_TEMP}/golden-corpus-failure/empty-job" ||
	fail "empty input must leave no bundle behind"

# Case 4: over the cap, tails truncate first and small files stay intact.
pg() {
	printf '%s\n---\n' "$1" >>"${PG_CALL_LOG}"
	case "$1" in
		*diag-q1-terraform-state-scopes* | *diag-q2-drift-findings*) printf '[]' ;;
		*diag-q3-iac-management-count*) printf '0' ;;
		*) return 1 ;;
	esac
}
mkdir -p "${fixture}/cap/work/logs" "${fixture}/cap/capture" "${fixture}/cap-tmp"
printf '{"snapshot":"repository-runtime"}\n' >"${fixture}/cap/work/logs/e2e-20repo-repository-runtime-snapshot.json"
printf '{"leg":"cap"}\n' >"${fixture}/cap/capture/recording.jsonl"
for i in $(seq 1 60); do
	for n in $(seq 1 300); do
		printf 'diag-cap-log %02d line %03d ..................................................................\n' "${i}" "${n}" >>"${fixture}/cap/work/logs/host-${i}.log"
	done
done
export RUNNER_TEMP="${fixture}/cap-tmp"
export ESHU_DIFFERENTIAL_CAPTURE_DIR="${fixture}/cap/capture"
export PG_CALL_LOG="${fixture}/cap/pg-calls.txt"
export GOLDEN_CORPUS_DIAG_MAX_KB=1024
work_dir="${fixture}/cap/work"
log_dir="${fixture}/cap/work/logs"
stage_failure_diagnostics >"${fixture}/cap/stage.out" 2>"${fixture}/cap/stage.err" ||
	fail "a truncatable bundle must still stage"
cap_dest="${RUNNER_TEMP}/golden-corpus-failure/diag-test-job"
for i in $(seq 1 60); do
	[[ "$(wc -l <"${cap_dest}/log-tails/host-${i}.log.tail" | tr -d ' ')" -eq 50 ]] ||
		fail "over-cap tails must truncate to 50 lines first"
done
cmp -s "${fixture}/cap/capture/recording.jsonl" "${cap_dest}/capture/recording.jsonl" ||
	fail "cap truncation must not touch the capture recording"
cmp -s "${log_dir}/e2e-20repo-repository-runtime-snapshot.json" "${cap_dest}/e2e-20repo-repository-runtime-snapshot.json" ||
	fail "cap truncation must not touch the runtime snapshot"
jq -e '.truncated_tails == true' "${cap_dest}/manifest.json" >/dev/null ||
	fail "manifest must record tail truncation"
unset GOLDEN_CORPUS_DIAG_MAX_KB

# Case 5: still over the cap after truncation fails loud with no bundle.
mkdir -p "${fixture}/huge/work/logs" "${fixture}/huge/capture" "${fixture}/huge-tmp"
printf '{"snapshot":"repository-runtime"}\n' >"${fixture}/huge/work/logs/e2e-20repo-repository-runtime-snapshot.json"
head -c 2000000 /dev/zero | tr '\0' 'c' >"${fixture}/huge/capture/big.jsonl"
printf 'small log line\n' >"${fixture}/huge/work/logs/api.log"
export RUNNER_TEMP="${fixture}/huge-tmp"
export ESHU_DIFFERENTIAL_CAPTURE_DIR="${fixture}/huge/capture"
export PG_CALL_LOG="${fixture}/huge/pg-calls.txt"
export GOLDEN_CORPUS_DIAG_MAX_KB=1024
work_dir="${fixture}/huge/work"
log_dir="${fixture}/huge/work/logs"
expect_failure still_over_cap stage_failure_diagnostics "huge-job"
rg -q '::error' "${fixture}/still_over_cap.log" || fail "an over-cap bundle must fail loud"
test ! -e "${RUNNER_TEMP}/golden-corpus-failure/huge-job" ||
	fail "an over-cap bundle must leave nothing behind to upload"
unset GOLDEN_CORPUS_DIAG_MAX_KB

# Case 6: redact_gate_log unit probes — each redaction mechanism in
# isolation, straight through the function (no staging).
# (a) a standalone long-base64 run outside any key block redacts inline
# while the surrounding line context survives.
printf 'context before 0123456789\n%s\ncontext after 0123456789\n' \
	'c3RyYWRkbGVib2R5ZGlhZ2tleWxpbmUxMjM0NTY3ODkwQUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVowMTIzNDU2Nzg5Ky8vPAD=' |
	redact_gate_log >"${fixture}/b64.out" ||
	fail "redact_gate_log must handle a standalone base64 run"
if rg -q -F -- 'c3RyYWRkbGVib2R5ZGlhZ2tleW' "${fixture}/b64.out"; then
	fail "a standalone long-base64 run must redact"
fi
rg -q -F -- 'context before 0123456789' "${fixture}/b64.out" ||
	fail "base64 redaction must stay inline, not nuke the line"
# (b) an END line with no BEGIN above it redacts back to the tail start,
# keeping only what follows the END line.
printf 'tail filler line one\nProc-Type: 4,ENCRYPTED\n%s\n%s\npost-end survivor\n' \
	'c3RyYWRkbGVib2R5ZGlhZ2tleWxpbmUxMjM0NTY3ODkwQUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVowMTIzNDU2Nzg5Ky8vPAD=' \
	'-----END RSA PRIVATE KEY-----' |
	redact_gate_log >"${fixture}/straddle.out" ||
	fail "redact_gate_log must handle END-without-BEGIN"
if rg -q -F -- 'Proc-Type' "${fixture}/straddle.out"; then
	fail "END-without-BEGIN must redact back to the tail start"
fi
if rg -q -F -- 'tail filler line one' "${fixture}/straddle.out"; then
	fail "END-without-BEGIN must redact the tail start itself"
fi
rg -q -F -- 'post-end survivor' "${fixture}/straddle.out" ||
	fail "END-without-BEGIN redaction must stop at the END line"

echo 'PASS: failure diagnostics staging is complete, redacted, and fail-closed'
