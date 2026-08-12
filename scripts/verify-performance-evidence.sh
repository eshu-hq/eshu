#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

base="${ESHU_PERFORMANCE_EVIDENCE_BASE:-}"
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  # An explicit destination refspec is required: `git fetch origin <branch>`
  # with no `:<dst>` only ever updates FETCH_HEAD, never
  # refs/remotes/origin/<branch> -- this is real git behavior independent of
  # any configured remote.origin.fetch, so under actions/checkout@v5's actual
  # narrow/shallow setup `origin/$GITHUB_BASE_REF` never resolved and `base`
  # silently fell through to HEAD~1 (last commit only) in every real CI run
  # (eshu-hq/eshu#5542 follow-up).
  git -C "$repo_root" fetch --no-tags --depth=1 origin \
    "$GITHUB_BASE_REF:refs/remotes/origin/$GITHUB_BASE_REF" >/dev/null 2>&1 || true
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

is_live_gate_coordination_file() {
  local path="$1"
  case "$path" in
    scripts/lib/live-gate-lock.sh) return 0 ;;
    scripts/lib/golden-corpus-keep-marker.sh) return 0 ;;
    *) return 1 ;;
  esac
}

# Pre-fetch the full diff once so comment-only and marker-added-lines checks
# are O(1) lookups against this cache instead of one git invocation per file.
# Falls back to the two-dot form on failure -- the three-dot (merge-base)
# form fails outright ("no merge base") when the fetched base tip and the
# local commit graph share no common ancestor object, which is the normal
# shape of a shallow CI checkout (eshu-hq/eshu#5542 follow-up). Mirrors the
# same fallback already used by changed_files above and
# is_runtime_config_by_content below. Empty if both forms are unavailable.
if _perf_diff_cache="$(git -C "$repo_root" diff --unified=0 "$base"...HEAD 2>/dev/null)"; then
  :
else
  _perf_diff_cache="$(git -C "$repo_root" diff --unified=0 "$base" HEAD 2>/dev/null || true)"
