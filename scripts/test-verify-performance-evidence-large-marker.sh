#!/usr/bin/env bash
set -euo pipefail

# Companion to scripts/test-verify-performance-evidence.sh, split out to keep
# that file under the repo's 500-line cap. Invoked from its tail.
#
# Regression coverage: a marker that is genuinely present in the diff's added
# lines must be found even when the evidence block is large and the marker is
# not on the last line.
#
# The marker checks used to pipe the added lines into `rg -q`. With `set -o
# pipefail`, that is a race: `rg -q` exits as soon as it matches, closing the
# read end while `printf` is still writing, so `printf` takes SIGPIPE and exits
# 141. pipefail then reports 141 for the whole pipeline -- rg's own 0 is
# discarded -- and the `if` reads a found marker as missing. The gate then
# fails a PR whose evidence is right there in the diff.
#
# It is not a bash-version quirk: reproduced deterministically on bash 3.2.57,
# 5.2.21, and 5.3.15, so CI runners are affected too. It needs the marker to be
# early and the block to be bigger than a pipe buffer, which is why every
# pre-existing small-evidence case in the suite passes and hid this.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-performance-evidence.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}/docs/public/reference" "${dir}/go/internal/storage/cypher"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Test"
  printf '# Local Performance\n' >"${dir}/docs/public/reference/local-performance-envelope.md"
  printf 'package cypher\n' >"${dir}/go/internal/storage/cypher/doc.go"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m initial
  printf '%s\n' "${dir}"
}

expect_pass() {
  local dir="$1"
  local label="$2"
  if ! ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${dir}" \
    ESHU_PERFORMANCE_EVIDENCE_BASE=HEAD~1 \
    "${verifier}" >/tmp/eshu-perf-large-marker.out 2>/tmp/eshu-perf-large-marker.err; then
    printf 'expected verifier to pass for %s in %s\n' "${label}" "${dir}" >&2
    printf -- '--- stdout ---\n' >&2
    sed -n '1,40p' /tmp/eshu-perf-large-marker.out >&2
    printf -- '--- stderr ---\n' >&2
    sed -n '1,40p' /tmp/eshu-perf-large-marker.err >&2
    exit 1
  fi
}

# One hot-path change, and one evidence file whose added lines carry both
# markers near the TOP followed by enough prose to exceed a pipe buffer.
large_marker_repo="$(init_repo large-marker)"
printf 'package cypher\nconst query = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n' \
  >"${large_marker_repo}/go/internal/storage/cypher/writer.go"
{
  printf '\n## Evidence\n\n'
  printf 'Performance Evidence: focused writer benchmark stayed flat.\n\n'
  printf 'Observability Evidence: existing writer metrics cover the changed path.\n\n'
  # Trailing prose. Must be large enough that printf is still writing when a
  # matching `rg -q` exits; 4000 lines clears any platform pipe buffer.
  for _ in $(seq 1 4000); do
    printf 'Context paragraph describing the measured run in enough detail that the evidence block is comfortably larger than one pipe buffer.\n'
  done
} >>"${large_marker_repo}/docs/public/reference/local-performance-envelope.md"
git -C "${large_marker_repo}" add .
git -C "${large_marker_repo}" commit -q -m 'hot change with a large evidence block'
expect_pass "${large_marker_repo}" 'markers early in a large evidence block'

printf 'verify-performance-evidence large-marker test passed\n'
