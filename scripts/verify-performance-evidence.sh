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
changed_files_path="$(mktemp "${TMPDIR:-/tmp}/eshu-performance-evidence-files.XXXXXX")"
if git -C "$repo_root" diff --name-only "$base"...HEAD >"$changed_files_path" 2>/dev/null; then
  :
else
  git -C "$repo_root" diff --name-only "$base" HEAD >"$changed_files_path"
fi
while IFS= read -r file; do
  [ -n "$file" ] && changed_files+=("$file")
done <"$changed_files_path"
rm -f "$changed_files_path"

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

is_runtime_config_file() {
  local path="$1"
  case "$path" in
    docker-compose*.yml|docker-compose*.yaml) return 0 ;;
    deploy/helm/eshu/**/*.yaml|deploy/helm/eshu/**/*.yml) return 0 ;;
    deploy/helm/eshu/*.yaml|deploy/helm/eshu/*.yml) return 0 ;;
    *) return 1 ;;
  esac
}

is_runtime_config_by_content() {
  local path="$1"
  local absolute="$repo_root/$path"
  local pattern='\b(ESHU_GRAPH_BACKEND|ESHU_[A-Z0-9_]*(WORKER|WORKERS|BATCH|TIMEOUT|PPROF|NORNICDB)[A-Z0-9_]*|NORNICDB_[A-Z0-9_]*(EMBEDDING|PPROF|BATCH|TIMEOUT|WORKER|WORKERS)[A-Z0-9_]*)\b'

  if [ -f "$absolute" ] && rg -q -e "$pattern" "$absolute"; then
    return 0
  fi
  if git -C "$repo_root" diff --unified=0 "$base"...HEAD -- "$path" 2>/dev/null | rg -q -e "$pattern"; then
    return 0
  fi
  if git -C "$repo_root" diff --unified=0 "$base" HEAD -- "$path" 2>/dev/null | rg -q -e "$pattern"; then
    return 0
  fi
  return 1
}

# Pre-fetch the full diff once so comment-only checks are O(1) hash lookups
# instead of one git invocation per file. Empty if the diff is unavailable.
_perf_diff_cache="$(git -C "$repo_root" diff --unified=0 "$base"...HEAD 2>/dev/null || true)"

# Newline-delimited map of changed files whose diff contains at least one
# non-comment, non-whitespace added/removed line. Files absent from the map had
# no diff at all; files mapped to 0 had only comments/blanks; files mapped to 1
# had real code changes. Keep this Bash-3 compatible for macOS hook runners.
_perf_code_change_map=""
if [ -n "${_perf_diff_cache}" ]; then
  _perf_cur=""
  while IFS= read -r line; do
    case "${line}" in
      "+++ b/"*)
        _perf_cur="${line#+++ b/}"
        # Deleted files show /dev/null; rename targets show new path.
        if [ "${_perf_cur}" != "/dev/null" ] && [ "${_perf_cur}" != "b/dev/null" ]; then
          _perf_code_change_map="${_perf_code_change_map}${_perf_cur}"$'\t'"0"$'\n'
        else
          _perf_cur=""
        fi
        ;;
      "+"*|"-")
        [ -z "${_perf_cur}" ] && continue
        _perf_payload="${line:1}"
        # Comment or blank: Go line (//), block markers (/* * */), shell/
        # YAML (#), or empty. Anything else flips the file to code-change.
        case "${_perf_payload}" in
          "//"*|"/"*"|"*"*"|"#"*|"") continue ;;
        esac
        _perf_code_change_map="${_perf_code_change_map}${_perf_cur}"$'\t'"1"$'\n'
        ;;
    esac
  # Feed the loop via process substitution rather than a `<<<` here-string:
  # bash 5.3.x hangs indefinitely on a here-string that feeds a while-read loop
  # once the diff crosses a byte threshold (reproduced on Homebrew bash 5.3.15,
  # Apple Silicon; 0% CPU, never returns). printf restores the trailing newline
  # that `<<<` would have added, so the final diff line is still read — using
  # `printf '%s'` (no newline) would drop it because _perf_diff_cache is captured
  # via command substitution, which strips the trailing newline, and a
  # last-line-only hot change would then be misread as comment-only. Do not
  # revert to `<<<` or to `printf '%s'`; see
  # scripts/test-verify-performance-evidence.sh (large-diff and last-line cases).
  done < <(printf '%s\n' "${_perf_diff_cache}")
  unset _perf_cur _perf_payload
fi

# True when every added/removed line in the diff for `path` is a comment
# or whitespace-only line. Used to suppress false positives when a hot-path
# file gets touched by a comment-only rollout (for example, adding an SPDX
# header to every .go file) where there is no actual runtime change. A
# file with any non-comment code change returns false so the gate still
# fires.
#
# Recognises Go line comments (//), block-comment markers (/* * */), shell
# and YAML comments (#), and blank lines. New/deleted/renamed files
# default to false (gate fires) — we cannot tell comment-only intent from
# those cheaply, and defaulting to "gate fires" is the safe side.
is_comment_only_change() {
  local path="$1"
  [ -n "${_perf_code_change_map}" ] || return 1
  printf '%s' "${_perf_code_change_map}" | awk -F '\t' -v path="$path" '
    $1 == path { value = $2 }
    END { exit !(value == "0") }
  '
}

is_evidence_file() {
  local path="$1"
  case "$path" in
    docs/public/adrs/*.md) return 0 ;;
    docs/public/reference/*.md) return 0 ;;
    docs/public/reference/**/*.md) return 0 ;;
    go/*/evidence-*.md|go/*/*/evidence-*.md|go/*/*/*/evidence-*.md|go/*/*/*/*/evidence-*.md|go/*/*/*/*/*/evidence-*.md) return 0 ;;
    go/*/README.md|go/*/*/README.md|go/*/*/*/README.md|go/*/*/*/*/README.md|go/*/*/*/*/*/README.md) return 0 ;;
    go/*/AGENTS.md|go/*/*/AGENTS.md|go/*/*/*/AGENTS.md|go/*/*/*/*/AGENTS.md|go/*/*/*/*/*/AGENTS.md) return 0 ;;
    *) return 1 ;;
  esac
}

hot_files=()
evidence_files=()

if [ "${#changed_files[@]}" -gt 0 ]; then
  for file in "${changed_files[@]}"; do
    if is_evidence_file "$file" && [ -f "$repo_root/$file" ]; then
      evidence_files+=("$repo_root/$file")
    fi

    if is_go_runtime_file "$file" \
      && { is_hot_path_by_location "$file" || is_hot_path_by_content "$file"; }; then
      # Hot-path file. If the only changes are comments or whitespace
      # (e.g. an SPDX-header rollout), the gate does not apply because
      # no runtime behaviour changed.
      if is_comment_only_change "$file"; then
        continue
      fi
      hot_files+=("$file")
      continue
    fi

    if is_runtime_config_file "$file" && is_runtime_config_by_content "$file"; then
      if is_comment_only_change "$file"; then
        continue
      fi
      hot_files+=("$file")
    fi
  done
fi

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
  printf 'Cypher, worker claims, leases, batching, concurrency knobs, or runtime\n'
  printf 'Compose/Helm settings are covered.\n'
} >&2

exit 1
