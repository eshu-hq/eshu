#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
list_only=false
deepseek=false
live_tmp_dir=""

usage() {
	# printf (a builtin, no pipe) instead of a heredoc: this body is over 512
	# bytes and would deadlock under Homebrew bash >= 5.1's pipe-buffer
	# heredoc write (#5074).
	printf '%s\n' \
		"Usage: $(basename "$0") [--deepseek] [--list]" \
		"" \
		"Runs the Ask Eshu local proof gate (issue #3332). By default this is fully" \
		"offline and secret-safe: it drives the real Ask Eshu runtime path (router mux," \
		"scoped-auth middleware, ask wiring, ask engine, openai-compatible provider" \
		"adapter against a local stub, runtime answer guardrail, and both the JSON and" \
		"SSE handlers) through CI-runnable Go integration tests, then scores the" \
		"committed redacted answer-quality scorecard fixture." \
		"" \
		"It proves these states without live DeepSeek credentials or a graph/Postgres" \
		"backend: ask disabled (503), missing provider (503), bad provider (503), the" \
		"scoped-token allowlist admitting POST /api/v0/ask, GET" \
		"/api/v0/status/answer-narration gate state, a clean cited answer succeeding on" \
		"JSON and SSE, and AKIA-key / Bearer-token / raw-address / uncited-claim" \
		"suppression on both JSON and SSE." \
		"" \
		"Pass --deepseek ONLY from a private operator environment that exports real" \
		"DeepSeek credentials. That section runs the live hosted end-to-end rerun and is" \
		"never executed in CI. It refuses to run unless the operator credential" \
		"environment variables are present, and it must never echo their values." \
		"" \
		"Use --list to print the proof commands without running them."
}

die() {
	printf 'verify-ask-eshu-local-proof: %s\n' "$*" >&2
	exit 1
}

cleanup() {
	if [[ -n "${live_tmp_dir:-}" ]]; then
		rm -rf "${live_tmp_dir}"
	fi
}

trap 'status=$?; cleanup; exit "${status}"' EXIT

while [[ $# -gt 0 ]]; do
	case "$1" in
		--list)
			list_only=true
			shift
			;;
		--deepseek)
			deepseek=true
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			die "unknown option: $1"
			;;
	esac
done

command -v go >/dev/null 2>&1 || die "go is required"
command -v rg >/dev/null 2>&1 || die "rg is required"
command -v bash >/dev/null 2>&1 || die "bash is required"
if [[ "${deepseek}" == "true" ]]; then
	command -v curl >/dev/null 2>&1 || die "curl is required for --deepseek"
fi

proof_pattern='TestAskLocalProof'
scorecard_fixture="cmd/eshu/testdata/ask-eshu-local-proof-scorecard.json"
ask_live_question='What service entrypoint does the demo repository expose?'

print_step() {
	local label="$1"
	shift
	printf '%s\n  %s\n' "${label}" "$*"
}

require_deepseek_env() {
	[[ -n "${ESHU_ASK_DEEPSEEK_API_KEY:-}" ]] || die "--deepseek requires ESHU_ASK_DEEPSEEK_API_KEY exported from a private operator environment"
	[[ -n "${ESHU_SEMANTIC_PROVIDER_PROFILES_JSON:-}" ]] || die "--deepseek requires ESHU_SEMANTIC_PROVIDER_PROFILES_JSON describing the live agent_reasoning DeepSeek profile"
	[[ -n "${ESHU_ASK_LOCAL_PROOF_BASE_URL:-}" ]] || die "--deepseek requires ESHU_ASK_LOCAL_PROOF_BASE_URL for the operator-local API endpoint"
	[[ -n "${ESHU_ASK_LOCAL_PROOF_API_TOKEN:-}" ]] || die "--deepseek requires ESHU_ASK_LOCAL_PROOF_API_TOKEN for the operator-local API endpoint"
}

