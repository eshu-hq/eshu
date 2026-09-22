#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Launches the credential-free cassette replay set and records every
# (scope_id, generation_id) pair it launches in GATE_EXPECTED_SCOPE_PAIRS, the
# exact set the settle poll (golden-corpus-collector-settle.sh) waits for. Alias
# specs are name:command:directory so a generic cassette-capable binary can
# replay a source without a dedicated command or a colliding process log.
# shellcheck disable=SC2154

golden_corpus_start_replay() {
	local name="$1" cmd="$2" dir="$3"
	local cassette cpid
	cassette="${repo_root}/testdata/cassettes/${dir}/${cassette_recording}"
	if [[ "${dir}" == "terraformstate" ]]; then
		cassette="${local_backend_cassette_path}"
	fi
	[[ -f "${cassette}" ]] || die "cassette not found: ${cassette}"
	golden_corpus_record_launched_pairs "${cassette}"
	start_bg "${name}" cpid "${bin_dir}/eshu-${cmd}" -mode=cassette -cassette-file="${cassette}"
	collector_pids+=("${cpid}")
	collector_names+=("${name}")
}

# golden_corpus_record_launched_pairs appends one "scope_id<TAB>generation_id"
# entry per scope of the cassette. The ids are the ones the collector commits:
# the cassette source passes ScopeID and GenerationID through unchanged
# (go/internal/replay/cassette/source.go). A missing or empty id dies here,
# because a blank pair can never be matched and would only surface later as
# an unexplained settle timeout.
golden_corpus_record_launched_pairs() {
	local cassette="$1" pairs line scope_id generation_id
	pairs="$(jq -r '.scopes[] | [(.scope_id // ""), (.generation_id // "")] | @tsv' "${cassette}")" ||
		die "failed to read scope/generation ids from cassette: ${cassette}"
	[[ -n "${pairs}" ]] || die "cassette carries no scopes: ${cassette}"
	while IFS= read -r line; do
		IFS=$'\t' read -r scope_id generation_id <<<"${line}"
		[[ -n "${scope_id}" && -n "${generation_id}" ]] ||
			die "cassette scope has an empty scope_id or generation_id: ${cassette} -> '${line}'"
		GATE_EXPECTED_SCOPE_PAIRS+=("${scope_id}"$'\t'"${generation_id}")
	done <<<"${pairs}"
}

golden_corpus_start_cassette_replays() {
	local spec name cmd dir remainder
	collector_pids=()
	collector_names=()
	GATE_EXPECTED_SCOPE_PAIRS=()
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
}
