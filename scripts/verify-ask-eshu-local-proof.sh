#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
list_only=false
deepseek=false

usage() {
	cat <<USAGE
Usage: $(basename "$0") [--deepseek] [--list]

Runs the Ask Eshu local proof gate (issue #3332). By default this is fully
offline and secret-safe: it drives the real Ask Eshu runtime path (router mux,
scoped-auth middleware, ask wiring, ask engine, openai-compatible provider
adapter against a local stub, runtime answer guardrail, and both the JSON and
SSE handlers) through CI-runnable Go integration tests, then scores the
committed redacted answer-quality scorecard fixture.

It proves these states without live DeepSeek credentials or a graph/Postgres
backend: ask disabled (503), missing provider (503), bad provider (503), the
scoped-token allowlist admitting POST /api/v0/ask, GET
/api/v0/status/answer-narration gate state, a clean cited answer succeeding on
JSON and SSE, and AKIA-key / Bearer-token / raw-address / uncited-claim
suppression on both JSON and SSE.

Pass --deepseek ONLY from a private operator environment that exports real
DeepSeek credentials. That section runs the live hosted end-to-end rerun and is
never executed in CI. It refuses to run unless the operator credential
environment variables are present, and it must never echo their values.

Use --list to print the proof commands without running them.
USAGE
}

die() {
	printf 'verify-ask-eshu-local-proof: %s\n' "$*" >&2
	exit 1
}

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

proof_pattern='TestAskLocalProof'
scorecard_fixture="cmd/eshu/testdata/ask-eshu-local-proof-scorecard.json"

print_step() {
	local label="$1"
	shift
	printf '%s\n  %s\n' "${label}" "$*"
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
		print_step "operator-local real DeepSeek end-to-end rerun" \
			"docker compose up the runtime stack with operator DeepSeek creds, then POST /api/v0/ask; redact all output"
	else
		print_step "operator-local real DeepSeek end-to-end rerun skipped" \
			"rerun with --deepseek from a private operator environment that exports DeepSeek credentials"
	fi
	exit 0
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
	# Operator-gated: real DeepSeek end-to-end rerun. This refuses to run unless
	# the operator credential environment is present, and it never echoes the
	# credential value. CI never sets these, so this branch never runs in CI.
	: "${ESHU_ASK_DEEPSEEK_API_KEY:?--deepseek requires ESHU_ASK_DEEPSEEK_API_KEY exported from a private operator environment}"
	: "${ESHU_SEMANTIC_PROVIDER_PROFILES_JSON:?--deepseek requires ESHU_SEMANTIC_PROVIDER_PROFILES_JSON describing the live agent_reasoning DeepSeek profile}"
	printf '==> operator-local real DeepSeek end-to-end rerun\n'
	printf 'operator: bring up the runtime stack with the live DeepSeek profile, POST /api/v0/ask (JSON and SSE), capture a REDACTED transcript, and re-run the scorecard against the redacted capture. Do not commit any captured output.\n'
else
	printf 'operator-local real DeepSeek end-to-end rerun skipped; rerun with --deepseek from a private operator environment that exports DeepSeek credentials (output must be redacted and never committed)\n'
fi

printf 'ask eshu local proof verification passed\n'
