#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/golden-corpus-ask-source.sh
source "${repo_root}/scripts/lib/golden-corpus-ask-source.sh"

fail() { printf 'test-golden-corpus-ask-source: %s\n' "$*" >&2; exit 1; }
die() { fail "$@"; }

case_dir="$(mktemp -d -t golden-ask-source.XXXXXX)"
trap 'rm -rf "${case_dir}"' EXIT
bin_dir="${case_dir}/bin"
log_dir="${case_dir}/logs"
mkdir -p "${bin_dir}" "${log_dir}"
touch "${bin_dir}/eshu-mock-openai-compatible"
GATE_ASK_PROVIDER_PORT=39191
started=0

host_tcp_port_open() { return 1; }
start_bg() {
	[[ "$1" == "mock-openai-compatible" ]] || fail "wrong process name: $1"
	[[ "$2" == "ask_provider_pid" ]] || fail "wrong pid variable: $2"
	[[ "$3" == "${bin_dir}/eshu-mock-openai-compatible" ]] || fail "wrong binary: $3"
	started=1
	printf -v "$2" '%s' 4242
}
curl() { return 0; }

golden_ask_source_start
[[ "${started}" == "1" ]] || fail "mock provider was not started"
[[ "${ESHU_ASK_ENABLED}" == "true" ]] || fail "Ask was not enabled"
[[ "${ESHU_ASK_NARRATION_ENABLED}" == "false" ]] || fail "narration posture was not pinned"
jq -e --arg endpoint "http://127.0.0.1:${GATE_ASK_PROVIDER_PORT}" '
  .profiles | length == 1
  and .[0].provider_kind == "openai_compatible"
  and .[0].credential_source.kind == "cloud_workload_identity"
  and .[0].endpoint_profile_id == $endpoint
  and .[0].source_classes == ["agent_reasoning"]
' <<<"${ESHU_SEMANTIC_PROVIDER_PROFILES_JSON}" >/dev/null || fail "provider profile JSON drifted"

host_tcp_port_open() { return 0; }
if (golden_ask_source_start); then
	fail "occupied provider port was accepted"
fi

printf 'PASS: golden Ask provider source helper\n'
