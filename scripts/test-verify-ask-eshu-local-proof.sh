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

# Unknown options must fail closed.
if bash "${verifier}" --not-a-flag >/dev/null 2>&1; then
	printf 'verifier accepted an unknown flag, want failure\n' >&2
	exit 1
fi

# Redaction: neither the verifier script nor the committed scorecard fixture may
# contain real-looking secrets, private hostnames, raw addresses, or Boats IP.
secret_pattern='AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----|xox[baprs]-[0-9A-Za-z-]+|\bsk-(live|prod)-[0-9A-Za-z]+'
host_pattern='boatsgroup|boattrader|\.internal\b|\.corp\b|[0-9]{1,3}(\.[0-9]{1,3}){3}'
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
