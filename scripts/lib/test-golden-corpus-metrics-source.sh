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
  length == 4
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
# Issue #9323: the fixture also registers the vulnerability-intelligence
# instances the B-7 readiness assertions read. They reuse the cassette scopes'
# own collector_instance_id values (testdata/cassettes/vulnerabilityintelligence/
# supply-chain-demo.json), which #9227's coverage tests pin verbatim — the gate
# registers those identities rather than renaming them. They ride along in the
# same list without disturbing the metrics selection (the API selector filters
# by collector_kind, so the extra entries are invisible to it).
jq -e '
  length == 4
  and ([.[1,2,3].instance_id] | sort) == ["collector-vulnintel-nvd-known", "collector-vulnintel-nvd-zero", "collector-vulnintel-scd"]
  and ([.[1,2,3] | select(.collector_kind != "vulnerability_intelligence")] | length) == 0
  and ([.[1,2,3] | select(.mode != "continuous" or .enabled != true or .claims_enabled != false)] | length) == 0
  and ([.[1,2,3].configuration.targets[] | .scope_id] | unique | length) == 5
  and ([.[1,2,3].configuration.targets[] | select(.source == "nvd") | .cve_ids[0]] | sort) == ["CVE-2026-00000", "CVE-2026-09000", "CVE-2026-09227", "CVE-2026-19227"]
  and ([.[] | select(.instance_id == "collector-vulnintel-scd") | .configuration.targets[] | select(.source == "osv") | .ecosystem] | join(",")) == "npm"
  and ([.[] | select(.instance_id == "collector-vulnintel-scd") | .configuration.derive_from_owned_packages.enabled] | all) == true
' <<<"${ESHU_COLLECTOR_INSTANCES_JSON}" >/dev/null || fail "collector JSON does not configure the golden vulnerability-intelligence fixture instances"
rg -q 'token|credential|authorization' < <(printf '%s\n' "${ESHU_COLLECTOR_INSTANCES_JSON}") && fail "collector JSON contains a credential field"

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
