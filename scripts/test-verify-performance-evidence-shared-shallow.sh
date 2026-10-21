#!/usr/bin/env bash
set -euo pipefail

# Companion to scripts/test-verify-performance-evidence.sh, split out to keep
# that file under the repo's 500-line cap. Invoked from its tail.
#
# Shared-shallow regressions (eshu-hq/eshu#8048): resolving a base over the
# network must not change the depth of a complete checkout. Every worktree
# shares one .git/shallow, so a --depth fetch in this gate shallows all lanes
# and fails unrelated lanes' methodology identity check. Both fetch paths are
# covered: the GITHUB_BASE_REF branch fetch and the explicit-base SHA fetch.
# Each fixture is a full file:// clone whose base exists only on the remote
# side: the gate must fetch it and the checkout must stay complete.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-performance-evidence.sh"

tmp_root="$(mktemp -d)"
# #8673: keep fixture repos out of the host git-ai daemon's Trace2 tracking.
# The daemon keys on its Trace2 socket env; any git process that emits to it
# teaches the daemon the fixture path (fixtures and their gate runs inherit
# this env). GIT_TRACE2_EVENT=0 disables the event stream (same convention as
# test-verify-moved-file-refs.sh); PERF likewise. Teardown retries stay as
# belt-and-braces.
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
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

assert_complete() {
  local label="$1"
  local dir="$2"
  if [ "$(git -C "${dir}" rev-parse --is-shallow-repository)" != "false" ]; then
    printf '%s: resolving the base rewrote shared shallow state\n' "${label}" >&2
    exit 1
  fi
}

# Path 1: GITHUB_BASE_REF branch fetch. The base branch exists only on the
# remote side; the gate must fetch it (origin/pr-base resolves afterwards)
# without shallowing the checkout.
ref_seed="$(init_repo ref-seed)"
# pr-base MUST be created after the clone: git clone fetches every branch,
# so creating it first pre-resolves origin/pr-base and the resolve-first
# guard would skip the fetch, leaving the full-depth branch-fetch path
# untested (eshu-hq/eshu#8104 review).
git clone --quiet "file://${ref_seed}" "${tmp_root}/ref-clone"
ref_clone="${tmp_root}/ref-clone"
git -C "${ref_seed}" branch pr-base
if git -C "${ref_clone}" rev-parse --verify --quiet origin/pr-base >/dev/null; then
  printf 'fixture setup broken: origin/pr-base pre-resolved, fetch path untested\n' >&2
  exit 1
fi
if [ "$(git -C "${ref_clone}" rev-parse --is-shallow-repository)" != "false" ]; then
  printf 'shallow fixture clone started shallow\n' >&2
  exit 1
fi
if env -u ESHU_PERFORMANCE_EVIDENCE_BASE -u GITHUB_BASE_REF -u GITHUB_EVENT_NAME \
  ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${ref_clone}" \
  GITHUB_BASE_REF="pr-base" \
  "${verifier}" >"${tmp_root}/ref.out" 2>"${tmp_root}/ref.err"; then
  :
else
  printf 'expected verifier to resolve the CI base on a complete checkout\n' >&2
  sed -n '1,40p' "${tmp_root}/ref.err" >&2
  exit 1
fi
assert_complete "branch-base fetch" "${ref_clone}"
if ! git -C "${ref_clone}" rev-parse --verify --quiet origin/pr-base >/dev/null; then
  printf 'branch-base fetch left origin/pr-base unresolved\n' >&2
  exit 1
fi

# Path 2: explicit-base SHA fetch. The pinned commit exists only on the
# remote side and the event is not pull_request, so the gate must fetch the
# object itself without shallowing the checkout.
sha_seed="$(init_repo sha-seed)"
git -C "${sha_seed}" commit -q --allow-empty -m second
git clone --quiet "file://${sha_seed}" "${tmp_root}/sha-clone"
sha_clone="${tmp_root}/sha-clone"
git -C "${sha_seed}" commit -q --allow-empty -m third
sha_tip="$(git -C "${sha_seed}" rev-parse HEAD)"
if [ "$(git -C "${sha_clone}" rev-parse --is-shallow-repository)" != "false" ]; then
  printf 'shallow fixture clone started shallow\n' >&2
  exit 1
fi
if env -u GITHUB_BASE_REF -u GITHUB_EVENT_NAME \
  ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${sha_clone}" \
  ESHU_PERFORMANCE_EVIDENCE_BASE="${sha_tip}" \
  "${verifier}" >"${tmp_root}/sha.out" 2>"${tmp_root}/sha.err"; then
  :
else
  printf 'expected verifier to fetch the pinned base on a complete checkout\n' >&2
  sed -n '1,40p' "${tmp_root}/sha.err" >&2
  exit 1
fi
assert_complete "explicit-base fetch" "${sha_clone}"
if ! git -C "${sha_clone}" cat-file -e "${sha_tip}^{commit}" 2>/dev/null; then
  printf 'explicit-base fetch left the pinned commit unfetched\n' >&2
  exit 1
fi

printf 'verify-performance-evidence shared-shallow tests passed\n'
