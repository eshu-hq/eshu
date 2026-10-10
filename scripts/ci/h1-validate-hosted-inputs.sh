#!/usr/bin/env bash
# Fail closed before a hosted H1 capture can be misidentified as exact source.
set -euo pipefail
if [[ -n "${GATE_API_BIN:-}" ]]; then
  printf 'H1 diagnostic cannot use a prebuilt API binary\n' >&2
  exit 1
fi
if [[ -n "${GATE_STACK_DIR:-}" ]]; then
  printf 'H1 diagnostic cannot use a foreign stack directory\n' >&2
  exit 1
fi
if [[ "${ESHU_GRAPH_BACKEND:-}" != neo4j || "${GATE_CONCURRENT_WORKERS:-}" != 4 ||
      "${GATE_CONCURRENT_REQUESTS:-}" != 20 || "${GATE_RUNS:-}" != 3 ||
      "${GATE_ITERATIONS:-}" != 20 || "${GATE_TOTAL_SCOPES:-}" != 800 ||
      "${GATE_NODES_PER_LABEL:-}" != 150000 || "${GATE_IAC_FACT_COUNT:-}" != 150000 ||
      "${GATE_SHARED_INTENT_COUNT:-}" != 2500000 ||
      "${GATE_BUDGETS:-}" != testdata/benchmarks/read-api-route-budgets.txt ||
      "${GATE_WORK_BUDGETS:-}" != testdata/benchmarks/read-api-route-work-budgets.txt ]]; then
  printf 'H1 workload differs from the declared hosted diagnostic profile\n' >&2
  exit 1
fi
