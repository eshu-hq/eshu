#!/usr/bin/env bash
set -euo pipefail

# Companion to scripts/test-verify-performance-evidence.sh, split out to keep
# that file under the repo's 500-line cap. Invoked from its tail.
#
# Regression coverage for eshu-hq/eshu#5542: a PR that touches a file which
# already carries a correctly-formatted marker left behind by an EARLIER,
# unrelated PR must not inherit a passing verdict from that untouched marker.
# Whole-file marker search (the bug) finds the old marker anywhere in the
# file and wrongly passes; the fix scopes the search to the diff's added
# lines, so the gate must fail unless THIS PR's own diff carries the marker.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-performance-evidence.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}/go/internal/storage/cypher"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Test"
  printf '%s\n' "${dir}"
}

run_verifier() {
  local dir="$1"
  ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${dir}" \
    ESHU_PERFORMANCE_EVIDENCE_BASE=HEAD~1 \
    "${verifier}" >/tmp/eshu-perf-gate-inherited.out 2>/tmp/eshu-perf-gate-inherited.err
}

expect_pass() {
  local dir="$1"
  if ! run_verifier "${dir}"; then
    printf 'expected verifier to pass in %s\n' "${dir}" >&2
    sed -n '1,120p' /tmp/eshu-perf-gate-inherited.err >&2
    exit 1
  fi
}

expect_fail() {
  local dir="$1"
  if run_verifier "${dir}"; then
    printf 'expected verifier to fail in %s\n' "${dir}" >&2
    sed -n '1,120p' /tmp/eshu-perf-gate-inherited.out >&2
    exit 1
  fi
}

write_baseline() {
  local dir="$1"
  printf 'package cypher\n\nconst readerQuery = "MATCH (r:Repository {id: $id}) RETURN r"\n' \
    >"${dir}/go/internal/storage/cypher/writer.go"
  cat >"${dir}/go/internal/storage/cypher/README.md" <<'MD'
# Cypher Storage

Overview of the cypher storage writer package.

## Prior Evidence (an earlier, unrelated PR)

Performance Evidence: baseline writer benchmark stayed flat.

No-Observability-Change: existing writer metrics already cover this path.
MD
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m 'baseline hot-path file + prior unrelated evidence marker'
}

# The old marker sits untouched as context; this PR's own edit to the same
# file is unrelated prose (a typo fix) with no marker text of its own. The
# gate must fail.
inherited_marker_repo="$(init_repo inherited-marker)"
write_baseline "${inherited_marker_repo}"
printf 'package cypher\n\nconst readerQuery = "MATCH (r:Repository {id: $id}) RETURN r"\nconst writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n' \
  >"${inherited_marker_repo}/go/internal/storage/cypher/writer.go"
cat >"${inherited_marker_repo}/go/internal/storage/cypher/README.md" <<'MD'
# Cypher Storage

Overview of the cypher storage writer package and its query surface.

## Prior Evidence (an earlier, unrelated PR)

Performance Evidence: baseline writer benchmark stayed flat.

No-Observability-Change: existing writer metrics already cover this path.
MD
git -C "${inherited_marker_repo}" add .
git -C "${inherited_marker_repo}" commit -q -m 'add writerQuery MERGE (hot change, no new evidence of its own)'
expect_fail "${inherited_marker_repo}"

# Companion case: the same inherited-marker setup, but this PR ALSO adds a
# genuine new marker of its own (added lines contain fresh marker text)
# alongside the untouched old one. The gate must pass because this PR's own
# diff now carries real evidence, proving the fix does not over-block a
# file that legitimately contains both an old and a new marker.
inherited_marker_with_new_evidence_repo="$(init_repo inherited-marker-with-new-evidence)"
write_baseline "${inherited_marker_with_new_evidence_repo}"
printf 'package cypher\n\nconst readerQuery = "MATCH (r:Repository {id: $id}) RETURN r"\nconst writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n' \
  >"${inherited_marker_with_new_evidence_repo}/go/internal/storage/cypher/writer.go"
cat >"${inherited_marker_with_new_evidence_repo}/go/internal/storage/cypher/README.md" <<'MD'
# Cypher Storage

Overview of the cypher storage writer package.

## Prior Evidence (an earlier, unrelated PR)

Performance Evidence: baseline writer benchmark stayed flat.

No-Observability-Change: existing writer metrics already cover this path.

## Current Evidence (this PR)

Performance Evidence: writerQuery MERGE benchmarked flat vs baseline on the
20-repo local NornicDB corpus; terminal queue depth 0, p50/p95 unchanged.

No-Observability-Change: existing writer span/metric coverage already
instruments this MERGE path.
MD
git -C "${inherited_marker_with_new_evidence_repo}" add .
git -C "${inherited_marker_with_new_evidence_repo}" commit -q -m 'add writerQuery MERGE with its own new evidence'
expect_pass "${inherited_marker_with_new_evidence_repo}"

