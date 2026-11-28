#!/usr/bin/env bash
# verify-performance-evidence: require hot-path runtime PRs to carry
# Performance/Benchmark/No-Regression and Observability markers in the GitHub
# PR description — never in committed ephemeral Markdown files.
#
# Usage:
#   scripts/verify-performance-evidence.sh [--pr-body <file>]
#
# In CI (GITHUB_EVENT_NAME=pull_request), pass the PR description via
# ESHU_PERFORMANCE_EVIDENCE_PR_BODY or --pr-body. On non-PR events (push to
# main, schedule, workflow_dispatch) or local runs without --pr-body, the gate
# verifies hot-path detection and exits 0 once no PR body is in scope.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=scripts/lib/gate-diff-base.sh
. "${script_dir}/lib/gate-diff-base.sh"

pr_body_file="${ESHU_PERFORMANCE_EVIDENCE_PR_BODY_FILE:-}"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --pr-body)
      pr_body_file="${2:-}"
      [ -n "$pr_body_file" ] || { printf 'verify-performance-evidence: --pr-body requires a file path\n' >&2; exit 2; }
      shift 2
      ;;
    --help|-h)
      printf 'usage: %s [--pr-body <file>]\n' "${0##*/}"
      exit 0
      ;;
    *)
      printf 'verify-performance-evidence: unknown argument: %s\n' "$1" >&2
      exit 2
      ;;
  esac
done

repo_root="${ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$script_dir" rev-parse --show-toplevel 2>/dev/null \
    || (cd "${script_dir}/.." && pwd))"
fi

base="${ESHU_PERFORMANCE_EVIDENCE_BASE:-}"
merge_parent="$(eshu_gate_merge_ref_parent "$repo_root")"
if [ -n "$merge_parent" ]; then
  # On a GitHub pull_request merge ref (2-parent HEAD), parent 1 is the exact
  # base tip combined with the PR. github.event.pull_request.base.sha can lag
  # behind main when main advances, which would attribute main's hot-path
  # commits to an unrelated PR.
  base="$merge_parent"
fi
if [ -n "$base" ] && ! git -C "$repo_root" cat-file -e "${base}^{commit}" 2>/dev/null; then
  if eshu_gate_fetch_base "$repo_root" "$base" &&
    git -C "$repo_root" cat-file -e "${base}^{commit}" 2>/dev/null; then
    :
  else
    printf 'verify-performance-evidence: base %s is not in this checkout and could not be fetched.\n' "$base" >&2
    exit 1
  fi
fi
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  if ! git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    eshu_gate_fetch_base "$repo_root" "$GITHUB_BASE_REF:refs/remotes/origin/$GITHUB_BASE_REF" || true
  fi
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base="origin/$GITHUB_BASE_REF"
  fi
fi
if [ -z "$base" ]; then
  if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
    merge_base="$(git -C "$repo_root" merge-base origin/main HEAD 2>/dev/null || true)"
    if [ -n "$merge_base" ] && [ "$merge_base" != "$(git -C "$repo_root" rev-parse HEAD 2>/dev/null)" ]; then
      base="$merge_base"
    fi
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

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/eshu-performance-evidence.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

changed_files_path="${tmp_dir}/changed-files.txt"
if ! git -C "$repo_root" diff --name-only "$base"...HEAD >"$changed_files_path" 2>/dev/null; then
  git -C "$repo_root" diff --name-only "$base" HEAD >"$changed_files_path"
fi

changed_files=()
while IFS= read -r file; do
  [ -n "$file" ] && changed_files+=("$file")
done <"$changed_files_path"

is_go_runtime_file() {
  case "$1" in
    *_test.go|*_bench_test.go|*/testdata/*|*/vendor/*|*/doc.go) return 1 ;;
    go/internal/*.go|go/cmd/*.go) return 0 ;;
    *) return 1 ;;
  esac
}

is_hot_path_by_location() {
  case "$1" in
    go/internal/storage/cypher/*.go|go/internal/storage/neo4j/*.go|go/internal/storage/postgres/*.go) return 0 ;;
    go/internal/collector/*.go|go/internal/collector/*/*.go|go/internal/graph/*.go|go/internal/projector/*.go) return 0 ;;
    go/internal/reducer/*.go|go/internal/reducer/*/*.go|go/internal/queue/*.go|go/internal/runtime/*.go|go/internal/workflow/*.go) return 0 ;;
    go/cmd/bootstrap-index/*.go|go/cmd/ingester/*.go|go/cmd/reducer/*.go|go/cmd/collector-*/*.go) return 0 ;;
    *) return 1 ;;
  esac
}

