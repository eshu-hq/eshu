#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-performance-evidence.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Test"
  mkdir -p "${dir}/docs/public/reference" "${dir}/go/internal/storage/cypher"
  printf '# Local Performance\n' >"${dir}/docs/public/reference/local-performance-envelope.md"
  printf 'package cypher\n' >"${dir}/go/internal/storage/cypher/doc.go"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m initial
  printf '%s\n' "${dir}"
}

run_verifier() {
  local dir="$1"
  ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${dir}" \
    ESHU_PERFORMANCE_EVIDENCE_BASE=HEAD~1 \
    "${verifier}" >/tmp/eshu-perf-gate.out 2>/tmp/eshu-perf-gate.err
}

expect_pass() {
  local dir="$1"
  if ! run_verifier "${dir}"; then
    printf 'expected verifier to pass in %s\n' "${dir}" >&2
    sed -n '1,120p' /tmp/eshu-perf-gate.err >&2
    exit 1
  fi
  if rg -q 'No such file or directory' /tmp/eshu-perf-gate.err; then
    printf 'expected verifier stderr to stay clean in %s\n' "${dir}" >&2
    sed -n '1,120p' /tmp/eshu-perf-gate.err >&2
    exit 1
  fi
}

expect_fail() {
  local dir="$1"
  if run_verifier "${dir}"; then
    printf 'expected verifier to fail in %s\n' "${dir}" >&2
    sed -n '1,120p' /tmp/eshu-perf-gate.out >&2
    exit 1
  fi
}

# Build a hard-timeout wrapper so a hung verifier fails the suite fast instead
# of blocking forever. Prefer coreutils timeout/gtimeout; fall back to a
# portable perl alarm wrapper (perl ships on macOS and Linux). timeout_cmd stays
# empty only when none of the three exist, in which case expect_completes_fail
# SKIPS the guard with a message rather than running unguarded — an unguarded
# run would hang forever on exactly the bash where the here-string bug bites.
timeout_secs=30
timeout_cmd=()
if command -v timeout >/dev/null 2>&1; then
  timeout_cmd=(timeout -s KILL "${timeout_secs}")
elif command -v gtimeout >/dev/null 2>&1; then
  timeout_cmd=(gtimeout -s KILL "${timeout_secs}")
elif command -v perl >/dev/null 2>&1; then
  # SIGALRM fires after the timeout and default-terminates the exec'd verifier
  # (the alarm timer survives exec); a hang then exits 128+14=142.
  timeout_cmd=(perl -e 'alarm shift; exec @ARGV or exit 127' "${timeout_secs}")
fi

# expect_completes_fail asserts the verifier both terminates under a hard
# timeout and fails on its own (hot change needs evidence). It guards the
# diff-processing loop against reintroducing a `<<<` here-string, which hangs
# bash 5.3.x on a large diff (see the diff-loop comment in
# verify-performance-evidence.sh). Exit-code map: 124 (timeout SIGTERM) or any
# signal kill >=128 (137 SIGKILL, 142 SIGALRM) means the verifier was still
# running at the deadline — a hang; 125/126/127 means the timeout wrapper itself
# could not run the command; 0 means the verifier wrongly passed; 1..123 is the
# verifier's own expected non-zero exit.
expect_completes_fail() {
  local dir="$1"
  if [ "${#timeout_cmd[@]}" -eq 0 ]; then
    printf 'SKIP: no timeout/gtimeout/perl available to guard %s against a hang\n' "${dir}" >&2
    return
  fi
  local rc=0
  "${timeout_cmd[@]}" env \
    ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${dir}" \
    ESHU_PERFORMANCE_EVIDENCE_BASE=HEAD~1 \
    "${verifier}" >/tmp/eshu-perf-gate.out 2>/tmp/eshu-perf-gate.err || rc=$?
  if [ "${rc}" -eq 124 ] || [ "${rc}" -ge 128 ]; then
    printf 'verifier did not finish within %ss in %s (rc=%s) — diff-loop here-string hang\n' \
      "${timeout_secs}" "${dir}" "${rc}" >&2
    sed -n '1,40p' /tmp/eshu-perf-gate.err >&2
    exit 1
  fi
  if [ "${rc}" -ge 125 ] && [ "${rc}" -le 127 ]; then
    printf 'timeout wrapper could not run the verifier in %s (rc=%s)\n' "${dir}" "${rc}" >&2
    sed -n '1,40p' /tmp/eshu-perf-gate.err >&2
    exit 1
  fi
  if [ "${rc}" -eq 0 ]; then
    printf 'expected verifier to fail (hot change needs evidence) in %s\n' "${dir}" >&2
    sed -n '1,120p' /tmp/eshu-perf-gate.out >&2
    exit 1
  fi
}

