#!/usr/bin/env bash
set -euo pipefail

# Companion to scripts/test-verify-performance-evidence.sh, split out to keep
# that file under the repo's 500-line cap. Invoked from its tail.
#
# Regression coverage for eshu-hq/eshu#7134. test.yml pins
# ESHU_PERFORMANCE_EVIDENCE_BASE to github.event.pull_request.base.sha and
# checks out the PR merge ref at fetch-depth 2. When main moves before the job
# starts, GitHub rebuilds the merge ref on the newer main, so the event base SHA
# is not in the clone and the verifier died with `fatal: bad object <sha>`
# (exit 128). The shape is reproduced with a real shallow clone, not a mock:
#
#  1. pull_request event, merge-commit HEAD: base resolves to HEAD^1 (the tree
#     the merge was built on). A docs-only PR passes; a hot-path PR with no
#     evidence still FAILS on the hot file, so the substitution cannot hide a
#     violation.
#  2. Any other event: the base object is fetched from origin and used.
#  3. Base not fetchable: exit non-zero with an actionable message, never a
#     silent pass and never `bad object`.
#
# ESHU_PERF_EVIDENCE_TEST_VERIFIER points the suite at another verifier copy;
# it exists so the RED run against the pre-fix script is reproducible.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${ESHU_PERF_EVIDENCE_TEST_VERIFIER:-${repo_root}/scripts/verify-performance-evidence.sh}"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

git_in() {
  local dir="$1"
  shift
  git -C "${dir}" "$@"
}

origin_dir="${tmp_root}/origin"
git init -q -b main "${origin_dir}"
git_in "${origin_dir}" config user.email "test@example.invalid"
git_in "${origin_dir}" config user.name "Eshu Test"
mkdir -p "${origin_dir}/docs" "${origin_dir}/go/internal/storage/cypher"
printf 'baseline\n' >"${origin_dir}/docs/note.md"
printf 'package cypher\n' >"${origin_dir}/go/internal/storage/cypher/writer.go"
git_in "${origin_dir}" add .
git_in "${origin_dir}" commit -q -m baseline
event_base="$(git_in "${origin_dir}" rev-parse HEAD)"

# PR 1: docs-only. PR 2: hot-path change with no evidence.
git_in "${origin_dir}" checkout -q -b docs-pr
printf 'branch docs edit\n' >"${origin_dir}/docs/note.md"
git_in "${origin_dir}" add .
git_in "${origin_dir}" commit -q -m 'PR docs edit'
git_in "${origin_dir}" checkout -q main
git_in "${origin_dir}" checkout -q -b hot-pr
printf 'package cypher\nconst readerQuery = "MATCH (n) RETURN n"\n' \
  >"${origin_dir}/go/internal/storage/cypher/reader.go"
git_in "${origin_dir}" add .
git_in "${origin_dir}" commit -q -m 'PR hot change without evidence'
git_in "${origin_dir}" checkout -q main

# main advances after the PR event: a hot change with no bearing on either PR.
printf 'package cypher\nconst writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n' \
  >"${origin_dir}/go/internal/storage/cypher/writer.go"
git_in "${origin_dir}" add .
git_in "${origin_dir}" commit -q -m 'main hot change after PR event'

# GitHub rebuilds refs/pull/N/merge on the NEWER main tip.
for pr in docs-pr hot-pr; do
  git_in "${origin_dir}" checkout -q -b "${pr}-merge" main
  git_in "${origin_dir}" merge -q --no-ff "${pr}" -m "PR merge commit ${pr}"
  git_in "${origin_dir}" checkout -q main
done

shallow_checkout() {
  local pr="$1"
  local dest="${tmp_root}/checkout-${pr}"
  git clone -q --depth=2 --branch "${pr}-merge" "file://${origin_dir}" "${dest}"
  # Fixture guard: the event base must be absent, or this proves nothing.
  if git_in "${dest}" cat-file -e "${event_base}^{commit}" 2>/dev/null; then
    printf 'fixture broken: event base %s is present in the shallow checkout\n' "${event_base}" >&2
    exit 1
  fi
  printf '%s\n' "${dest}"
}

# run_verifier <checkout> <event-name> <base>: stdout/stderr land in
# ${tmp_root}/out and ${tmp_root}/err; the exit status is returned.
run_verifier() {
  local checkout="$1" event_name="$2" base="$3"
  local status=0
  env -u GITHUB_BASE_REF -u GITHUB_EVENT_NAME \
    ${event_name:+GITHUB_EVENT_NAME="${event_name}"} \
    ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${checkout}" \
    ESHU_PERFORMANCE_EVIDENCE_BASE="${base}" \
    "${verifier}" >"${tmp_root}/out" 2>"${tmp_root}/err" || status=$?
  return "${status}"
}

