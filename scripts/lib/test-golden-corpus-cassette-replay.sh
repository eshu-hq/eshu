#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/golden-corpus-cassette-replay.sh
source "${repo_root}/scripts/lib/golden-corpus-cassette-replay.sh"

fail() { printf 'test-golden-corpus-cassette-replay: %s\n' "$*" >&2; exit 1; }
die() { fail "$@"; }

collector_specs=()
for index in {1..18}; do collector_specs+=("collector-${index}:cassette-${index}"); done
cassette_replay_alias_specs=("semantic-extraction-cassette:collector-prometheus-mimir:semanticextraction")
captured=()
# Replace only the launch step: pair recording is exercised separately below.
golden_corpus_start_replay() { captured+=("$1|$2|$3"); }
golden_corpus_start_cassette_replays
[[ "${#GATE_EXPECTED_SCOPE_PAIRS[@]}" == "0" ]] || fail "launch did not reset GATE_EXPECTED_SCOPE_PAIRS"
[[ "${#captured[@]}" == "19" ]] || fail "replay count = ${#captured[@]}, want 19"
[[ "${captured[18]}" == "semantic-extraction-cassette|collector-prometheus-mimir|semanticextraction" ]] ||
	fail "semantic alias parsed as ${captured[18]}"

if (
	die() { exit 75; }
	cassette_replay_alias_specs=("missing-fields")
	golden_corpus_start_cassette_replays
); then
	fail "malformed replay alias was accepted"
else
	status=$?
	[[ "${status}" == "75" ]] || fail "malformed alias returned ${status}, want 75"
fi

# The settle poll waits for exactly the pairs recorded here (#6965), so every
# scope of every cassette must be recorded with its own ids, in order, and a
# blank id must die rather than become an unmatchable pair.
pair_dir="$(mktemp -d -t golden-corpus-cassette-pairs.XXXXXX)"
trap 'rm -rf "${pair_dir}"' EXIT
printf '%s' '{"scopes":[{"scope_id":"s-a","generation_id":"g-1"},{"scope_id":"s-a","generation_id":"g-2"},{"scope_id":"s-b","generation_id":"g-3"}]}' >"${pair_dir}/ok.json"
GATE_EXPECTED_SCOPE_PAIRS=()
golden_corpus_record_launched_pairs "${pair_dir}/ok.json"
[[ "${#GATE_EXPECTED_SCOPE_PAIRS[@]}" == "3" ]] || fail "recorded ${#GATE_EXPECTED_SCOPE_PAIRS[@]} pairs, want 3"
[[ "${GATE_EXPECTED_SCOPE_PAIRS[1]}" == "s-a"$'\t'"g-2" ]] || fail "second pair recorded as '${GATE_EXPECTED_SCOPE_PAIRS[1]}'"
[[ "${GATE_EXPECTED_SCOPE_PAIRS[2]}" == "s-b"$'\t'"g-3" ]] || fail "third pair recorded as '${GATE_EXPECTED_SCOPE_PAIRS[2]}'"
for bad in '{"scopes":[{"scope_id":"s-a"}]}' '{"scopes":[{"generation_id":"g-1"}]}' '{"scopes":[{"scope_id":"","generation_id":"g-1"}]}' '{"scopes":[]}'; do
	printf '%s' "${bad}" >"${pair_dir}/bad.json"
	if (
		die() { exit 75; }
		GATE_EXPECTED_SCOPE_PAIRS=()
		golden_corpus_record_launched_pairs "${pair_dir}/bad.json"
	); then
		fail "cassette with a missing id or no scopes was accepted: ${bad}"
	else
		status=$?
		[[ "${status}" == "75" ]] || fail "bad cassette ${bad} returned ${status}, want 75"
	fi
done

printf 'PASS: golden cassette replay helper\n'