plain_repo="$(init_repo plain)"
printf 'package docs\n' >"${plain_repo}/go/internal/storage/cypher/notes_test.go"
git -C "${plain_repo}" add .
git -C "${plain_repo}" commit -q -m 'test-only change'
expect_pass "${plain_repo}"

hot_repo="$(init_repo hot)"
printf 'package cypher\nconst query = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n' \
  >"${hot_repo}/go/internal/storage/cypher/writer.go"
git -C "${hot_repo}" add .
git -C "${hot_repo}" commit -q -m 'hot cypher change'
expect_fail "${hot_repo}"

new_collector_repo="$(init_repo new-collector)"
mkdir -p "${new_collector_repo}/go/internal/collector/terraformcloud"
printf 'package terraformcloud\nconst query = "MATCH (r:Repository {id: $id}) RETURN r"\n' \
  >"${new_collector_repo}/go/internal/collector/terraformcloud/project.go"
git -C "${new_collector_repo}" add .
git -C "${new_collector_repo}" commit -q -m 'new collector query'
expect_fail "${new_collector_repo}"

new_concurrency_repo="$(init_repo new-concurrency)"
mkdir -p "${new_concurrency_repo}/go/internal/collector/awscloud/services/s3"
cat >"${new_concurrency_repo}/go/internal/collector/awscloud/services/s3/runner.go" <<'GO'
package s3

type Runner struct {
	MaxConcurrentClaims int
}

func (r Runner) ClaimBatch() int {
	return r.MaxConcurrentClaims
}
GO
git -C "${new_concurrency_repo}" add .
git -C "${new_concurrency_repo}" commit -q -m 'new collector concurrency'
expect_fail "${new_concurrency_repo}"

evidence_repo="$(init_repo evidence)"
printf 'package cypher\nconst query = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n' \
  >"${evidence_repo}/go/internal/storage/cypher/writer.go"
printf '\n## Current Evidence\n\nPerformance Evidence: focused writer benchmark stayed flat.\n\nObservability Evidence: existing writer metrics covered the changed path.\n' \
  >>"${evidence_repo}/docs/public/reference/local-performance-envelope.md"
git -C "${evidence_repo}" add .
git -C "${evidence_repo}" commit -q -m 'hot change with evidence'
expect_pass "${evidence_repo}"

package_evidence_repo="$(init_repo package-evidence)"
mkdir -p "${package_evidence_repo}/go/internal/query"
printf 'package query\nconst query = "MATCH (r:Repository) RETURN r"\n' \
  >"${package_evidence_repo}/go/internal/query/repository.go"
cat >"${package_evidence_repo}/go/internal/query/evidence-notes.md" <<'MD'
# Query Evidence Notes

No-Regression Evidence: focused query tests prove the bounded read shape.

No-Observability-Change: existing query spans cover the changed path.
MD
git -C "${package_evidence_repo}" add .
git -C "${package_evidence_repo}" commit -q -m 'hot change with package evidence note'
expect_pass "${package_evidence_repo}"