curl_live_api() {
	local output="$1"
	shift
	curl --fail-with-body --silent --show-error --connect-timeout 5 --max-time 60 \
		-H "Authorization: Bearer ${ESHU_ASK_LOCAL_PROOF_API_TOKEN}" \
		"$@" >"${output}"
}

assert_publish_safe_file() {
	local file="$1"
	if rg --fixed-strings --quiet "${ESHU_ASK_DEEPSEEK_API_KEY}" "${file}"; then
		die "live proof response contained the DeepSeek credential"
	fi
	if rg --fixed-strings --quiet "${ESHU_ASK_LOCAL_PROOF_API_TOKEN}" "${file}"; then
		die "live proof response contained the API token"
	fi
	local secret_pattern='AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----|xox[baprs]-[0-9A-Za-z-]+|\bsk-(live|prod)-[0-9A-Za-z]+'
	local marine='boat'
	local group_suffix='sgroup'
	local marketplace_suffix='trader'
	local octet='[[:digit:]]{1,3}'
	local private_tld="\\.(internal|corp)\\b"
	local host_pattern="${marine}${group_suffix}|${marine}${marketplace_suffix}|${private_tld}|${octet}(\\.${octet}){3}"
	if rg --pcre2 --quiet "${secret_pattern}" "${file}"; then
		die "live proof response contained a secret-like value"
	fi
	if rg --pcre2 --ignore-case --quiet "${host_pattern}" "${file}"; then
		die "live proof response contained a private host or raw address"
	fi
}

assert_contains_literal() {
	local file="$1"
	local literal="$2"
	local label="$3"
	if ! rg --fixed-strings --quiet "${literal}" "${file}"; then
		die "${label} missing from live proof response"
	fi
}

assert_matches() {
	local file="$1"
	local pattern="$2"
	local label="$3"
	if ! rg --pcre2 --quiet "${pattern}" "${file}"; then
		die "${label} missing from live proof response"
	fi
}

run_deepseek_live_proof() {
	require_deepseek_env
	local base_url="${ESHU_ASK_LOCAL_PROOF_BASE_URL%/}"
	live_tmp_dir="$(mktemp -d)"

	local status_out="${live_tmp_dir}/status.json"
	local json_out="${live_tmp_dir}/ask.json"
	local sse_out="${live_tmp_dir}/ask.sse"

	run_step "operator-local GET /api/v0/status/answer-narration" \
		curl_live_api "${status_out}" "${base_url}/api/v0/status/answer-narration"
	assert_publish_safe_file "${status_out}"
	assert_matches "${status_out}" '"provider_configured"[[:space:]]*:[[:space:]]*true' "provider_configured=true"

	run_step "operator-local POST /api/v0/ask JSON" \
		curl_live_api "${json_out}" \
			-H "Content-Type: application/json" \
			-X POST \
			--data "{\"question\":\"${ask_live_question}\"}" \
			"${base_url}/api/v0/ask"
	assert_publish_safe_file "${json_out}"
	assert_contains_literal "${json_out}" '"answer_prose"' "answer_prose"
	assert_contains_literal "${json_out}" '"evidence_handles"' "evidence_handles"
	assert_contains_literal "${json_out}" '"truth"' "truth"

	run_step "operator-local POST /api/v0/ask SSE" \
		curl_live_api "${sse_out}" \
			-H "Content-Type: application/json" \
			-H "Accept: text/event-stream" \
			-X POST \
			--data "{\"question\":\"${ask_live_question}\"}" \
			"${base_url}/api/v0/ask"
	assert_publish_safe_file "${sse_out}"
	assert_contains_literal "${sse_out}" "event: answer" "SSE answer event"
	assert_contains_literal "${sse_out}" "event: done" "SSE done event"
	assert_contains_literal "${sse_out}" "data:" "SSE data frame"

	(
		cd "${repo_root}/go"
		run_step "answer-quality + publish-safety scorecard over the committed redacted fixture after live rerun" \
			go run ./cmd/eshu answer-quality-scorecard --from "${scorecard_fixture}"
	)
}

