#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-performance-evidence.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Test"
  mkdir -p "${dir}/docs/docs/reference" "${dir}/go/internal/storage/cypher"
  printf '# Local Performance\n' >"${dir}/docs/docs/reference/local-performance-envelope.md"
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
}

expect_fail() {
  local dir="$1"
  if run_verifier "${dir}"; then
    printf 'expected verifier to fail in %s\n' "${dir}" >&2
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
  >>"${evidence_repo}/docs/docs/reference/local-performance-envelope.md"
git -C "${evidence_repo}" add .
git -C "${evidence_repo}" commit -q -m 'hot change with evidence'
expect_pass "${evidence_repo}"

missing_observability_repo="$(init_repo missing-observability)"
printf 'package cypher\nconst query = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n' \
  >"${missing_observability_repo}/go/internal/storage/cypher/writer.go"
printf '\n## Current Evidence\n\nPerformance Evidence: focused writer benchmark stayed flat.\n' \
  >>"${missing_observability_repo}/docs/docs/reference/local-performance-envelope.md"
git -C "${missing_observability_repo}" add .
git -C "${missing_observability_repo}" commit -q -m 'hot change without observability evidence'
expect_fail "${missing_observability_repo}"

printf 'verify-performance-evidence tests passed\n'