deleted_evidence_repo="$(init_repo deleted-evidence)"
mkdir -p "${deleted_evidence_repo}/go/internal/collector/oldsource"
printf 'package oldsource\nconst query = "MATCH (n) RETURN n"\n' \
  >"${deleted_evidence_repo}/go/internal/collector/oldsource/source.go"
cat >"${deleted_evidence_repo}/go/internal/collector/oldsource/README.md" <<'MD'
# Old Source

Performance Evidence: baseline fixture only.

No-Observability-Change: baseline fixture only.
MD
git -C "${deleted_evidence_repo}" add .
git -C "${deleted_evidence_repo}" commit -q -m 'old collector baseline'
rm -rf "${deleted_evidence_repo}/go/internal/collector/oldsource"
printf '\n## Current Evidence\n\nPerformance Evidence: removed facade source; no runtime work remains.\n\nNo-Observability-Change: no runtime path remains.\n' \
  >>"${deleted_evidence_repo}/docs/public/reference/local-performance-envelope.md"
git -C "${deleted_evidence_repo}" add -A
git -C "${deleted_evidence_repo}" commit -q -m 'delete collector with evidence'
expect_pass "${deleted_evidence_repo}"

missing_observability_repo="$(init_repo missing-observability)"
printf 'package cypher\nconst query = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n' \
  >"${missing_observability_repo}/go/internal/storage/cypher/writer.go"
printf '\n## Current Evidence\n\nPerformance Evidence: focused writer benchmark stayed flat.\n' \
  >>"${missing_observability_repo}/docs/public/reference/local-performance-envelope.md"
git -C "${missing_observability_repo}" add .
git -C "${missing_observability_repo}" commit -q -m 'hot change without observability evidence'
expect_fail "${missing_observability_repo}"

compose_runtime_repo="$(init_repo compose-runtime)"
printf 'services:\n  nornicdb:\n    environment:\n      NORNICDB_EMBEDDING_ENABLED: "false"\n' \
  >"${compose_runtime_repo}/docker-compose.yaml"
git -C "${compose_runtime_repo}" add .
git -C "${compose_runtime_repo}" commit -q -m 'runtime compose change'
expect_fail "${compose_runtime_repo}"

compose_runtime_removal_repo="$(init_repo compose-runtime-removal)"
printf 'services:\n  nornicdb:\n    environment:\n      NORNICDB_EMBEDDING_ENABLED: "false"\n' \
  >"${compose_runtime_removal_repo}/docker-compose.yaml"
git -C "${compose_runtime_removal_repo}" add .
git -C "${compose_runtime_removal_repo}" commit -q -m 'add runtime compose baseline'
printf 'services:\n  nornicdb:\n    environment: {}\n' \
  >"${compose_runtime_removal_repo}/docker-compose.yaml"
git -C "${compose_runtime_removal_repo}" add .
git -C "${compose_runtime_removal_repo}" commit -q -m 'remove runtime compose knob'
expect_fail "${compose_runtime_removal_repo}"

# Comment-only change to an EXISTING hot-path Go file (e.g. SPDX header
# rollout): the file already exists with hot-path content, and the diff
# only adds comment lines at the top. The gate must NOT trip.
spdx_repo="$(init_repo spdx-rollout)"
{
  printf 'package cypher\n'
  printf '\n'
  printf 'const writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n'
} >"${spdx_repo}/go/internal/storage/cypher/writer.go"
git -C "${spdx_repo}" add .
git -C "${spdx_repo}" commit -q -m 'baseline hot-path writer'
{
  printf '// SPDX-License-Identifier: MIT\n'
  printf '// Copyright (c) 2025-2026 eshu-hq\n'
  printf '\n'
  printf 'package cypher\n'
  printf '\n'
  printf 'const writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n'
} >"${spdx_repo}/go/internal/storage/cypher/writer.go"
git -C "${spdx_repo}" add .
git -C "${spdx_repo}" commit -q -m 'add SPDX header (comment-only)'
expect_pass "${spdx_repo}"

