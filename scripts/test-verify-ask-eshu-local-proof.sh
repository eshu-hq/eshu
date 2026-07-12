#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-ask-eshu-local-proof.sh"
fixture="${repo_root}/go/cmd/eshu/testdata/ask-eshu-local-proof-scorecard.json"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -f "${verifier}" ]]; then
	printf 'missing verifier: %s\n' "${verifier}" >&2
	exit 1
fi
if [[ ! -x "${verifier}" ]]; then
	printf 'verifier is not executable: %s\n' "${verifier}" >&2
	exit 1
fi

# Syntax check.
bash -n "${verifier}"

# The --list output must name every proof command without running anything.
list_log="${tmp_dir}/list.log"
bash "${verifier}" --list >"${list_log}"

rg --fixed-strings --quiet "go test ./cmd/api -run 'TestAskLocalProof'" "${list_log}"
rg --fixed-strings --quiet "go test ./internal/query -run 'TestScopedHTTPRoute_Ask'" "${list_log}"
rg --fixed-strings --quiet "go run ./cmd/eshu answer-quality-scorecard --from" "${list_log}"
rg --fixed-strings --quiet "TestAskEshuLocalProofScorecardFixturePasses" "${list_log}"
rg --fixed-strings --quiet "JSON+SSE cited success" "${list_log}"
rg --fixed-strings --quiet "leak suppression" "${list_log}"

# The default --list run must NOT activate the operator DeepSeek section.
if rg --fixed-strings --quiet "operator-local real DeepSeek end-to-end rerun skipped" "${list_log}"; then
	:
else
	printf 'default --list output must mark the DeepSeek section skipped\n' >&2
	exit 1
fi

# The --deepseek --list output must list the operator-gated section.
deepseek_log="${tmp_dir}/deepseek.log"
bash "${verifier}" --deepseek --list >"${deepseek_log}"
rg --fixed-strings --quiet "operator-local real DeepSeek end-to-end rerun" "${deepseek_log}"
rg --fixed-strings --quiet "curl --fail-with-body" "${deepseek_log}"
rg --fixed-strings --quiet "GET /api/v0/status/answer-narration" "${deepseek_log}"
rg --fixed-strings --quiet "POST /api/v0/ask JSON" "${deepseek_log}"
rg --fixed-strings --quiet "Accept: text/event-stream" "${deepseek_log}"
rg --fixed-strings --quiet "answer-quality-scorecard --from" "${deepseek_log}"
if rg --fixed-strings --quiet "bring up the runtime stack" "${deepseek_log}"; then
	printf '%s\n' '--deepseek --list still contains manual-only runtime instructions' >&2
	exit 1
fi

# Unknown options must fail closed.
if bash "${verifier}" --not-a-flag >/dev/null 2>&1; then
	printf 'verifier accepted an unknown flag, want failure\n' >&2
	exit 1
fi

fake_bin="${tmp_dir}/bin"
mkdir -p "${fake_bin}"
# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes the
# entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-ask-eshu-local-proof-fake-curl.sh" >"${fake_bin}/curl"
cat >"${fake_bin}/go" <<'FAKE_GO'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${FAKE_GO_LOG:?}"
if [[ "$*" == *"answer-quality-scorecard"* && "${FAKE_SCORECARD_FAIL:-}" == "true" ]]; then
	exit 17
fi
exit 0
FAKE_GO
chmod +x "${fake_bin}/curl" "${fake_bin}/go"

run_fake_deepseek() {
	local out="$1"
	shift
	PATH="${fake_bin}:$PATH" \
		FAKE_CURL_LOG="${tmp_dir}/curl.log" \
		FAKE_GO_LOG="${tmp_dir}/go.log" \
		ESHU_ASK_DEEPSEEK_API_KEY="redacted-test-key" \
		ESHU_SEMANTIC_PROVIDER_PROFILES_JSON='{"profiles":[]}' \
		ESHU_ASK_LOCAL_PROOF_BASE_URL="https://localhost.example" \
		ESHU_ASK_LOCAL_PROOF_API_TOKEN="redacted-api-token" \
		"$@" bash "${verifier}" --deepseek >"${out}" 2>&1
}

# Missing live-proof configuration must fail before invoking curl or go.
missing_env_log="${tmp_dir}/missing-env.log"
if PATH="${fake_bin}:$PATH" \
	FAKE_CURL_LOG="${tmp_dir}/missing-curl.log" \
	FAKE_GO_LOG="${tmp_dir}/missing-go.log" \
	ESHU_ASK_DEEPSEEK_API_KEY="redacted-test-key" \
	ESHU_SEMANTIC_PROVIDER_PROFILES_JSON='{"profiles":[]}' \
	ESHU_ASK_LOCAL_PROOF_BASE_URL="https://localhost.example" \
	env -u ESHU_ASK_LOCAL_PROOF_API_TOKEN bash "${verifier}" --deepseek >"${missing_env_log}" 2>&1; then
	printf '%s\n' '--deepseek passed without ESHU_ASK_LOCAL_PROOF_API_TOKEN' >&2
	exit 1
