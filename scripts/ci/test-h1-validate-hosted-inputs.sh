#!/usr/bin/env bash
# Exercise the real diagnostic input guard without starting Compose.
set -euo pipefail
guard=scripts/ci/h1-validate-hosted-inputs.sh
base=(env GATE_API_BIN= GATE_STACK_DIR= ESHU_GRAPH_BACKEND=neo4j GATE_CONCURRENT_WORKERS=4 GATE_CONCURRENT_REQUESTS=20 GATE_RUNS=3 GATE_ITERATIONS=20 GATE_TOTAL_SCOPES=800 GATE_NODES_PER_LABEL=150000 GATE_IAC_FACT_COUNT=150000 GATE_SHARED_INTENT_COUNT=2500000 GATE_BUDGETS=testdata/benchmarks/read-api-route-budgets.txt GATE_WORK_BUDGETS=testdata/benchmarks/read-api-route-work-budgets.txt)
"${base[@]}" bash "$guard"
if output="$("${base[@]}" GATE_API_BIN=/tmp/foreign-api bash "$guard" 2>&1)"; then
  printf 'foreign API binary was accepted\n' >&2
  exit 1
fi
[[ "$output" == *'prebuilt API'* ]] || { printf 'wrong API rejection: %s\n' "$output" >&2; exit 1; }
if output="$("${base[@]}" GATE_STACK_DIR=/tmp/foreign-stack bash "$guard" 2>&1)"; then
  printf 'foreign stack was accepted\n' >&2
  exit 1
fi
[[ "$output" == *'foreign stack'* ]] || { printf 'wrong stack rejection: %s\n' "$output" >&2; exit 1; }
if output="$("${base[@]}" GATE_CONCURRENT_WORKERS=1 bash "$guard" 2>&1)"; then
  printf 'wrong concurrent workload was accepted\n' >&2
  exit 1
fi
[[ "$output" == *'H1 workload'* ]] || { printf 'wrong workload rejection: %s\n' "$output" >&2; exit 1; }
if output="$("${base[@]}" GATE_BUDGETS=/tmp/foreign-budgets bash "$guard" 2>&1)"; then
  printf 'foreign budget table was accepted\n' >&2
  exit 1
fi
[[ "$output" == *'H1 workload'* ]] || { printf 'wrong budget rejection: %s\n' "$output" >&2; exit 1; }
printf 'PASS: H1 exact hosted input guard\n'
