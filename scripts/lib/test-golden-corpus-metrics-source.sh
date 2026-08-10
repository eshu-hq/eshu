#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/golden-corpus-metrics-source.sh
source "${repo_root}/scripts/lib/golden-corpus-metrics-source.sh"

fail() { printf 'test-golden-corpus-metrics-source: %s\n' "$*" >&2; exit 1; }
die() { fail "$@"; }

case_dir="$(mktemp -d -t golden-metrics-source.XXXXXX)"
trap 'rm -rf "${case_dir}"' EXIT
bin_dir="${case_dir}/bin"
log_dir="${case_dir}/logs"
mkdir -p "${bin_dir}" "${log_dir}"
touch "${bin_dir}/eshu-mock-prometheus-mimir"
GATE_PROMETHEUS_SOURCE_PORT=39090
GATE_METRICS_SOURCE_READY_ATTEMPTS=1
GATE_METRICS_SOURCE_READY_SLEEP_SECONDS=0

host_tcp_port_open() { return 1; }
curl() { return 0; }
start_bg() {
	[[ "$1" == "mock-prometheus-mimir" ]] || fail "unexpected process name: $1"
	[[ "${MOCK_PROMETHEUS_MIMIR_LISTEN_ADDR:-}" == "127.0.0.1:39090" ]] || fail "listen address was not scoped to the gate port"
	printf -v "$2" '%s' 4242
}

golden_metrics_source_start
# shellcheck disable=SC2154  # assigned by the sourced helper through start_bg.
[[ "${metrics_source_pid}" == "4242" ]] || fail "mock process pid was not captured"
: >"${log_dir}/mock-prometheus-mimir.log"
[[ "${ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID}" == "golden-prometheus-range" ]] || fail "explicit instance id missing"
jq -e '
  length == 1
  and .[0].instance_id == "golden-prometheus-range"
  and .[0].collector_kind == "prometheus_mimir"
  and .[0].mode == "continuous"
  and .[0].enabled == true
  and .[0].configuration.targets == [{
    provider: "prometheus",
    base_url: "http://127.0.0.1:39090",
    tenant_id: "golden-corpus",
    enabled: true
  }]
' <<<"${ESHU_COLLECTOR_INSTANCES_JSON}" >/dev/null || fail "collector JSON does not pin the credential-free source"
rg -q 'token|credential|authorization' <<<"${ESHU_COLLECTOR_INSTANCES_JSON}" && fail "collector JSON contains a credential field"

if (
	die() { exit 73; }
	host_tcp_port_open() { return 0; }
	golden_metrics_source_start
); then
	fail "occupied source port was accepted"
else
	status=$?
	[[ "${status}" == "73" ]] || fail "occupied port returned ${status}, want 73"
fi

if (
	die() { exit 74; }
	host_tcp_port_open() { return 1; }
	curl() { return 1; }
	golden_metrics_source_start
); then
	fail "unready mock source was accepted"
else
	status=$?
	[[ "${status}" == "74" ]] || fail "unready source returned ${status}, want 74"
fi

printf 'PASS: golden metrics source helper\n'
