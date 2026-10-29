#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Starts the credential-free Prometheus-compatible range source used by B-7.
# The caller owns process cleanup through start_bg's bg_pids registration.
# shellcheck disable=SC2154

golden_metrics_source_json() {
	local source_url="$1"
	# Issue #9323: the remaining entries register the vulnerability-intelligence
	# fixture instances the B-7 readiness assertions read. They reuse the
	# cassette scopes' own collector_instance_id values, which #9227's
	# coverage tests pin verbatim — the gate registers those identities rather
	# than renaming them. The API metrics selector (go/cmd/api/metrics_source.go)
	# filters by collector_kind, so the extra entries are invisible to the
	# Prometheus source wiring. The NVD feed_url must match the endpoint the
	# cassette's source_snapshot facts record, or committed-coverage matching
	# finds no snapshot. CVE-2026-09000 is a configured-but-never-collected
	# target: it has no cassette facts and gets no seeded state, so it reads
	# pending.
	jq -cn --arg source_url "${source_url}" '[{
	  instance_id: "golden-prometheus-range",
	  collector_kind: "prometheus_mimir",
	  mode: "continuous",
	  enabled: true,
	  bootstrap: false,
	  claims_enabled: false,
	  display_name: "Golden Prometheus range source",
	  configuration: {
	    targets: [{provider: "prometheus", base_url: $source_url, tenant_id: "golden-corpus", enabled: true}]
	  }
	}, {
	  instance_id: "collector-vulnintel-nvd-known",
	  collector_kind: "vulnerability_intelligence",
	  mode: "continuous",
	  enabled: true,
	  bootstrap: false,
	  claims_enabled: false,
	  display_name: "Golden NVD known-CVE fixture",
	  configuration: {
	    targets: [
	      {scope_id: "vulnerability_intelligence:supply-chain-demo:nvd-known",
	       source: "nvd", cve_ids: ["CVE-2026-19227"],
	       feed_url: "https://nvd.supply-chain-demo.test/rest/json/cves/2.0"}
	    ]
	  }
	}, {
	  instance_id: "collector-vulnintel-nvd-zero",
	  collector_kind: "vulnerability_intelligence",
	  mode: "continuous",
	  enabled: true,
	  bootstrap: false,
	  claims_enabled: false,
	  display_name: "Golden NVD zero-result fixture",
	  configuration: {
	    targets: [
	      {scope_id: "vulnerability_intelligence:supply-chain-demo:nvd-zero",
	       source: "nvd", cve_ids: ["CVE-2026-09227"],
	       feed_url: "https://nvd.supply-chain-demo.test/rest/json/cves/2.0"}
	    ]
	  }
	}, {
	  instance_id: "collector-vulnintel-scd",
	  collector_kind: "vulnerability_intelligence",
	  mode: "continuous",
	  enabled: true,
	  bootstrap: false,
	  claims_enabled: false,
	  display_name: "Golden supply-chain-demo OSV fixture",
	  configuration: {
	    targets: [
	      {scope_id: "vulnerability_intelligence:supply-chain-demo:npm",
	       source: "osv", ecosystem: "npm",
	       queries: [{package: {name: "supply-chain-demo-lib", ecosystem: "npm"}, version: "1.2.2"}]},
	      {scope_id: "vulnerability_intelligence:supply-chain-demo:nvd-ready",
	       source: "nvd", cve_ids: ["CVE-2026-00000"],
	       feed_url: "https://nvd.supply-chain-demo.test/rest/json/cves/2.0"},
	      {scope_id: "vulnerability_intelligence:supply-chain-demo:nvd-pending",
	       source: "nvd", cve_ids: ["CVE-2026-09000"],
	       feed_url: "https://nvd.supply-chain-demo.test/rest/json/cves/2.0"}
	    ],
	    derive_from_owned_packages: {enabled: true, ecosystems: ["npm"]}
	  }
	}]'
}

golden_metrics_source_start() {
	local source_url="http://127.0.0.1:${GATE_PROMETHEUS_SOURCE_PORT}"
	local ready=false
	local attempts="${GATE_METRICS_SOURCE_READY_ATTEMPTS:-30}"
	local delay="${GATE_METRICS_SOURCE_READY_SLEEP_SECONDS:-1}"

	if host_tcp_port_open "127.0.0.1" "${GATE_PROMETHEUS_SOURCE_PORT}"; then
		die "Prometheus fixture port ${GATE_PROMETHEUS_SOURCE_PORT} is already in use"
	fi
	export ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID="golden-prometheus-range"
	ESHU_COLLECTOR_INSTANCES_JSON="$(golden_metrics_source_json "${source_url}")" ||
		die "failed to build Prometheus fixture collector configuration"
	export ESHU_COLLECTOR_INSTANCES_JSON

	MOCK_PROMETHEUS_MIMIR_LISTEN_ADDR="127.0.0.1:${GATE_PROMETHEUS_SOURCE_PORT}" \
		start_bg mock-prometheus-mimir metrics_source_pid "${bin_dir}/eshu-mock-prometheus-mimir"
	for ((i = 0; i < attempts; i++)); do
		if curl -fsS "${source_url}/health" >/dev/null 2>&1; then
			ready=true
			break
		fi
		sleep "${delay}"
	done
	[[ "${ready}" == "true" ]] || {
		tail -30 "${log_dir}/mock-prometheus-mimir.log" >&2 || true
		die "mock Prometheus/Mimir /health never returned on port ${GATE_PROMETHEUS_SOURCE_PORT}"
	}
}