fi

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
      "+++ /dev/null")
        # Deleted-file new-path header (git writes "+++ /dev/null", no b/
        # prefix, so the "+++ b/"* arm above does not catch it). Reset the
        # current file: otherwise this header matches the "+"* arm below and,
        # together with the deleted file's removed lines (now matched by "-"*),
        # would be attributed to the PREVIOUS file's map entry and wrongly flip
        # a comment-only prior change to a code change.
        _perf_cur=""
        ;;
      "--- a/"*|"--- /dev/null")
        # Old-path diff header (--- a/foo, or --- /dev/null for a new file).
        # It starts with "-", so it must be excluded before the removed-line
        # arm below (which now matches "-"*), or it would be misread as removed
        # content and wrongly flip the current file to a code change.
        continue
        ;;
      "+"*|"-"*)
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
  # Exclude fixture/vendor/generated .md files before the whitelist below
  # runs. This must come first because bash `case` has no negation: `*`
  # inside a `case` pattern matches `/` too (unlike `compgen -G`'s glob
  # engine, which an earlier review used to verify this same ladder and
  # which does NOT cross `/` -- that mismatch is exactly how the bug below
  # slipped past review). That means a single `go/*.md` pattern already
  # matches every path under go/** at any depth, INCLUDING
  # go/cmd/audit-preflight/testdata/*.md -- four checked-in test fixtures
  # that are not documentation and must never satisfy the evidence gate
  # (eshu-hq/eshu#5542 follow-up). testdata/ is the confirmed, currently
  # present offender; vendor/ and generated/ are excluded defensively for
  # the same "not hand-authored documentation" reason even though no .md
  # files live there today, so the gate does not quietly reopen the same
  # hole the moment one appears. Matches both a leading path segment
  # (e.g. testdata/foo.md under the repo-root testdata/ tree) and a nested
  # one (e.g. go/cmd/audit-preflight/testdata/foo.md).
  case "$path" in
    testdata/*|*/testdata/*|vendor/*|*/vendor/*|generated/*|*/generated/*) return 1 ;;
  esac
  case "$path" in
    docs/public/adrs/*.md) return 0 ;;
    docs/public/reference/*.md) return 0 ;;
    docs/public/reference/**/*.md) return 0 ;;
    docs/internal/evidence/*.md) return 0 ;;
    docs/internal/evidence/**/*.md) return 0 ;;
    docs/internal/design/*.md) return 0 ;;
    docs/internal/design/**/*.md) return 0 ;;
    # Any .md directly under a go/** package directory is a recognized
    # evidence location, not just README.md/AGENTS.md/evidence-*.md. The
    # repo's real, actively-used convention has topic-named package docs
    # carrying genuine markers well beyond those three filenames (e.g.
    # go/internal/query/read-models.md, go/internal/storage/postgres/
    # gotchas-and-invariants.md, go/internal/reducer/shared-projection.md,
    # and the #5786 reducer README split's sibling docs). A narrower
    # whitelist recognized only 584 of 679 real .md files repo-wide that
    # carry a genuine marker and false-blocked legitimate PRs whose only
    # evidence lived in one of the other 95, including real merged commit
    # 7be40a0842 (#5747) which recorded evidence in
    # go/internal/query/read-models.md (eshu-hq/eshu#5542 follow-up).
    # Subsumes the earlier go/*/README.md, go/*/AGENTS.md,
    # go/*/evidence-*.md, and go/internal/reducer/*.md patterns, and the
    # previous go/*/*.md|go/*/*/*.md|... "depth ladder": under `case`
    # semantics `*` already crosses `/`, so `go/*.md` alone matches every
    # depth and the extra ladder rungs were unreachable dead weight
    # (eshu-hq/eshu#5542 follow-up).
    go/*.md) return 0 ;;
    # sdk/go/ is a sibling Go-module tree the go/*.md pattern above does not
    # reach (e.g. sdk/go/collector/README.md, sdk/go/factschema/README.md),
    # same gap class as go/** (eshu-hq/eshu#5542 follow-up). Collapsed for
    # the same dead-weight-ladder reason as go/*.md above.
    sdk/go/*.md) return 0 ;;
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
      continue
    fi

    # These shell helpers own cross-worktree exclusion and the one Docker
    # liveness query used to reclaim retained stacks. Changes can alter runner
    # wait/refusal behavior or add external calls even though no Go/runtime
    # configuration file changed.
    if is_live_gate_coordination_file "$file"; then
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

# Marker presence is decided from the PR's own ADDED lines, not whole-file
# content. Whole-file search lets a PR inherit a passing gate from an
# unrelated marker left behind by an earlier PR in a file it merely touches
# (eshu-hq/eshu#5542): the marker sits in the file as unchanged context, not
# as something this diff contributed, so it must not satisfy the gate.
#
# Filters the already-fetched _perf_diff_cache instead of spawning a fresh
# `git diff` per evidence file -- the cache above exists precisely so
# per-file checks are O(1) lookups, not one git invocation each. Uses plain
# string comparisons (not regex) so an evidence path containing regex
# metacharacters (e.g. a literal `.` in a filename) still compares exactly.
added_lines_for_evidence_file() {
  local rel="$1"
  [ -n "${_perf_diff_cache}" ] || return 0
  printf '%s\n' "${_perf_diff_cache}" | awk -v target="${rel}" '
    substr($0, 1, 6) == "+++ b/" {
      # No /dev/null guard needed here: "+++ b/" is exactly six characters, so
      # substr($0, 7) is always the path *after* b/ -- for "+++ b/dev/null" it
      # is "dev/null", never "/dev/null" or "b/dev/null". A real deleted-file
      # header is "+++ /dev/null", which does not match this rule at all and is
      # handled by the next one. The shell loop in _perf_code_change_map keeps
      # an equivalent check for its own reasons; do not copy it back here.
      cur = substr($0, 7)
      next
    }
    $0 == "+++ /dev/null" { cur = ""; next }
    substr($0, 1, 6) == "--- a/" { next }
    $0 == "--- /dev/null" { next }
    substr($0, 1, 1) == "+" {
      if (cur == target) print substr($0, 2)
      next
    }
  '
}

has_performance_evidence=1
has_observability_evidence=1
if [ "${#evidence_files[@]}" -gt 0 ]; then
  for evidence_file in "${evidence_files[@]}"; do
    evidence_rel="${evidence_file#"$repo_root"/}"
    evidence_added="$(added_lines_for_evidence_file "$evidence_rel")"
    [ -z "$evidence_added" ] && continue
    # Tolerates an optional single parenthetical/bracketed qualifier between
    # the marker phrase and the colon (e.g. "No-Regression Evidence (#5369):"),
    # an established, already-merged convention on main (docs/public/
    # reference/cypher-performance.md, go/internal/ask/engine/README.md, and
    # 36 other files -- 116 such markers repo-wide) that the original
    # phrase-then-colon-only regex made invisible to the gate. The colon
    # remains mandatory either way, so a bare mention of the phrase with no
    # colon at all -- "No-Regression Evidence (as discussed above) helps
    # operators..." -- still does not match (eshu-hq/eshu#5542 follow-up).
    # Match against a file, never a pipe. `printf ... | rg -q` is a race under
    # `set -o pipefail`: rg exits the instant it matches, closing the read end
    # while printf is still writing, so printf takes SIGPIPE (141). pipefail
    # then reports 141 for the pipeline and discards rg's own 0, and the `if`
    # reads a marker that IS present as missing -- failing the gate on a PR
    # whose evidence is right there in the diff. Reproduced deterministically
    # on bash 3.2.57, 5.2.21, and 5.3.15, so CI runners are affected too; it
    # only shows when the marker is early and the block exceeds a pipe buffer,
    # which is why the small-evidence cases never caught it. Do not reintroduce
    # a pipe here, with or without -q; see
    # scripts/test-verify-performance-evidence-large-marker.sh.
    evidence_added_path="$(mktemp "${TMPDIR:-/tmp}/eshu-performance-evidence-added.XXXXXX")"
    printf '%s\n' "$evidence_added" >"$evidence_added_path"
    if rg -q -e '(^|[[:space:]])(Performance Evidence|Benchmark Evidence|No-Regression Evidence)([[:space:]]*(\([^()]*\)|\[[^\[\]]*\]))?[[:space:]]*:' "$evidence_added_path"; then
      has_performance_evidence=0
    fi
    if rg -q -e '(^|[[:space:]])(Observability Evidence|No-Observability-Change)([[:space:]]*(\([^()]*\)|\[[^\[\]]*\]))?[[:space:]]*:' "$evidence_added_path"; then
      has_observability_evidence=0
    fi
    rm -f "$evidence_added_path"
  done
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
