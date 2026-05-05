#!/usr/bin/env bash
# Phase 1B benchmark: A/B test tx batch size, multiprocess parsing, flush/call batch sizes
# Run from the repo root with docker-compose stack available.
# Usage: ./scripts/benchmark_phase1b.sh <workspace-path> [results-dir]

set -euo pipefail

WORKSPACE="${1:?Usage: $0 <workspace-path> [results-dir]}"
RESULTS_DIR="${2:-${ESHU_PHASE1B_RESULTS_DIR:-./.benchmarks/phase1b-results}}"
mkdir -p "$RESULTS_DIR"

run_benchmark() {
    local name="$1"
    shift
    local env_overrides=("$@")

    echo "=== Benchmark: $name ==="
    echo "  Env: ${env_overrides[*]}"

    # Clean slate
    docker-compose down -v 2>/dev/null || true
    sleep 2

    # Start with overrides
    ESHU_FILESYSTEM_HOST_ROOT="$WORKSPACE" "${env_overrides[@]}" docker-compose up --build --force-recreate -d 2>&1 | tail -3

    # Wait for bootstrap to finish (poll every 5s, max 10 min)
    local waited=0
    while [ $waited -lt 600 ]; do
        status=$(docker inspect --format='{{.State.Status}}' eshu-bootstrap-index-1 2>/dev/null || echo "unknown")
        if [ "$status" = "exited" ]; then
            break
        fi
        sleep 5
        waited=$((waited + 5))
        if [ $((waited % 30)) -eq 0 ]; then
            echo "  Waiting... ${waited}s"
        fi
    done

    if [ $waited -ge 600 ]; then
        echo "  TIMEOUT after 600s"
        docker-compose logs bootstrap-index 2>&1 | tail -20 > "$RESULTS_DIR/${name}_timeout.log"
        return 1
    fi

    # Extract timing
    local finalization_line
    finalization_line=$(docker-compose logs bootstrap-index 2>&1 | rg "Finalization timings" | tail -1)
    local exit_code
    exit_code=$(docker inspect --format='{{.State.ExitCode}}' eshu-bootstrap-index-1 2>/dev/null || echo "unknown")

    echo "  Exit code: $exit_code"
    echo "  $finalization_line" \
        | sed 's/^.*| //' \
        | jq -r '
            "  Total: \((.extra_keys.total_seconds // "?"))s",
            "  function_calls: \((.extra_keys.function_calls_seconds // "?"))s",
            "  relationship_resolution: \((.extra_keys.relationship_resolution_seconds // "?"))s"
        ' 2>/dev/null || echo "  (could not parse timing)"

    # Save full logs
    docker-compose logs bootstrap-index 2>&1 > "$RESULTS_DIR/${name}_bootstrap.log"

    # Extract entity counts and repo discovery
    echo "  --- Entity summary ---"
    rg "Committed graph entities" "$RESULTS_DIR/${name}_bootstrap.log" \
        | sed 's/^.*| //' \
        | jq -r '
            .extra_keys as $extra
            | ($extra.entity_totals // {}) as $totals
            | "    \((($extra.repo_path // "?") | split("/") | last)): \(([$totals[]?] | add // 0)) entities (\($totals))"
        ' 2>/dev/null || true

    echo "  --- Memory ---"
    rg "After finalization" "$RESULTS_DIR/${name}_bootstrap.log" \
        | tail -1 \
        | sed 's/^.*| //' \
        | jq -r '"    \(.message // "?")"' \
        2>/dev/null || true

    echo ""
}

echo "Phase 1B Benchmarks — $(date)"
echo "Workspace: $WORKSPACE"
echo "Results: $RESULTS_DIR"
echo ""

# Baseline (current defaults: tx_batch=5, threaded, flush=2000, call_batch=250)
run_benchmark "baseline" env

# 1B.1: TX batch size = 10
run_benchmark "tx_batch_10" env ESHU_GRAPH_WRITE_TX_FILE_BATCH_SIZE=10

# 1B.1: TX batch size = 25
run_benchmark "tx_batch_25" env ESHU_GRAPH_WRITE_TX_FILE_BATCH_SIZE=25

# 1B.2: Multiprocess parsing
run_benchmark "multiprocess" env ESHU_REPO_FILE_PARSE_MULTIPROCESS=true

# 1B.3: Flush threshold = 10000 + call batch = 1000
# These are constants, not env vars, so we'd need code changes.
# Skip for now — the env-var-controlled knobs are the priority.

# Combined winner (assuming tx_batch=25 + multiprocess)
run_benchmark "combined" env ESHU_GRAPH_WRITE_TX_FILE_BATCH_SIZE=25 ESHU_REPO_FILE_PARSE_MULTIPROCESS=true

echo "=== All benchmarks complete ==="
echo "Results in: $RESULTS_DIR"