is_runtime_config_file() {
  case "$1" in
    docker-compose*.yml|docker-compose*.yaml|deploy/helm/eshu/*.yaml|deploy/helm/eshu/*.yml|deploy/helm/eshu/**/*.yaml|deploy/helm/eshu/**/*.yml) return 0 ;;
    *) return 1 ;;
  esac
}

is_live_gate_coordination_file() {
  case "$1" in
    scripts/lib/live-gate-lock.sh|scripts/lib/golden-corpus-keep-marker.sh) return 0 ;;
    *) return 1 ;;
  esac
}

# Build comment-only set in one awk pass over the diff.
diff_file="${tmp_dir}/diff.txt"
if ! git -C "$repo_root" diff --unified=0 "$base"...HEAD >"$diff_file" 2>/dev/null; then
  git -C "$repo_root" diff --unified=0 "$base" HEAD >"$diff_file" 2>/dev/null || : >"$diff_file"
fi

comment_only_file="${tmp_dir}/comment-only.txt"
awk '
  substr($0, 1, 6) == "+++ b/" {
    cur = substr($0, 7)
    if (cur != "/dev/null" && cur != "b/dev/null") seen0[cur] = 1
    else cur = ""
    next
  }
  $0 == "+++ /dev/null" { cur = ""; next }
  substr($0, 1, 6) == "--- a/" || $0 == "--- /dev/null" { next }
  substr($0, 1, 1) == "+" || substr($0, 1, 1) == "-" {
    if (cur == "") next
    payload = substr($0, 2)
    first = substr(payload, 1, 1)
    if (payload == "" || substr(payload, 1, 2) == "//" || substr(payload, 1, 2) == "/*" || first == "#") next
    has1[cur] = 1
  }
  END { for (f in seen0) if (!(f in has1)) print f }
' "$diff_file" | LC_ALL=C sort -u >"$comment_only_file"

is_comment_only_change() {
  rg --no-config -Fxq -- "$1" "$comment_only_file"
}

content_hot_file="${tmp_dir}/content-hot.txt"
: >"$content_hot_file"
go_scan_list=()
if [ "${#changed_files[@]}" -gt 0 ]; then
  for f in "${changed_files[@]}"; do
    case "$f" in *.go) ;; *) continue ;; esac
    [ -f "$repo_root/$f" ] && go_scan_list+=("$repo_root/$f")
  done
fi
if [ "${#go_scan_list[@]}" -gt 0 ]; then
  idx=0
  total="${#go_scan_list[@]}"
  while [ "$idx" -lt "$total" ]; do
    chunk=("${go_scan_list[@]:idx:200}")
    rg_rc=0
    rg --no-config -l \
      -e '(^|[^A-Za-z])(MATCH|MERGE|UNWIND|DETACH DELETE|CREATE)([^A-Za-z]|$)' \
      -e '\b(ClaimBatch|ClaimLease|LeaseTTL|Heartbeat|MaxConcurrent|Worker|Workers|BatchSize|ExecuteGroup|ExecuteWrite|SKIP LOCKED|ON CONFLICT)\b' \
      -e '\b(errgroup|semaphore|WaitGroup|Mutex|RWMutex|chan|goroutine)\b' \
      -e 'go[[:space:]]+func[[:space:]]*\(' \
      "${chunk[@]}" >>"$content_hot_file" 2>/dev/null || rg_rc=$?
    if [ "$rg_rc" -gt 1 ]; then
      printf 'verify-performance-evidence: hot-content scan failed (exit %s)\n' "$rg_rc" >&2
      exit 1
    fi
    idx=$((idx + 200))
  done
fi

is_hot_path_by_content() {
  rg --no-config -Fxq -- "$repo_root/$1" "$content_hot_file"
}

