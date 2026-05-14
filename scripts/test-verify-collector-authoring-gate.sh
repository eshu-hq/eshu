#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-collector-authoring-gate.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Test"
  mkdir -p "${dir}/docs/docs/reference/telemetry" "${dir}/go/internal/collector/base"
  printf '# Collector Authoring\n' >"${dir}/docs/docs/guides-placeholder.md"
  printf '# Telemetry\n' >"${dir}/docs/docs/reference/telemetry/index.md"
  printf 'package base\n' >"${dir}/go/internal/collector/base/doc.go"
  printf '# Base\n' >"${dir}/go/internal/collector/base/README.md"
  printf '# AGENTS\n' >"${dir}/go/internal/collector/base/AGENTS.md"
  printf 'package base\nfunc TestBase(t interface{}) {}\n' >"${dir}/go/internal/collector/base/source_test.go"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m initial
  printf '%s\n' "${dir}"
}

run_verifier() {
  local dir="$1"
  ESHU_COLLECTOR_AUTHORING_REPO_ROOT="${dir}" \
    ESHU_COLLECTOR_AUTHORING_BASE=HEAD~1 \
    "${verifier}" >/tmp/eshu-collector-authoring.out 2>/tmp/eshu-collector-authoring.err
}

expect_pass() {
  local dir="$1"
  if ! run_verifier "${dir}"; then
    printf 'expected verifier to pass in %s\n' "${dir}" >&2
    sed -n '1,140p' /tmp/eshu-collector-authoring.err >&2
    exit 1
  fi
}

expect_fail() {
  local dir="$1"
  if run_verifier "${dir}"; then
    printf 'expected verifier to fail in %s\n' "${dir}" >&2
    sed -n '1,140p' /tmp/eshu-collector-authoring.out >&2
    exit 1
  fi
}

plain_repo="$(init_repo plain)"
printf '# docs only\n' >"${plain_repo}/README.md"
git -C "${plain_repo}" add .
git -C "${plain_repo}" commit -q -m 'docs only'
expect_pass "${plain_repo}"

missing_docs_repo="$(init_repo missing-docs)"
mkdir -p "${missing_docs_repo}/go/internal/collector/confluence2"
printf 'package confluence2\n' >"${missing_docs_repo}/go/internal/collector/confluence2/source.go"
git -C "${missing_docs_repo}" add .
git -C "${missing_docs_repo}" commit -q -m 'collector without package docs'
expect_fail "${missing_docs_repo}"

missing_markers_repo="$(init_repo missing-markers)"
mkdir -p "${missing_markers_repo}/go/internal/collector/confluence2"
printf 'package confluence2\n' >"${missing_markers_repo}/go/internal/collector/confluence2/source.go"
printf 'package confluence2\n' >"${missing_markers_repo}/go/internal/collector/confluence2/doc.go"
printf '# Confluence2\n' >"${missing_markers_repo}/go/internal/collector/confluence2/README.md"
printf '# AGENTS\n' >"${missing_markers_repo}/go/internal/collector/confluence2/AGENTS.md"
printf 'package confluence2\nfunc TestSource(t interface{}) {}\n' >"${missing_markers_repo}/go/internal/collector/confluence2/source_test.go"
git -C "${missing_markers_repo}" add .
git -C "${missing_markers_repo}" commit -q -m 'collector without evidence markers'
expect_fail "${missing_markers_repo}"

complete_repo="$(init_repo complete)"
mkdir -p "${complete_repo}/go/internal/collector/confluence2" "${complete_repo}/go/internal/telemetry"
printf 'package confluence2\n' >"${complete_repo}/go/internal/collector/confluence2/source.go"
printf 'package confluence2\n' >"${complete_repo}/go/internal/collector/confluence2/doc.go"
cat >"${complete_repo}/go/internal/collector/confluence2/README.md" <<'MD'
# Confluence2

Collector Performance Evidence: smoke fixture scanned 25 pages, emitted 76 facts,
and completed under the documented local budget.

Collector Observability Evidence: source request, parse, fact-emission, and
failure metrics expose the bounded source stage without page IDs or titles.

Collector Deployment Evidence: no hosted runtime in this slice; ServiceMonitor
coverage is deferred until a charted command package lands.
MD
printf '# AGENTS\n' >"${complete_repo}/go/internal/collector/confluence2/AGENTS.md"
printf 'package confluence2\nfunc TestSource(t interface{}) {}\n' >"${complete_repo}/go/internal/collector/confluence2/source_test.go"
printf 'package telemetry\nconst MetricDimensionFailureClass = "failure_class"\n' >"${complete_repo}/go/internal/telemetry/contract.go"
git -C "${complete_repo}" add .
git -C "${complete_repo}" commit -q -m 'complete collector gate evidence'
expect_pass "${complete_repo}"

printf 'verify-collector-authoring-gate tests passed\n'