show_output() {
  printf -- '--- stdout ---\n' >&2
  sed -n '1,60p' "${tmp_root}/out" >&2
  printf -- '--- stderr ---\n' >&2
  sed -n '1,60p' "${tmp_root}/err" >&2
}

fail_case() {
  printf 'FAIL: %s\n' "$1" >&2
  show_output
  exit 1
}

docs_checkout="$(shallow_checkout docs-pr)"
hot_checkout="$(shallow_checkout hot-pr)"

# Case 1a: pull_request event, base missing -> HEAD^1; docs-only PR passes.
status=0
run_verifier "${docs_checkout}" pull_request "${event_base}" || status=$?
[ "${status}" -eq 0 ] || fail_case "pull_request + missing base: expected pass via HEAD^1, got exit ${status}"
if rg -q 'bad object' "${tmp_root}/err"; then
  fail_case "pull_request + missing base: bad object leaked"
fi
rg -q 'using HEAD\^1' "${tmp_root}/err" || fail_case "expected the HEAD^1 substitution notice on stderr"
rg -q 'no hot Cypher/concurrency/runtime files changed' "${tmp_root}/out" \
  || fail_case "expected docs-only PR to see no hot files (main's writer.go change must not be attributed)"

# Case 1b: same shape, but the PR itself changes hot code with no evidence.
# HEAD^1 must not hide it.
status=0
run_verifier "${hot_checkout}" pull_request "${event_base}" || status=$?
[ "${status}" -eq 1 ] || fail_case "hot PR without evidence: expected exit 1, got ${status}"
rg -q 'reader\.go' "${tmp_root}/err" || fail_case "hot PR must be reported on its own hot file"
if rg -q 'writer\.go' "${tmp_root}/err"; then
  fail_case "main's writer.go change was attributed to the PR"
fi

# Case 2: not a pull_request event (or no event): fetch the base from origin.
# The two-dot fallback then sees main's newer change (documented, pre-existing
# shallow-clone limit), so assert only that the object was fetched and the
# gate ran to a verdict instead of dying on the missing object.
for event_name in "" push; do
  status=0
  run_verifier "${docs_checkout}" "${event_name}" "${event_base}" || status=$?
  if rg -q 'bad object|not a commit in this checkout' "${tmp_root}/err"; then
    fail_case "event '${event_name}': expected the base to be fetched, not rejected"
  fi
  case "${status}" in
    0 | 1) ;;
    *) fail_case "event '${event_name}': expected a verdict (0 or 1), got exit ${status}" ;;
  esac
  git_in "${docs_checkout}" cat-file -e "${event_base}^{commit}" \
    || fail_case "event '${event_name}': base object was not fetched"
  # Reset for the next iteration so fetch is proven again.
  docs_checkout="$(rm -rf "${docs_checkout}" && shallow_checkout docs-pr)"
done

# Case 3: base cannot be fetched -> clear, actionable, non-zero. Applies with
# and without the pull_request event (a non-merge HEAD gets no HEAD^1 rescue).
bogus_base="0123456789abcdef0123456789abcdef01234567"
for event_name in "" push; do
  status=0
  run_verifier "${docs_checkout}" "${event_name}" "${bogus_base}" || status=$?
  [ "${status}" -eq 1 ] || fail_case "unfetchable base (event '${event_name}'): expected exit 1, got ${status}"
  rg -q 'could not be fetched from origin' "${tmp_root}/err" \
    || fail_case "unfetchable base: expected the actionable message"
  rg -q "${bogus_base}" "${tmp_root}/err" || fail_case "unfetchable base: message must name the base"
  if rg -q 'bad object' "${tmp_root}/err"; then
    fail_case "unfetchable base: raw git 'bad object' leaked instead of the message"
  fi
done

# Case 3b: pull_request event but HEAD is NOT a merge commit and the base is
# unfetchable -> fail, do not substitute HEAD^1 (that would silently narrow the
# diff to the last commit).
plain_checkout="${tmp_root}/checkout-plain"
git clone -q --depth=2 --branch docs-pr "file://${origin_dir}" "${plain_checkout}"
if git_in "${plain_checkout}" rev-parse --verify --quiet 'HEAD^2' >/dev/null; then
  fail_case "fixture broken: plain checkout HEAD is a merge commit"
fi
status=0
run_verifier "${plain_checkout}" pull_request "${bogus_base}" || status=$?
[ "${status}" -eq 1 ] || fail_case "pull_request + non-merge HEAD + unfetchable base: expected exit 1, got ${status}"
rg -q 'could not be fetched from origin' "${tmp_root}/err" \
  || fail_case "non-merge HEAD must not be rescued by HEAD^1"

printf 'verify-performance-evidence missing-base test passed\n'