# Mixed change to an existing hot-path Go file: comment header AND a real
# code edit in the same commit. Gate must still trip because the code
# change is real.
mixed_repo="$(init_repo mixed-change)"
{
  printf 'package cypher\n'
  printf '\n'
  printf 'const writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n'
} >"${mixed_repo}/go/internal/storage/cypher/writer.go"
git -C "${mixed_repo}" add .
git -C "${mixed_repo}" commit -q -m 'baseline hot-path writer'
{
  printf '// SPDX-License-Identifier: MIT\n'
  printf '// Copyright (c) 2025-2026 eshu-hq\n'
  printf '\n'
  printf 'package cypher\n'
  printf '\n'
  printf 'const writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid, kind: row.kind})"\n'
} >"${mixed_repo}/go/internal/storage/cypher/writer.go"
git -C "${mixed_repo}" add .
git -C "${mixed_repo}" commit -q -m 'add SPDX header and tweak hot-path query'
expect_fail "${mixed_repo}"

# Whitespace-only change to a hot-path Go file (no comment, no code): gate
# must NOT trip because the diff is purely indentation/blank lines.
whitespace_repo="$(init_repo whitespace-only)"
{
  printf 'package cypher\n'
  printf '\n'
  printf 'const writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n'
} >"${whitespace_repo}/go/internal/storage/cypher/writer.go"
git -C "${whitespace_repo}" add .
git -C "${whitespace_repo}" commit -q -m 'baseline hot-path file'
{
  printf 'package cypher\n'
  printf '\n'
  printf '\n'
  printf 'const writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n'
} >"${whitespace_repo}/go/internal/storage/cypher/writer.go"
git -C "${whitespace_repo}" add .
git -C "${whitespace_repo}" commit -q -m 'add blank line (whitespace-only)'
expect_pass "${whitespace_repo}"

# Regression: a large hot-path diff must not hang the diff-processing loop.
# bash 5.3.x hangs indefinitely when a `<<<` here-string feeds the while-read
# loop once the cached diff crosses a byte threshold; the loop reads its input
# via process substitution to stay safe. ~1200 real code lines push the cached
# diff well past the threshold that hangs the here-string form.
large_diff_repo="$(init_repo large-diff)"
{
  printf 'package cypher\n'
  printf 'const writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n'
  for i in $(seq 1 1200); do
    printf 'const pad%d = "row %d value padding a wide diff payload"\n' "${i}" "${i}"
  done
} >"${large_diff_repo}/go/internal/storage/cypher/writer.go"
git -C "${large_diff_repo}" add .
git -C "${large_diff_repo}" commit -q -m 'large hot-path diff'
expect_completes_fail "${large_diff_repo}"

# Regression: the final line of the cached diff must still be processed. The
# cached diff is captured via command substitution, which strips the trailing
# newline, so the loop must restore it (printf '%s\n', not '%s'). Here the only
# real code change is the last diff line (a new const appended after two
# comment-only additions); if the loop drops that line the file is misread as
# comment-only and the gate wrongly passes.
last_line_repo="$(init_repo last-line)"
{
  printf 'package cypher\n'
  printf 'const writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n'
} >"${last_line_repo}/go/internal/storage/cypher/writer.go"
git -C "${last_line_repo}" add .
git -C "${last_line_repo}" commit -q -m 'baseline hot-path writer'
{
  printf '// header comment one\n'
  printf '// header comment two\n'
  printf 'package cypher\n'
  printf 'const writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n'
  printf 'const extra = "another real hot-path line"\n'
} >"${last_line_repo}/go/internal/storage/cypher/writer.go"
git -C "${last_line_repo}" add .
git -C "${last_line_repo}" commit -q -m 'append decisive code on last diff line'
expect_fail "${last_line_repo}"

printf 'verify-performance-evidence tests passed\n'
