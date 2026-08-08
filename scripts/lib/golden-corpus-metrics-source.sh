#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Starts the credential-free Prometheus-compatible range source used by B-7.
# The caller owns process cleanup through start_bg's bg_pids registration.
# shellcheck disable=SC2154

golden_metrics_source_json() {
	local source_url="$1"
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
