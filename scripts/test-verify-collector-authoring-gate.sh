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
  mkdir -p "${dir}/docs/public/reference/telemetry" "${dir}/go/internal/collector/base"
  printf '# Collector Authoring\n' >"${dir}/docs/public/guides-placeholder.md"
  printf '# Telemetry\n' >"${dir}/docs/public/reference/telemetry/index.md"
  printf 'package base\n' >"${dir}/go/internal/collector/base/doc.go"
  printf '# Base\n' >"${dir}/go/internal/collector/base/README.md"
  printf '# Base Agent Rules\n' >"${dir}/go/internal/collector/base/AGENTS.md"
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
printf '# Confluence2 Agent Rules\n' >"${missing_markers_repo}/go/internal/collector/confluence2/AGENTS.md"
printf 'package confluence2\nfunc TestSource(t interface{}) {}\n' >"${missing_markers_repo}/go/internal/collector/confluence2/source_test.go"
git -C "${missing_markers_repo}" add .
git -C "${missing_markers_repo}" commit -q -m 'collector without evidence markers'
expect_fail "${missing_markers_repo}"

deleted_repo="$(init_repo deleted-package)"
mkdir -p "${deleted_repo}/go/internal/collector/oldsource"
printf 'package oldsource\n' >"${deleted_repo}/go/internal/collector/oldsource/source.go"
printf 'package oldsource\n' >"${deleted_repo}/go/internal/collector/oldsource/doc.go"
printf '# Old Source Agent Rules\n' >"${deleted_repo}/go/internal/collector/oldsource/AGENTS.md"
cat >"${deleted_repo}/go/internal/collector/oldsource/README.md" <<'MD'
# Old Source

Collector Performance Evidence: baseline fixture only.

Collector Observability Evidence: no runtime path.

Collector Deployment Evidence: no hosted runtime.

No-Observability-Change: baseline fixture only.
MD
printf 'package oldsource\nfunc TestSource(t interface{}) {}\n' >"${deleted_repo}/go/internal/collector/oldsource/source_test.go"
git -C "${deleted_repo}" add .
git -C "${deleted_repo}" commit -q -m 'collector before deletion'
rm -rf "${deleted_repo}/go/internal/collector/oldsource"
git -C "${deleted_repo}" add -A
git -C "${deleted_repo}" commit -q -m 'delete collector package'
expect_pass "${deleted_repo}"

complete_repo="$(init_repo complete)"
mkdir -p "${complete_repo}/go/internal/collector/confluence2" "${complete_repo}/go/internal/telemetry"
printf 'package confluence2\n' >"${complete_repo}/go/internal/collector/confluence2/source.go"
printf 'package confluence2\n' >"${complete_repo}/go/internal/collector/confluence2/doc.go"
printf '# Confluence2 Agent Rules\n' >"${complete_repo}/go/internal/collector/confluence2/AGENTS.md"
cat >"${complete_repo}/go/internal/collector/confluence2/README.md" <<'MD'
# Confluence2

Collector Performance Evidence: smoke fixture scanned 25 pages, emitted 76 facts,
and completed under the documented local budget.

Collector Observability Evidence: source request, parse, fact-emission, and
failure metrics expose the bounded source stage without page IDs or titles.

Collector Deployment Evidence: no hosted runtime in this slice; ServiceMonitor
coverage is deferred until a charted command package lands.
MD
printf 'package confluence2\nfunc TestSource(t interface{}) {}\n' >"${complete_repo}/go/internal/collector/confluence2/source_test.go"
printf 'package telemetry\nconst SpanJiraFetch = "jira.fetch"\n' >"${complete_repo}/go/internal/telemetry/contract_jira.go"
git -C "${complete_repo}" add .
git -C "${complete_repo}" commit -q -m 'complete collector gate evidence'
expect_pass "${complete_repo}"

# Regression: with neither ESHU_COLLECTOR_AUTHORING_BASE nor GITHUB_BASE_REF
# set -- the shape every test above bypasses by pinning HEAD~1 -- the base must
# be the merge base with origin/main. A HEAD~1 default scopes the gate to the
# last commit, so a collector added in an earlier commit of a multi-commit
# branch escapes whenever the tip commit is innocuous.
#
# Gives the fixture a real origin/main to resolve a merge base against: clone
# the repo at its initial commit into a bare origin, then branch away from it.
setup_branch_on_origin() {
  local dir="$1"
  local origin_dir="${dir}-origin"
  git -C "${dir}" branch -M main
  git clone -q --bare "${dir}" "${origin_dir}"
  git -C "${dir}" remote add origin "${origin_dir}"
  git -C "${dir}" fetch -q origin
  git -C "${dir}" checkout -q -b feature
}

run_verifier_local_base() {
  local dir="$1"
  env -u ESHU_COLLECTOR_AUTHORING_BASE -u GITHUB_BASE_REF \
    ESHU_COLLECTOR_AUTHORING_REPO_ROOT="${dir}" \
    "${verifier}" >/tmp/eshu-collector-authoring.out \
    2>/tmp/eshu-collector-authoring.err
}

merge_base_repo="$(init_repo merge-base)"
setup_branch_on_origin "${merge_base_repo}"
# Commit A: a collector with no package docs -- the gate must reject this.
mkdir -p "${merge_base_repo}/go/internal/collector/confluence2"
printf 'package confluence2\n' \
  >"${merge_base_repo}/go/internal/collector/confluence2/source.go"
git -C "${merge_base_repo}" add .
git -C "${merge_base_repo}" commit -q -m 'branch commit A: collector without package docs'
# Commit B: an innocuous tip commit that used to hide commit A from the gate.
printf '# readme\n' >"${merge_base_repo}/README.md"
git -C "${merge_base_repo}" add .
git -C "${merge_base_repo}" commit -q -m 'branch commit B: readme touch'
if run_verifier_local_base "${merge_base_repo}"; then
  printf 'expected the gate to FAIL: the branch adds an undocumented collector\n' >&2
  printf 'in an earlier commit and its tip commit is innocuous. A pass here\n' >&2
  printf 'means the base fell back to HEAD~1 and scoped to the last commit.\n' >&2
  sed -n '1,140p' /tmp/eshu-collector-authoring.out >&2
  exit 1
fi

# The widened window must not fire on a branch with no collector change at all.
merge_base_clean_repo="$(init_repo merge-base-clean)"
setup_branch_on_origin "${merge_base_clean_repo}"
printf '# docs only\n' >"${merge_base_clean_repo}/README.md"
git -C "${merge_base_clean_repo}" add .
git -C "${merge_base_clean_repo}" commit -q -m 'branch commit A: docs only'
printf '# docs only, again\n' >"${merge_base_clean_repo}/README.md"
git -C "${merge_base_clean_repo}" add .
git -C "${merge_base_clean_repo}" commit -q -m 'branch commit B: docs only'
if ! run_verifier_local_base "${merge_base_clean_repo}"; then
  printf 'expected a docs-only branch to PASS under the merge-base window\n' >&2
  sed -n '1,140p' /tmp/eshu-collector-authoring.err >&2
  exit 1
fi

printf 'verify-collector-authoring-gate tests passed\n'
