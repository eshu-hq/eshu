#!/usr/bin/env bash
# Functional + structural cases for #6401: the staged-fixture pin proof and
# the required-tool preflight (scripts/lib/golden-corpus-gate-integrity.sh).
# Sourced by scripts/test-verify-golden-corpus-gate.sh after its
# require()/require_in helpers, repo_root, fail(), and script are already in
# scope, and after golden-corpus-gate-integrity.sh has been syntax-checked and
# sourced.
#
# Every capture of an INTENTIONALLY failing subshell below uses the
# `( ... ) >capture 2>&1 || status=$?` idiom, not `cmd; status=$?` -- a bare
# assignment following a failing command is NOT exempt from this file's
# inherited `set -e`, only an assignment on the `||` side of a tested command
# is (https://mywiki.wooledge.org/BashFAQ/105; see
# scripts/lib/golden-corpus-local-backend.sh for the same rule stated against
# the same pitfall). `cmd; status=$?` aborted this file silently, with zero
# output, before the fix.

# Structural: the live gate must actually CALL both helpers, not only the
# self-test -- this is the #6401 gap itself. The staged-pin assertion already
# existed in golden-corpus-stage-cases.sh, which only the self-test runs; the
# live gate never asserted it and never checked for its required tools.
require "tool preflight wired into the live gate" "golden_corpus_require_gate_tools"
require "staged-pin assertion wired into the live gate" "golden_corpus_assert_pinned_fixtures"

# ---------------------------------------------------------------------------
# BITES (1): a git-ignored file in the SOURCE fixture directory must not
# perturb the staged deterministic HEAD. This is the central proof: it
# reproduces the actual defect (cp -R copying .gitignore'd junk into a commit
# stage.sh then makes) rather than merely exercising new plumbing.
gate_integrity_ignored_source="${repo_root}/tests/fixtures/ecosystems/container-ci-lineage/.DS_Store"
[[ -e "${gate_integrity_ignored_source}" ]] &&
	fail "test harness: unexpected pre-existing .DS_Store in the container-ci-lineage fixture source"
(
	trap 'rm -f "${gate_integrity_ignored_source}"' EXIT
	printf 'macOS Finder metadata (test fixture)\n' >"${gate_integrity_ignored_source}"
	git -C "${repo_root}" check-ignore --quiet -- \
		"tests/fixtures/ecosystems/container-ci-lineage/.DS_Store" ||
		fail "test harness: planted .DS_Store is not actually git-ignored; this BITES proves nothing"

	gate_integrity_stage_dir="$(mktemp -d -t golden-corpus-gate-integrity-stage.XXXXXX)"
	gate_integrity_stage_corpus="${gate_integrity_stage_dir}/corpus"
	mkdir -p "${gate_integrity_stage_corpus}"
	(
		corpus_dir="${gate_integrity_stage_corpus}"
		corpus_fixtures=(container-ci-lineage)
		die() { fail "$*"; }
		# shellcheck source=scripts/lib/golden-corpus-stage.sh
		. "${repo_root}/scripts/lib/golden-corpus-stage.sh"
		stage_minimal_corpus >/dev/null
	)
	golden_corpus_assert_staged_pin "container-ci-lineage" \
		"${gate_integrity_stage_corpus}/container-ci-lineage" \
		"ci_cd_run:github_actions:acme:container-ci-lineage" "9100" fail
	rm -rf "${gate_integrity_stage_dir}"
)

# ---------------------------------------------------------------------------
# BITES (2): a genuine staged-pin mismatch must fail fast (no Docker) and name
# the fixture, both SHAs, and the offending tree entry.
gate_integrity_mismatch_dir="$(mktemp -d -t golden-corpus-gate-integrity-mismatch.XXXXXX)"
gate_integrity_mismatch_repo="${gate_integrity_mismatch_dir}/container-ci-lineage"
mkdir -p "${gate_integrity_mismatch_repo}"
printf 'unexpected drift\n' >"${gate_integrity_mismatch_repo}/drift-marker.txt"
git -C "${gate_integrity_mismatch_repo}" -c init.defaultBranch=main init --object-format=sha1 >/dev/null 2>&1
git -C "${gate_integrity_mismatch_repo}" config user.email "gate@eshu.local" >/dev/null 2>&1
git -C "${gate_integrity_mismatch_repo}" config user.name "Golden Gate" >/dev/null 2>&1
git -C "${gate_integrity_mismatch_repo}" add -A >/dev/null 2>&1
GIT_AUTHOR_DATE="2026-08-04T12:00:00Z" GIT_COMMITTER_DATE="2026-08-04T12:00:00Z" \
	git -C "${gate_integrity_mismatch_repo}" commit -m "drift" >/dev/null 2>&1

gate_integrity_mismatch_expected="$(golden_corpus_pinned_commit_sha \
	"ci_cd_run:github_actions:acme:container-ci-lineage" "9100")"
gate_integrity_mismatch_head="$(git -C "${gate_integrity_mismatch_repo}" rev-parse HEAD)"
gate_integrity_mismatch_capture="${gate_integrity_mismatch_dir}/capture.log"
gate_integrity_mismatch_status=0
(
	die_capture() { printf '%s\n' "$*"; exit 1; }
	golden_corpus_assert_staged_pin "container-ci-lineage" "${gate_integrity_mismatch_repo}" \
		"ci_cd_run:github_actions:acme:container-ci-lineage" "9100" die_capture
) >"${gate_integrity_mismatch_capture}" 2>&1 || gate_integrity_mismatch_status=$?
[[ "${gate_integrity_mismatch_status}" -ne 0 ]] ||
	fail "staged-pin assertion must fail fast on a HEAD/pin mismatch, without Docker"
