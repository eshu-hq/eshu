#!/usr/bin/env bash
#
# golden-corpus-query-shell.sh — backend-split query-truth shell for the
# golden corpus gate orchestrator (issue #11513, part of #11457; refs
# #11456, #11467, #11514). Extracted into a lib chunk so the orchestrator
# stays under the 500-line cap.
#
# The gate used to build and start the in-tree ./cmd/api binary on every
# backend. That tree is slated for deletion (#11456), so the shell is
# split by backend here:
#
# - Neo4j leg: eshu-core-api (go/cmd/core-api), built from this checkout.
#   It fails closed on any other backend, so it can only ever serve the
#   supported Neo4j leg (#7331); it survives the go/cmd/api deletion.
# - NornicDB leg: eshu-local-api, the ./cmd/eshu-local-api tree (relocated
#   out of ./cmd/api under #11514) built under the surviving name. The
#   #11456 honesty note records the range-scan caveat — not here. If
#   NornicDB loses serving entirely, this leg's fate follows that same
#   product call (#11467); until then the leg keeps serving under the
#   surviving name.
#
# Either shell serves GATE_API_PORT, so the satellite eshu-mcp-server keeps
# its -core-base-url pointed at that port on both backends, and the cleanup
# trap keeps reaping the single api_pid under its unchanged name. Querying
# core directly is sufficient because relay fidelity to the shipped
# satellite is proven by the parity and latency legs, not by this gate.
#
# Requires (set by the orchestrator before either helper runs): bin_dir,
# log_dir, ESHU_GRAPH_BACKEND, GATE_API_PORT, and build_bin/start_bg/
# wait_for_gate_http plus the die()/log() functions. Shell selection keys
# off ESHU_GRAPH_BACKEND at call time.

# build_query_shell builds the query-truth binary for the active backend and
# nothing else: on Neo4j the surviving core binary, elsewhere the interim
# name #11456 still provides. It never touches the deleted identity.
build_query_shell() {
	if [[ "${ESHU_GRAPH_BACKEND}" == "neo4j" ]]; then
		build_bin core-api
	else
		build_bin local-api
	fi
}

# start_query_shell launches the backend shell built above under the
# long-standing api/api_pid names and waits for its backend-gated /readyz.
start_query_shell() {
	if [[ "${ESHU_GRAPH_BACKEND}" == "neo4j" ]]; then
		log "start eshu-core-api for query truth"
		start_bg api api_pid "${bin_dir}/eshu-core-api" --addr=":${GATE_API_PORT}"
		wait_for_gate_http "eshu-core-api /readyz never returned on port ${GATE_API_PORT}" "http://localhost:${GATE_API_PORT}/readyz" "api"
	else
		log "start eshu-local-api for query truth (NornicDB session shell, #11514)"
		start_bg api api_pid "${bin_dir}/eshu-local-api"
		wait_for_gate_http "eshu-local-api /readyz never returned on port ${GATE_API_PORT}" "http://localhost:${GATE_API_PORT}/readyz" "api"
	fi
}
