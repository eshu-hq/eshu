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
golden_corpus_start_replay() { captured+=("$1|$2|$3"); }
unset GATE_MIN_COLLECTOR_SOURCES || true
golden_corpus_start_cassette_replays
[[ "${GATE_MIN_COLLECTOR_SOURCES}" == "19" ]] || fail "source floor = ${GATE_MIN_COLLECTOR_SOURCES}, want 19"
[[ "${#captured[@]}" == "19" ]] || fail "replay count = ${#captured[@]}, want 19"
[[ "${captured[18]}" == "semantic-extraction-cassette|collector-prometheus-mimir|semanticextraction" ]] ||
	fail "semantic alias parsed as ${captured[18]}"

if (
	die() { exit 75; }
	cassette_replay_alias_specs=("missing-fields")
	unset GATE_MIN_COLLECTOR_SOURCES || true
	golden_corpus_start_cassette_replays
); then
	fail "malformed replay alias was accepted"
else
	status=$?
	[[ "${status}" == "75" ]] || fail "malformed alias returned ${status}, want 75"
fi

printf 'PASS: golden cassette replay helper\n'
