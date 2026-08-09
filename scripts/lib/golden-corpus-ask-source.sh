#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Starts the closed, credential-free OpenAI-compatible provider used by B-7's
# deployed Ask proof. The caller owns cleanup through start_bg.
# shellcheck disable=SC2154

golden_ask_source_profile_json() {
	local endpoint="$1"
	jq -cn --arg endpoint "${endpoint}" '{profiles: [{
      profile_id: "golden-ask-provider",
      display_name: "Golden Ask provider",
      provider_kind: "openai_compatible",
      credential_source: {kind: "cloud_workload_identity"},
      model_id: "golden-ask",
      endpoint_profile_id: $endpoint,
      source_classes: ["agent_reasoning"]
    }]}'
}

golden_ask_source_start() {
	local endpoint="http://127.0.0.1:${GATE_ASK_PROVIDER_PORT}"
	local ready=false
	local attempts="${GATE_ASK_PROVIDER_READY_ATTEMPTS:-30}"
	local delay="${GATE_ASK_PROVIDER_READY_SLEEP_SECONDS:-1}"

	if host_tcp_port_open "127.0.0.1" "${GATE_ASK_PROVIDER_PORT}"; then
		die "Ask provider fixture port ${GATE_ASK_PROVIDER_PORT} is already in use"
	fi
	export ESHU_ASK_ENABLED=true
	# The deployed proof exercises the real provider planning/tool loop and uses
	# the tool packet's deterministic prose. Narration is a distinct posture and
	# stays disabled so the mock never fabricates evidence-bearing prose.
	export ESHU_ASK_NARRATION_ENABLED=false
	ESHU_SEMANTIC_PROVIDER_PROFILES_JSON="$(golden_ask_source_profile_json "${endpoint}")" ||
		die "failed to build golden Ask provider profile"
	export ESHU_SEMANTIC_PROVIDER_PROFILES_JSON

	MOCK_OPENAI_COMPATIBLE_LISTEN_ADDR="127.0.0.1:${GATE_ASK_PROVIDER_PORT}" \
		start_bg mock-openai-compatible ask_provider_pid "${bin_dir}/eshu-mock-openai-compatible"
	for ((i = 0; i < attempts; i++)); do
		if curl -fsS "${endpoint}/health" >/dev/null 2>&1; then
			ready=true
			break
		fi
		sleep "${delay}"
	done
	[[ "${ready}" == "true" ]] || {
		tail -30 "${log_dir}/mock-openai-compatible.log" >&2 || true
		die "mock OpenAI-compatible /health never returned on port ${GATE_ASK_PROVIDER_PORT}"
	}
}
