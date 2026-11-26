#!/usr/bin/env bash
#
# golden-corpus-query-shell-cases.sh — backend-split query-truth shell cases
# for scripts/test-verify-golden-corpus-gate.sh (issue #11513).
#
# Sourced by that mirror and by nothing else; it calls the mirror's fail()
# and shares its require_in/require/require_in_region/require_invocation
# helpers from golden-corpus-mirror-matcher.sh. It lives here rather than
# inline so the mirror stays under the repo's 500-line file rule, with the
# same gutted-or-early-return sentinel the other extracted case chunks use.
#
# Contract under test: the orchestrator never names a serving binary inline.
# It sources golden-corpus-query-shell.sh and calls build_query_shell (which
# builds eshu-core-api on Neo4j, eshu-local-api on NornicDB) and
# start_api_with_winners_resweep (which runs the #10880 production resweep,
# then delegates to start_query_shell, which starts exactly one shell under
# the unchanged api/api_pid names). The eshu-api identity #11456 deletes is
# built and started nowhere on the gate path — the static half of the
# binary-absence probe (the live half runs the Neo4j leg with go/cmd/api
# moved aside).

query_shell_lib="${repo_root}/scripts/lib/golden-corpus-query-shell.sh"
[[ -f "${query_shell_lib}" ]] || fail "missing query shell lib: ${query_shell_lib}"
bash -n "${query_shell_lib}" || fail "golden-corpus-query-shell.sh has a syntax error"
query_shell_host_helpers="${repo_root}/scripts/lib/golden-corpus-host-helpers.sh"
[[ -f "${query_shell_host_helpers}" ]] || fail "missing host helpers lib: ${query_shell_host_helpers}"

# The orchestrator delegates both halves to the extracted lib: build inline,
# start via the winners helper, which delegates to the backend split.
require "query shell lib source" "golden-corpus-query-shell.sh"
require_invocation "query shell build invocation" "build_query_shell"
require_invocation "query truth entry point" "start_api_with_winners_resweep"
require_in "entry point delegates to backend split" "${query_shell_host_helpers}" 'start_query_shell'

# Binary-absence static half: no code line of the orchestrator may build or
# start the eshu-api identity. Both needles are whole-line/prefixed so a
# comment naming the deleted binary (record-why prose) cannot satisfy them,
# and neither needle occurs in eshu-mcp-server or eshu-local-api lines.
if rg --quiet -- '^build_bin api$' "${script}"; then
	fail "orchestrator must not build the in-tree eshu-api identity (use build_query_shell, #11513)"
fi
if rg --quiet -- 'bin_dir}/eshu-api"' "${script}"; then
	fail "orchestrator must not start the eshu-api identity (use start_query_shell, #11513)"
fi

# The split itself lives in the extracted lib, keyed off ESHU_GRAPH_BACKEND
# in each half (region-scoped: the key legitimately appears twice).
require_in_region "build keys off backend" "${query_shell_lib}" \
	'/^build_query_shell/,/^}/' '"${ESHU_GRAPH_BACKEND}"'
require_in_region "start keys off backend" "${query_shell_lib}" \
	'/^start_query_shell/,/^}/' '"${ESHU_GRAPH_BACKEND}"'
# Each half names its surviving shell exactly once in code; prose mentions
# of the same names in comments are skipped by the matcher.
require_in "neo4j shell build" "${query_shell_lib}" 'build_bin core-api'
require_in "neo4j shell start" "${query_shell_lib}" '"${bin_dir}/eshu-core-api"'
require_in "nornicdb shell build" "${query_shell_lib}" 'build_bin local-api'
require_in "nornicdb shell start" "${query_shell_lib}" '"${bin_dir}/eshu-local-api"'
# The extracted lib must never reference the deleted-identity binary path
# (comments name it bare, without the bin_dir prefix, so this stays honest).
if rg --quiet -- 'bin_dir}/eshu-api"' "${query_shell_lib}"; then
	fail "query shell lib must never reference the eshu-api binary path (#11513)"
fi
# Each half's /readyz failure names its own shell, so a timeout on the wrong
# backend cannot be mistaken for the other leg's outage.
require_in "neo4j readyz names the shell" "${query_shell_lib}" 'eshu-core-api /readyz never returned'
require_in "nornicdb readyz names the shell" "${query_shell_lib}" 'eshu-local-api /readyz never returned'
# The interim NornicDB name is emitted by exactly one build line in the
# shared host helpers (still shared with the read-api-latency gate, whose
# build_bin api consumer is out of #11513 scope and stays untouched).
require_in "local-api build emits the interim name" "${query_shell_host_helpers}" 'eshu-local-api'

query_shell_cases_completed=1