# Regression coverage for the #5786 reducer README split (flagged during
# that lane's own review): is_evidence_file() must recognize the new sibling
# docs go/internal/reducer/*.md (e.g. gotchas-supply-chain-and-vulnerabilities.md)
# as evidence locations, or a hot reducer change whose only evidence lives in
# one of those docs is a false-red. This also proves the new location
# composes correctly with the added-lines scoping above: a marker added in a
# sibling doc's own added lines must pass, but an inherited pre-existing
# marker in a sibling doc must still fail.
reducer_sibling_doc_evidence_repo="$(init_repo reducer-sibling-doc-evidence)"
mkdir -p "${reducer_sibling_doc_evidence_repo}/go/internal/reducer"
printf 'package reducer\n\nconst readerQuery = "MATCH (r:Repository {id: $id}) RETURN r"\n' \
  >"${reducer_sibling_doc_evidence_repo}/go/internal/reducer/container_image_identity.go"
git -C "${reducer_sibling_doc_evidence_repo}" add .
git -C "${reducer_sibling_doc_evidence_repo}" commit -q -m 'baseline reducer file, no evidence docs yet'
printf 'package reducer\n\nconst readerQuery = "MATCH (r:Repository {id: $id}) RETURN r"\nconst writerQuery = "UNWIND $rows AS row MERGE (n:Image {uid: row.uid})"\n' \
  >"${reducer_sibling_doc_evidence_repo}/go/internal/reducer/container_image_identity.go"
cat >"${reducer_sibling_doc_evidence_repo}/go/internal/reducer/gotchas-supply-chain-and-vulnerabilities.md" <<'MD'
# Reducer Gotchas: Supply Chain And Vulnerabilities

## Current Evidence (this PR)

Performance Evidence: container_image_identity MERGE benchmarked flat vs
baseline on the 20-repo local NornicDB corpus; terminal queue depth 0.

No-Observability-Change: existing reducer span/metric coverage already
instruments this MERGE path.
MD
git -C "${reducer_sibling_doc_evidence_repo}" add .
git -C "${reducer_sibling_doc_evidence_repo}" commit -q -m 'add MERGE (hot change) with evidence in new #5786 sibling doc'
expect_pass "${reducer_sibling_doc_evidence_repo}"

# Compose check: recognizing the new sibling-doc location must NOT let an
# untouched, inherited marker in one satisfy the gate -- the added-lines
# scoping from the first regression above still applies to this new
# location too.
reducer_sibling_doc_inherited_repo="$(init_repo reducer-sibling-doc-inherited)"
mkdir -p "${reducer_sibling_doc_inherited_repo}/go/internal/reducer"
printf 'package reducer\n\nconst readerQuery = "MATCH (r:Repository {id: $id}) RETURN r"\n' \
  >"${reducer_sibling_doc_inherited_repo}/go/internal/reducer/container_image_identity.go"
cat >"${reducer_sibling_doc_inherited_repo}/go/internal/reducer/gotchas-supply-chain-and-vulnerabilities.md" <<'MD'
# Reducer Gotchas: Supply Chain and Vulnerabilities

## Prior Evidence (an earlier, unrelated PR)

Performance Evidence: baseline suppression benchmark stayed flat.

No-Observability-Change: existing suppression metrics already cover this path.
MD
git -C "${reducer_sibling_doc_inherited_repo}" add .
git -C "${reducer_sibling_doc_inherited_repo}" commit -q -m 'baseline reducer file + prior unrelated evidence marker in sibling doc'
printf 'package reducer\n\nconst readerQuery = "MATCH (r:Repository {id: $id}) RETURN r"\nconst writerQuery = "UNWIND $rows AS row MERGE (n:Image {uid: row.uid})"\n' \
  >"${reducer_sibling_doc_inherited_repo}/go/internal/reducer/container_image_identity.go"
cat >"${reducer_sibling_doc_inherited_repo}/go/internal/reducer/gotchas-supply-chain-and-vulnerabilities.md" <<'MD'
# Reducer Gotchas: Supply Chain And Vulnerabilities

## Prior Evidence (an earlier, unrelated PR)

Performance Evidence: baseline suppression benchmark stayed flat.

No-Observability-Change: existing suppression metrics already cover this path.
MD
git -C "${reducer_sibling_doc_inherited_repo}" add .
git -C "${reducer_sibling_doc_inherited_repo}" commit -q -m 'add MERGE (hot change, no new evidence of its own; unrelated heading typo fix)'
expect_fail "${reducer_sibling_doc_inherited_repo}"

printf 'verify-performance-evidence inherited-marker tests passed\n'