is_runtime_config_by_content() {
  local path="$1"
  local abs="$repo_root/$path"
  local pattern='\b(ESHU_GRAPH_BACKEND|ESHU_[A-Z0-9_]*(WORKER|WORKERS|BATCH|TIMEOUT|PPROF|NORNICDB)[A-Z0-9_]*|NORNICDB_[A-Z0-9_]*(EMBEDDING|PPROF|BATCH|TIMEOUT|WORKER|WORKERS)[A-Z0-9_]*)\b'
  if [ -f "$abs" ] && rg --no-config -q -e "$pattern" "$abs"; then
    return 0
  fi
  if git -C "$repo_root" diff --unified=0 "$base"...HEAD -- "$path" 2>/dev/null | rg --no-config -q -e "$pattern"; then
    return 0
  fi
  return 1
}

hot_files=()
if [ "${#changed_files[@]}" -gt 0 ]; then
  for file in "${changed_files[@]}"; do
    if is_go_runtime_file "$file" && { is_hot_path_by_location "$file" || is_hot_path_by_content "$file"; }; then
      is_comment_only_change "$file" && continue
      hot_files+=("$file")
      continue
    fi
    if is_runtime_config_file "$file" && is_runtime_config_by_content "$file"; then
      is_comment_only_change "$file" && continue
      hot_files+=("$file")
      continue
    fi
    if is_live_gate_coordination_file "$file"; then
      is_comment_only_change "$file" && continue
      hot_files+=("$file")
    fi
  done
fi

if [ "${#hot_files[@]}" -eq 0 ]; then
  printf 'verify-performance-evidence: no hot Cypher/concurrency/runtime files changed\n'
  exit 0
fi

body_check_path="${tmp_dir}/pr-body.txt"
has_body_input=0
if [ -n "$pr_body_file" ]; then
  if [ ! -f "$pr_body_file" ]; then
    printf 'verify-performance-evidence: PR body file not found: %s\n' "$pr_body_file" >&2
    exit 2
  fi
  cp "$pr_body_file" "$body_check_path"
  has_body_input=1
elif [ "${GITHUB_EVENT_NAME:-}" = "pull_request" ] || [ "${GITHUB_EVENT_NAME:-}" = "pull_request_target" ]; then
  printf '%s\n' "${ESHU_PERFORMANCE_EVIDENCE_PR_BODY:-}" >"$body_check_path"
  has_body_input=1
elif [ -z "${GITHUB_EVENT_NAME:-}" ] && [ -n "${ESHU_PERFORMANCE_EVIDENCE_PR_BODY:-}" ]; then
  printf '%s\n' "${ESHU_PERFORMANCE_EVIDENCE_PR_BODY}" >"$body_check_path"
  has_body_input=1
fi

if [ "$has_body_input" -eq 0 ]; then
  printf 'verify-performance-evidence: %d hot-path file(s) changed; PR body markers are enforced on pull_request (pass --pr-body <file> to check a draft locally)\n' "${#hot_files[@]}"
  exit 0
fi

has_perf=1
has_obs=1
if [ -s "$body_check_path" ]; then
  rg --no-config -q -e '(^|[[:space:]])[*_]*(Performance Evidence|Benchmark Evidence|No-Regression Evidence)([[:space:]]*(\([^()]*\)|\[[^\[\]]*\]))?[*_]*[[:space:]]*:' "$body_check_path" && has_perf=0 || true
  rg --no-config -q -e '(^|[[:space:]])[*_]*(Observability Evidence|No-Observability-Change)([[:space:]]*(\([^()]*\)|\[[^\[\]]*\]))?[*_]*[[:space:]]*:' "$body_check_path" && has_obs=0 || true
fi

if [ "$has_perf" -eq 0 ] && [ "$has_obs" -eq 0 ]; then
  printf 'verify-performance-evidence: benchmark and observability markers found in PR body for hot-path changes\n'
  exit 0
fi

{
  printf 'verify-performance-evidence: hot Cypher/concurrency/runtime changes need markers in the GitHub PR description.\n'
  printf '\nChanged hot files:\n'
  for file in "${hot_files[@]}"; do
    printf '  - %s\n' "$file"
  done
  printf '\nAdd one performance marker to the PR description (do NOT commit ephemeral files or append markers to go/**/*.md):\n'
  printf '  - Performance Evidence:\n'
  printf '  - Benchmark Evidence:\n'
  printf '  - No-Regression Evidence:\n'
  printf '\nAnd one observability marker to the PR description:\n'
  printf '  - Observability Evidence:\n'
  printf '  - No-Observability-Change:\n'
  printf '\nState the baseline, after measurement, backend/version, input shape, and operator signal.\n'
} >&2
exit 1