fi
if rg --fixed-strings --quiet "ask eshu local proof verification passed" "${missing_env_log}"; then
	printf '%s\n' '--deepseek printed pass text after missing env failure' >&2
	exit 1
fi
if [[ -s "${tmp_dir}/missing-curl.log" || -s "${tmp_dir}/missing-go.log" ]]; then
	printf '%s\n' '--deepseek invoked proof commands before validating required env' >&2
	exit 1
fi

# With fake live services, --deepseek must run status, JSON Ask, SSE Ask, and
# scorecard commands before printing the final pass line.
>"${tmp_dir}/curl.log"
>"${tmp_dir}/go.log"
success_log="${tmp_dir}/success.log"
run_fake_deepseek "${success_log}" env
rg --fixed-strings --quiet "/api/v0/status/answer-narration" "${tmp_dir}/curl.log"
rg --fixed-strings --quiet "/api/v0/ask" "${tmp_dir}/curl.log"
rg --fixed-strings --quiet "Accept: text/event-stream" "${tmp_dir}/curl.log"
rg --fixed-strings --quiet "answer-quality-scorecard --from" "${tmp_dir}/go.log"
rg --fixed-strings --quiet "ask eshu local proof verification passed" "${success_log}"

# Live request, SSE framing, scorecard, and redaction failures must all fail
# closed without printing the final pass marker.
for failure in json sse; do
	>"${tmp_dir}/curl.log"
	>"${tmp_dir}/go.log"
	failure_log="${tmp_dir}/curl-${failure}.log"
	if run_fake_deepseek "${failure_log}" env FAKE_CURL_FAIL="${failure}"; then
		printf '%s\n' "--deepseek passed despite fake ${failure} curl failure" >&2
		exit 1
	fi
	if rg --fixed-strings --quiet "ask eshu local proof verification passed" "${failure_log}"; then
		printf '%s\n' "--deepseek printed pass text after ${failure} curl failure" >&2
		exit 1
	fi
done

bad_sse_log="${tmp_dir}/bad-sse.log"
if run_fake_deepseek "${bad_sse_log}" env FAKE_CURL_BAD_SSE=true; then
	printf '%s\n' '--deepseek passed despite malformed SSE transcript' >&2
	exit 1
fi

scorecard_failure_log="${tmp_dir}/scorecard-failure.log"
if run_fake_deepseek "${scorecard_failure_log}" env FAKE_SCORECARD_FAIL=true; then
	printf '%s\n' '--deepseek passed despite scorecard failure' >&2
	exit 1
fi
if rg --fixed-strings --quiet "ask eshu local proof verification passed" "${scorecard_failure_log}"; then
	printf '%s\n' '--deepseek printed pass text after scorecard failure' >&2
	exit 1
fi

leak_log="${tmp_dir}/leak.log"
if run_fake_deepseek "${leak_log}" env FAKE_CURL_LEAK=true; then
	printf '%s\n' '--deepseek passed despite secret-like live response' >&2
	exit 1
fi
if rg --fixed-strings --quiet "AKIAIOSFODNN7EXAMPLE" "${leak_log}"; then
	printf '%s\n' '--deepseek printed an unredacted live secret-like value' >&2
	exit 1
fi

# Redaction: neither the verifier script nor the committed scorecard fixture may
# contain real-looking secrets, private organization terms, hostnames, or raw addresses.
secret_pattern='AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----|xox[baprs]-[0-9A-Za-z-]+|\bsk-(live|prod)-[0-9A-Za-z]+'
marine='boat'
group_suffix='sgroup'
marketplace_suffix='trader'
octet='[[:digit:]]{1,3}'
private_tld='\.(internal|corp)\b'
host_pattern="${marine}${group_suffix}|${marine}${marketplace_suffix}|${private_tld}|${octet}(\.${octet}){3}"
for target in "${verifier}" "${fixture}"; do
	if rg --pcre2 --quiet "${secret_pattern}" "${target}"; then
		printf 'redaction failure: secret-like value found in %s\n' "${target}" >&2
		exit 1
	fi
	if rg --pcre2 --ignore-case --quiet "${host_pattern}" "${target}"; then
		printf 'redaction failure: private host/address/IP found in %s\n' "${target}" >&2
		exit 1
	fi
done

# The committed fixture must declare the v1 scorecard schema and cover all seven
# prompt families.
rg --fixed-strings --quiet '"version": "answer-quality-scorecard/v1"' "${fixture}"
for family in service_story code_topic incident_context supply_chain_impact \
	documentation_truth freshness_readiness hosted_onboarding_governance; do
	rg --fixed-strings --quiet "\"${family}\"" "${fixture}"
done

printf 'ask eshu local proof verifier tests passed\n'
