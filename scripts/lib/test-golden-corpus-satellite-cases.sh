#!/usr/bin/env bash
# Satellite MCP binary cases for the B-7 gate mirror test (#9670).
# This file is sourced by scripts/test-verify-golden-corpus-gate.sh after its
# matcher helpers and shared paths are initialized; it needs require_in,
# host_helpers_lib, and script from that caller.
#
# The in-tree go/cmd/mcp-server is gone: the host build must route mcp-server
# to the pinned satellite clone, or the gate dies at
# `stat go/cmd/mcp-server`. A silent revert reopens every MCP-backed leg
# (corpus-gate x2, differential x5, latency-gate).
require_in "satellite mcp-server build route" "${host_helpers_lib}" "build_satellite_bin() {"
require_in "satellite mcp-server clone pin" "${host_helpers_lib}" "eshu-hq/eshu-mcp.git"
# The satellite honors the same ESHU_MCP_* env except the listen address and
# core origin, which moved to flags (#9670): the old ESHU_MCP_ADDR-only start
# would bind :8080 and die without --core-base-url, and an unmapped profile
# would serve production truth against a local_full_stack snapshot.
require_in "satellite mcp-server start flags" "${script}" '-core-base-url="http://localhost:${GATE_API_PORT}"'
require_in "satellite mcp-server addr flag" "${script}" '-addr=":${GATE_MCP_PORT}"'
require_in "satellite mcp query profile mapping" "${script}" 'ESHU_MCP_QUERY_PROFILE="${ESHU_QUERY_PROFILE:-}"'
latency_script="${repo_root}/scripts/verify-read-api-latency-gate.sh"
require_in "satellite mcp-server latency start flags" "${latency_script}" '-core-base-url="http://127.0.0.1:${GATE_API_PORT}"'
require_in "satellite mcp-server latency addr flag" "${latency_script}" '-addr="127.0.0.1:${GATE_MCP_PORT}"'
require_in "satellite mcp query profile mapping in latency gate" "${latency_script}" 'ESHU_MCP_QUERY_PROFILE="${ESHU_QUERY_PROFILE:-}"'
require_in "satellite mcp query profile mapping" "${script}" 'ESHU_MCP_QUERY_PROFILE="${ESHU_QUERY_PROFILE:-}"'

satellite_cases_completed=1
