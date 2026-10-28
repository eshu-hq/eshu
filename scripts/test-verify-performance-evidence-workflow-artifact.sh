#!/usr/bin/env bash
set -euo pipefail

# Companion to scripts/test-verify-performance-evidence.sh, split out to keep
# that file under the repo's 500-line cap. Invoked from its tail.
#
# Workflow-artifact evidence (#9315): a hot-path PR can point at the CI run
# that recorded the evidence instead of committing the record. A shaped
# `Performance Evidence (workflow-artifact: <run-id>/<name>):` (or Benchmark /
# No-Regression) marker plus the matching observability marker in the PR's own
# added lines satisfies the gate; the committed-file path keeps working
# unchanged. Malformed pointers (empty or unnamespaced refs) and pointers in
# fixture trees must not satisfy it.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-performance-evidence.sh"

tmp_root="$(mktemp -d)"
# #8673: keep fixture repos out of the host git-ai daemon's Trace2 tracking.
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
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
  local out="${tmp_root}/verifier.out"
  local err="${tmp_root}/verifier.err"
  if ! ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${dir}" \
    ESHU_PERFORMANCE_EVIDENCE_BASE=HEAD~1 \
    "${verifier}" >"${out}" 2>"${err}"; then
    printf 'expected verifier to pass for %s in %s\n' "${label}" "${dir}" >&2
    printf -- '--- stdout ---\n' >&2
    sed -n '1,40p' "${out}" >&2
    printf -- '--- stderr ---\n' >&2
    sed -n '1,40p' "${err}" >&2
    exit 1
  fi
}

expect_fail() {
  local dir="$1"
  local label="$2"
  local out="${tmp_root}/verifier.out"
  local err="${tmp_root}/verifier.err"
  if ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${dir}" \
    ESHU_PERFORMANCE_EVIDENCE_BASE=HEAD~1 \
    "${verifier}" >"${out}" 2>"${err}"; then
    printf 'expected verifier to fail for %s in %s\n' "${label}" "${dir}" >&2
    exit 1
  fi
}

hot_change() {
  local dir="$1"
  printf 'package cypher\nconst query = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n' \
    >"${dir}/go/internal/storage/cypher/writer.go"
}

# Artifact pointers in the changed hot file's own added lines satisfy both
# halves of the gate with no committed evidence file.
artifact_inline_repo="$(init_repo artifact-inline)"
hot_change "${artifact_inline_repo}"
cat >>"${artifact_inline_repo}/go/internal/storage/cypher/writer.go" <<'GO'
// Performance Evidence (workflow-artifact: 39421484741/freshness-scale): canary bound held on the scheduled run.
// Observability Evidence (workflow-artifact: 39421484741/freshness-scale): writerishop metrics scraped on that run.
GO
git -C "${artifact_inline_repo}" add .
git -C "${artifact_inline_repo}" commit -q -m 'hot change with inline artifact pointers'
expect_pass "${artifact_inline_repo}" 'inline run-id artifact pointers'

# A full Actions run URL is a shaped ref too, recorded in a package note.
artifact_url_repo="$(init_repo artifact-url)"
hot_change "${artifact_url_repo}"
mkdir -p "${artifact_url_repo}/go/internal/query"
cat >"${artifact_url_repo}/go/internal/query/evidence-notes.md" <<'MD'
# Query Evidence Notes

No-Regression Evidence (workflow-artifact: https://github.com/eshu-hq/eshu/actions/runs/39421484741): bounded read shape held.

Observability Evidence (workflow-artifact: https://github.com/eshu-hq/eshu/actions/runs/39421484741): query spans present on that run.
MD
git -C "${artifact_url_repo}" add .
git -C "${artifact_url_repo}" commit -q -m 'hot change with run-URL artifact pointers'
expect_pass "${artifact_url_repo}" 'run-URL artifact pointers'

# An empty artifact ref points nowhere and must not satisfy the gate.
artifact_empty_repo="$(init_repo artifact-empty)"
hot_change "${artifact_empty_repo}"
cat >>"${artifact_empty_repo}/go/internal/storage/cypher/writer.go" <<'GO'
// Performance Evidence (workflow-artifact:): pointer with no ref.
// Observability Evidence (workflow-artifact:): pointer with no ref.
GO
git -C "${artifact_empty_repo}" add .
git -C "${artifact_empty_repo}" commit -q -m 'hot change with empty artifact refs'
expect_fail "${artifact_empty_repo}" 'empty artifact refs'

# A ref without a namespace (no run-id/artifact-name shape, no URL) is not
# a resolvable pointer and must not satisfy the gate.
artifact_bare_repo="$(init_repo artifact-bare)"
hot_change "${artifact_bare_repo}"
cat >>"${artifact_bare_repo}/go/internal/storage/cypher/writer.go" <<'GO'
// Performance Evidence (workflow-artifact: somerun): bare word is not a pointer.
// Observability Evidence (workflow-artifact: somerun): bare word is not a pointer.
GO
git -C "${artifact_bare_repo}" add .
git -C "${artifact_bare_repo}" commit -q -m 'hot change with bare artifact refs'
expect_fail "${artifact_bare_repo}" 'bare artifact refs'

# A slash-bearing non-pointer names no run and must not satisfy the gate:
# the pointer must carry a numeric run id or a run URL (eshu-hq/eshu#9368).
artifact_slash_repo="$(init_repo artifact-slash)"
hot_change "${artifact_slash_repo}"
cat >>"${artifact_slash_repo}/go/internal/storage/cypher/writer.go" <<'GO'
// Performance Evidence (workflow-artifact: see/above): slash-bearing prose is not a pointer.
// Observability Evidence (workflow-artifact: see/above): slash-bearing prose is not a pointer.
GO
git -C "${artifact_slash_repo}" add .
git -C "${artifact_slash_repo}" commit -q -m 'hot change with slash-bearing non-pointer refs'
expect_fail "${artifact_slash_repo}" 'slash-bearing non-pointer refs'

# A shaped pointer inside a fixture tree must not satisfy the gate: testdata
# carries fixtures, never evidence (eshu-hq/eshu#5542 follow-up).
artifact_fixture_repo="$(init_repo artifact-fixture)"
hot_change "${artifact_fixture_repo}"
mkdir -p "${artifact_fixture_repo}/go/internal/storage/cypher/testdata"
cat >"${artifact_fixture_repo}/go/internal/storage/cypher/testdata/proof.md" <<'MD'
# Fixture Proof

Performance Evidence (workflow-artifact: 39421484741/freshness-scale): fixture text is not evidence.

Observability Evidence (workflow-artifact: 39421484741/freshness-scale): fixture text is not evidence.
MD
git -C "${artifact_fixture_repo}" add .
git -C "${artifact_fixture_repo}" commit -q -m 'hot change with fixture-tree pointers'
expect_fail "${artifact_fixture_repo}" 'fixture-tree pointers'

printf 'verify-performance-evidence workflow-artifact test passed\n'