if [[ "${list_only}" == "true" ]]; then
	print_step "ask runtime path proof: disabled, missing/bad provider, status, JSON+SSE cited success, leak suppression" \
		"go test ./cmd/api -run '${proof_pattern}' -count=1"
	print_step "scoped-token allowlist admits POST /api/v0/ask" \
		"go test ./internal/query -run 'TestScopedHTTPRoute_Ask' -count=1"
	print_step "answer-quality + publish-safety scorecard over the committed redacted fixture" \
		"go run ./cmd/eshu answer-quality-scorecard --from ${scorecard_fixture}"
	print_step "committed scorecard fixture CLI regression" \
		"go test ./cmd/eshu -run 'TestAskEshuLocalProofScorecardFixturePasses' -count=1"
	if [[ "${deepseek}" == "true" ]]; then
		print_step "operator-local real DeepSeek end-to-end rerun: GET /api/v0/status/answer-narration" \
			"curl --fail-with-body --silent --show-error -H 'Authorization: Bearer <redacted>' \"\${ESHU_ASK_LOCAL_PROOF_BASE_URL}/api/v0/status/answer-narration\""
		print_step "operator-local real DeepSeek end-to-end rerun: POST /api/v0/ask JSON" \
			"curl --fail-with-body --silent --show-error -H 'Authorization: Bearer <redacted>' -H 'Content-Type: application/json' -X POST --data '{\"question\":\"${ask_live_question}\"}' \"\${ESHU_ASK_LOCAL_PROOF_BASE_URL}/api/v0/ask\""
		print_step "operator-local real DeepSeek end-to-end rerun: POST /api/v0/ask SSE" \
			"curl --fail-with-body --silent --show-error -H 'Authorization: Bearer <redacted>' -H 'Content-Type: application/json' -H 'Accept: text/event-stream' -X POST --data '{\"question\":\"${ask_live_question}\"}' \"\${ESHU_ASK_LOCAL_PROOF_BASE_URL}/api/v0/ask\""
		print_step "operator-local real DeepSeek end-to-end rerun scorecard" \
			"go run ./cmd/eshu answer-quality-scorecard --from ${scorecard_fixture}"
	else
		print_step "operator-local real DeepSeek end-to-end rerun skipped" \
			"rerun with --deepseek from a private operator environment that exports DeepSeek credentials"
	fi
	exit 0
fi

if [[ "${deepseek}" == "true" ]]; then
	require_deepseek_env
fi

run_step() {
	local label="$1"
	shift
	printf '==> %s\n' "${label}"
	"$@"
}

(
	cd "${repo_root}/go"
	run_step "ask runtime path proof (disabled/missing/bad provider, status, JSON+SSE cited success, leak suppression)" \
		go test ./cmd/api -run "${proof_pattern}" -count=1
	run_step "scoped-token allowlist admits POST /api/v0/ask" \
		go test ./internal/query -run 'TestScopedHTTPRoute_Ask' -count=1
	run_step "committed scorecard fixture CLI regression" \
		go test ./cmd/eshu -run 'TestAskEshuLocalProofScorecardFixturePasses' -count=1
	run_step "answer-quality + publish-safety scorecard over the committed redacted fixture" \
		go run ./cmd/eshu answer-quality-scorecard --from "${scorecard_fixture}"
)

if [[ "${deepseek}" == "true" ]]; then
	printf '==> operator-local real DeepSeek end-to-end rerun\n'
	run_deepseek_live_proof
else
	printf 'operator-local real DeepSeek end-to-end rerun skipped; rerun with --deepseek from a private operator environment that exports DeepSeek credentials (output must be redacted and never committed)\n'
fi

printf 'ask eshu local proof verification passed\n'