for gate_integrity_needle in \
	"container-ci-lineage" "${gate_integrity_mismatch_head}" \
	"${gate_integrity_mismatch_expected}" "drift-marker.txt"; do
	rg --fixed-strings --quiet -- "${gate_integrity_needle}" "${gate_integrity_mismatch_capture}" ||
		fail "staged-pin mismatch message must name: ${gate_integrity_needle}"
done
rm -rf "${gate_integrity_mismatch_dir}"

# ---------------------------------------------------------------------------
# BITES (3a): the tool preflight must name the exact missing tool.
gate_integrity_empty_path="$(mktemp -d -t golden-corpus-gate-integrity-path.XXXXXX)"
gate_integrity_preflight_capture="${gate_integrity_empty_path}/capture.log"
gate_integrity_preflight_status=0
(
	PATH="${gate_integrity_empty_path}"
	fail_capture() { printf '%s\n' "$*"; exit 1; }
	golden_corpus_require_tools fail_capture rg
) >"${gate_integrity_preflight_capture}" 2>&1 || gate_integrity_preflight_status=$?
[[ "${gate_integrity_preflight_status}" -ne 0 ]] ||
	fail "tool preflight must fail when rg is hidden from PATH"
rg --fixed-strings --quiet -- "rg" "${gate_integrity_preflight_capture}" ||
	fail "tool preflight failure must name the missing tool: rg"

# BITES (3b): the changed-since marker count must not fold a missing rg into
# a false (old=0, new=0) precondition failure -- it must report a missing-tool
# failure that names rg instead.
gate_integrity_changed_since_dir="$(mktemp -d -t golden-corpus-gate-integrity-changed-since.XXXXXX)"
gate_integrity_changed_since_fixture="${gate_integrity_changed_since_dir}/supply-chain-demo-db/config/freshness.cfg"
mkdir -p "$(dirname "${gate_integrity_changed_since_fixture}")"
printf 'release_marker = "baseline"\n' >"${gate_integrity_changed_since_fixture}"
gate_integrity_changed_since_capture="${gate_integrity_changed_since_dir}/capture.log"
gate_integrity_changed_since_status=0
(
	corpus_dir="${gate_integrity_changed_since_dir}"
	die() { printf '%s\n' "$*"; exit 1; }
	PATH="${gate_integrity_empty_path}"
	# shellcheck source=scripts/lib/golden-corpus-changed-since.sh
	. "${repo_root}/scripts/lib/golden-corpus-changed-since.sh"
	golden_changed_since_mutate_fixture
) >"${gate_integrity_changed_since_capture}" 2>&1 || gate_integrity_changed_since_status=$?
[[ "${gate_integrity_changed_since_status}" -ne 0 ]] ||
	fail "changed-since fixture mutation must fail when rg is missing from PATH"
if rg --fixed-strings --quiet -- "old=0, new=0" "${gate_integrity_changed_since_capture}"; then
	fail "changed-since must not fold a missing rg into a false (old=0, new=0) precondition failure"
fi
rg --fixed-strings --quiet -- "rg" "${gate_integrity_changed_since_capture}" ||
	fail "changed-since missing-tool failure must name rg"
rm -rf "${gate_integrity_empty_path}" "${gate_integrity_changed_since_dir}"

# ---------------------------------------------------------------------------
# BITES (3c): the preflight must demand docker ONLY in compose mode. Every
# docker call in this gate is guarded by use_compose
# (golden-corpus-host-helpers.sh pg(), golden-corpus-cleanup.sh teardown), so
# requiring it under --no-compose would fail a supported mode on a machine that
# legitimately has none. Both directions are asserted: a missing docker must be
# fatal at use_compose=1 and tolerated at use_compose=0.
gate_integrity_nodocker_dir="$(mktemp -d -t golden-corpus-gate-integrity-nodocker.XXXXXX)"
mkdir -p "${gate_integrity_nodocker_dir}/bin"
for gate_integrity_shim in rg jq; do
	command -v "${gate_integrity_shim}" >/dev/null 2>&1 &&
		ln -s "$(command -v "${gate_integrity_shim}")" "${gate_integrity_nodocker_dir}/bin/${gate_integrity_shim}"
done
(
	PATH="${gate_integrity_nodocker_dir}/bin"
	fail_capture() { printf '%s\n' "$*"; exit 1; }
	golden_corpus_require_gate_tools fail_capture 0
) >"${gate_integrity_nodocker_dir}/nocompose.log" 2>&1
gate_integrity_nodocker_status=$?
[[ "${gate_integrity_nodocker_status}" -eq 0 ]] ||
	fail "preflight must NOT require docker under --no-compose (use_compose=0), got: $(cat "${gate_integrity_nodocker_dir}/nocompose.log")"
(
	PATH="${gate_integrity_nodocker_dir}/bin"
	fail_capture() { printf '%s\n' "$*"; exit 1; }
	golden_corpus_require_gate_tools fail_capture 1
) >"${gate_integrity_nodocker_dir}/compose.log" 2>&1
gate_integrity_compose_status=$?
[[ "${gate_integrity_compose_status}" -ne 0 ]] ||
	fail "preflight must require docker under compose mode (use_compose=1)"
rg --fixed-strings --quiet -- "docker" "${gate_integrity_nodocker_dir}/compose.log" ||
	fail "compose-mode preflight failure must name the missing tool: docker"
rm -rf "${gate_integrity_nodocker_dir}"

gate_integrity_cases_completed=1
