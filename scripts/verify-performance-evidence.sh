#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

base="${ESHU_PERFORMANCE_EVIDENCE_BASE:-}"
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  git -C "$repo_root" fetch --no-tags --depth=1 origin "$GITHUB_BASE_REF" >/dev/null 2>&1 || true
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base="origin/$GITHUB_BASE_REF"
  fi
fi
if [ -z "$base" ]; then
  if git -C "$repo_root" rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    base="HEAD~1"
  else
    printf 'verify-performance-evidence: no base commit available, skipping\n'
    exit 0
  fi
fi

changed_files=()
if git -C "$repo_root" diff --name-only "$base"...HEAD >/tmp/eshu-performance-evidence-files 2>/dev/null; then
  :
else
  git -C "$repo_root" diff --name-only "$base" HEAD >/tmp/eshu-performance-evidence-files
fi
while IFS= read -r file; do
  [ -n "$file" ] && changed_files+=("$file")
done </tmp/eshu-performance-evidence-files
rm -f /tmp/eshu-performance-evidence-files

is_go_runtime_file() {
  local path="$1"
  case "$path" in
    *.go) ;;
    *) return 1 ;;
  esac
  case "$path" in
    *_test.go|*_bench_test.go|*/testdata/*|*/vendor/*|*/doc.go) return 1 ;;
  esac
  case "$path" in
    go/internal/*|go/cmd/*) return 0 ;;
    *) return 1 ;;
  esac
}

is_hot_path_by_location() {
  local path="$1"
  case "$path" in
    go/internal/storage/cypher/*.go) return 0 ;;
    go/internal/storage/neo4j/*.go) return 0 ;;
    go/internal/storage/postgres/*.go) return 0 ;;
    go/internal/collector/*.go) return 0 ;;
    go/internal/collector/*/*.go) return 0 ;;
    go/internal/graph/*.go) return 0 ;;
    go/internal/projector/*.go) return 0 ;;
    go/internal/reducer/*.go) return 0 ;;
    go/internal/reducer/*/*.go) return 0 ;;
    go/internal/queue/*.go) return 0 ;;
    go/internal/runtime/*.go) return 0 ;;
    go/internal/workflow/*.go) return 0 ;;
    go/cmd/bootstrap-index/*.go) return 0 ;;
    go/cmd/ingester/*.go) return 0 ;;
    go/cmd/reducer/*.go) return 0 ;;
    go/cmd/collector-*/*.go) return 0 ;;
    *) return 1 ;;
  esac
}

is_hot_path_by_content() {
  local path="$1"
  local absolute="$repo_root/$path"
  [ -f "$absolute" ] || return 1

  rg -q -e '(^|[^A-Za-z])(MATCH|MERGE|UNWIND|DETACH DELETE|CREATE)([^A-Za-z]|$)' \
    -e '\b(ClaimBatch|ClaimLease|LeaseTTL|Heartbeat|MaxConcurrent|Worker|Workers|BatchSize|ExecuteGroup|ExecuteWrite|SKIP LOCKED|ON CONFLICT)\b' \
    -e '\b(errgroup|semaphore|WaitGroup|Mutex|RWMutex|chan|goroutine)\b' \
    -e 'go[[:space:]]+func[[:space:]]*\(' \
    "$absolute"
}

is_evidence_file() {
  local path="$1"
  case "$path" in
    docs/docs/adrs/*.md) return 0 ;;
    docs/docs/reference/*.md) return 0 ;;
    docs/docs/reference/**/*.md) return 0 ;;
    go/**/README.md) return 0 ;;
    go/**/AGENTS.md) return 0 ;;
    *) return 1 ;;
  esac
}

hot_files=()
evidence_files=()

for file in "${changed_files[@]}"; do
  if is_evidence_file "$file"; then
    evidence_files+=("$repo_root/$file")
  fi

  is_go_runtime_file "$file" || continue
  if is_hot_path_by_location "$file" || is_hot_path_by_content "$file"; then
    hot_files+=("$file")
  fi
done

if [ "${#hot_files[@]}" -eq 0 ]; then
  printf 'verify-performance-evidence: no hot Cypher/concurrency/runtime files changed\n'
  exit 0
fi

has_performance_evidence=1
has_observability_evidence=1
if [ "${#evidence_files[@]}" -gt 0 ]; then
  rg -q -e '(^|[[:space:]])(Performance Evidence|Benchmark Evidence|No-Regression Evidence):' \
    "${evidence_files[@]}" && has_performance_evidence=0
  rg -q -e '(^|[[:space:]])(Observability Evidence|No-Observability-Change):' \
    "${evidence_files[@]}" && has_observability_evidence=0
fi

if [ "$has_performance_evidence" -eq 0 ] && [ "$has_observability_evidence" -eq 0 ]; then
  printf 'verify-performance-evidence: benchmark and observability markers found for hot-path changes\n'
  exit 0
fi

{
  printf 'verify-performance-evidence: hot Cypher/concurrency/runtime changes need tracked evidence.\n'
  printf '\nChanged hot files:\n'
  for file in "${hot_files[@]}"; do
    printf '  - %s\n' "$file"
  done
  printf '\nAdd a tracked docs/ADR/package note changed in this PR with a benchmark marker:\n'
  printf '  - Performance Evidence:\n'
  printf '  - Benchmark Evidence:\n'
  printf '  - No-Regression Evidence:\n'
  printf '\nAlso include an observability marker:\n'
  printf '  - Observability Evidence:\n'
  printf '  - No-Observability-Change:\n'
  printf '\nThe note must name the baseline, after measurement, backend/version, input shape,\n'
  printf 'terminal queue or row counts, telemetry/log/status evidence, and why the change\n'
  printf 'is safe. PR text alone is not enough because future agents need the evidence\n'
  printf 'in the repo.\n'
  printf '\nThis gate is content-based as well as path-based, so new collectors that add\n'
  printf 'Cypher, worker claims, leases, batching, or concurrency knobs are covered.\n'
} >&2

exit 1
