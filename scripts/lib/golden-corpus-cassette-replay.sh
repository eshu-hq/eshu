#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Launches the credential-free cassette replay set and derives both settle
# thresholds from the actual inputs. Alias specs are name:command:directory so
# a generic cassette-capable binary can replay a source without a dedicated
# command or a colliding process log.
# shellcheck disable=SC2154

golden_corpus_start_replay() {
	local name="$1" cmd="$2" dir="$3"
	local cassette cassette_scopes cpid
	cassette="${repo_root}/testdata/cassettes/${dir}/${cassette_recording}"
	if [[ "${dir}" == "terraformstate" ]]; then
		cassette="${local_backend_cassette_path}"
	fi
	[[ -f "${cassette}" ]] || die "cassette not found: ${cassette}"
	cassette_scopes="$(jq -r '.scopes | length' "${cassette}")" ||
		die "failed to count scopes in cassette: ${cassette}"
	[[ "${cassette_scopes}" =~ ^[0-9]+$ ]] ||
		die "cassette scope count is not numeric: ${cassette} -> ${cassette_scopes}"
	GATE_EXPECTED_TOTAL_SCOPES=$((GATE_EXPECTED_TOTAL_SCOPES + cassette_scopes))
	start_bg "${name}" cpid "${bin_dir}/eshu-${cmd}" -mode=cassette -cassette-file="${cassette}"
	collector_pids+=("${cpid}")
	collector_names+=("${name}")
}

golden_corpus_start_cassette_replays() {
	local spec name cmd dir remainder
	collector_pids=()
	collector_names=()
	GATE_EXPECTED_TOTAL_SCOPES=0
	for spec in "${collector_specs[@]}"; do
		cmd="${spec%%:*}"
		dir="${spec##*:}"
		golden_corpus_start_replay "${cmd}" "${cmd}" "${dir}"
	done
	for spec in "${cassette_replay_alias_specs[@]}"; do
		[[ "${spec}" == *:*:* && "${spec#*:*:}" != *:* ]] ||
			die "cassette replay alias must be name:command:directory: ${spec}"
		name="${spec%%:*}"
		remainder="${spec#*:}"
		cmd="${remainder%%:*}"
		dir="${remainder##*:}"
		[[ -n "${name}" && -n "${cmd}" && -n "${dir}" ]] ||
			die "cassette replay alias contains an empty field: ${spec}"
		golden_corpus_start_replay "${name}" "${cmd}" "${dir}"
	done
	: "${GATE_MIN_COLLECTOR_SOURCES:=$((${#collector_specs[@]} + ${#cassette_replay_alias_specs[@]}))}"
}
