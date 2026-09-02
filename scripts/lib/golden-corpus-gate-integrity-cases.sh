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
# legitimately has none. Both directions are asserted.
#
# The subshells run inside `if`, not as bare commands: this file is sourced by a
# `set -e` script, where a bare failing subshell aborts the whole run before its
# exit status can be read -- which reads as a silent, message-less failure.
gate_integrity_nodocker_dir="$(mktemp -d -t golden-corpus-gate-integrity-nodocker.XXXXXX)"
mkdir -p "${gate_integrity_nodocker_dir}/bin"
for gate_integrity_shim in rg jq; do
	gate_integrity_shim_path="$(command -v "${gate_integrity_shim}" || true)"
	[[ -n "${gate_integrity_shim_path}" ]] ||
		fail "test harness: ${gate_integrity_shim} must be on PATH to build the no-docker preflight shim"
	ln -s "${gate_integrity_shim_path}" "${gate_integrity_nodocker_dir}/bin/${gate_integrity_shim}"
done

if (
	PATH="${gate_integrity_nodocker_dir}/bin"
	fail_capture() { printf '%s\n' "$*"; exit 1; }
	golden_corpus_require_gate_tools fail_capture 0
) >"${gate_integrity_nodocker_dir}/nocompose.log" 2>&1; then
	: # expected: --no-compose tolerates a missing docker
else
	fail "preflight must NOT require docker under --no-compose (use_compose=0): $(cat "${gate_integrity_nodocker_dir}/nocompose.log")"
fi

if (
	PATH="${gate_integrity_nodocker_dir}/bin"
	fail_capture() { printf '%s\n' "$*"; exit 1; }
	golden_corpus_require_gate_tools fail_capture 1
) >"${gate_integrity_nodocker_dir}/compose.log" 2>&1; then
	fail "preflight must require docker under compose mode (use_compose=1)"
fi
rg --fixed-strings --quiet -- "docker" "${gate_integrity_nodocker_dir}/compose.log" ||
	fail "compose-mode preflight failure must name the missing tool: docker"
rm -rf "${gate_integrity_nodocker_dir}"

# ---------------------------------------------------------------------------
# BITES (1b): a GLOBAL gitignore must not strip an untracked fixture addition.
# --exclude-standard consults core.excludesfile, so without pinning it the gate
# would silently drop a developer's in-progress fixture file whose name happens
# to match a pattern in their personal global ignore -- contradicting the
# guarantee that only repository ignore rules apply.
gate_integrity_global_dir="$(mktemp -d -t golden-corpus-gate-integrity-global.XXXXXX)"
printf 'scratch-*\n' >"${gate_integrity_global_dir}/globalignore"
gate_integrity_global_add="${repo_root}/tests/fixtures/ecosystems/container-ci-lineage/scratch-wip.txt"
[[ -e "${gate_integrity_global_add}" ]] &&
	fail "test harness: unexpected pre-existing scratch-wip.txt in the fixture source"
(
	trap 'rm -f "${gate_integrity_global_add}"' EXIT
	printf 'work in progress\n' >"${gate_integrity_global_add}"
	gate_integrity_global_corpus="${gate_integrity_global_dir}/corpus"
	mkdir -p "${gate_integrity_global_corpus}"
	(
		export GIT_CONFIG_GLOBAL="${gate_integrity_global_dir}/gitconfig"
		git config --file "${GIT_CONFIG_GLOBAL}" core.excludesfile "${gate_integrity_global_dir}/globalignore"
		corpus_dir="${gate_integrity_global_corpus}"
		corpus_fixtures=(container-ci-lineage)
		die() { fail "$*"; }
		# shellcheck source=scripts/lib/golden-corpus-stage.sh
		. "${repo_root}/scripts/lib/golden-corpus-stage.sh"
		stage_minimal_corpus >/dev/null
	)
	[[ -f "${gate_integrity_global_corpus}/container-ci-lineage/scratch-wip.txt" ]] ||
		fail "a globally-ignored (not repo-ignored) untracked fixture addition must still stage; only repository ignore rules may strip"
)
rm -rf "${gate_integrity_global_dir}"

# BITES (1c): an ignored filename carrying a character git would C-quote must
# still be removed. Without -z, git renders such a name as a quoted string and
# the rm targets that rendering rather than the file, so the ignored file
# survives -- the exact defect this staging step exists to prevent.
gate_integrity_quote_dir="$(mktemp -d -t golden-corpus-gate-integrity-quote.XXXXXX)"
gate_integrity_quote_add="${repo_root}/tests/fixtures/ecosystems/container-ci-lineage/naughty"$'	'"name.swp"
(
	trap 'rm -f "${gate_integrity_quote_add}"' EXIT
	printf 'editor swap\n' >"${gate_integrity_quote_add}"
	git -C "${repo_root}" check-ignore --quiet -- "${gate_integrity_quote_add}" ||
		fail "test harness: planted swap file is not git-ignored; this BITES proves nothing"
	gate_integrity_quote_corpus="${gate_integrity_quote_dir}/corpus"
	mkdir -p "${gate_integrity_quote_corpus}"
	(
		corpus_dir="${gate_integrity_quote_corpus}"
		corpus_fixtures=(container-ci-lineage)
		die() { fail "$*"; }
		# shellcheck source=scripts/lib/golden-corpus-stage.sh
		. "${repo_root}/scripts/lib/golden-corpus-stage.sh"
		stage_minimal_corpus >/dev/null
	)
	golden_corpus_assert_staged_pin "container-ci-lineage" \
		"${gate_integrity_quote_corpus}/container-ci-lineage" \
		"ci_cd_run:github_actions:acme:container-ci-lineage" "9100" fail
)
rm -rf "${gate_integrity_quote_dir}"

# ---------------------------------------------------------------------------
# BITES (1d): a hostile GLOBAL commit.gpgsign, core.excludesfile, and
# i18n.commitEncoding must not perturb deployable-config's staged HEAD or
# annotated tag. Without the matching pins in stage.sh's deployable-config
# block, commit.gpgsign=true kills the commit silently (no diagnostic, since
# every call is `>/dev/null 2>&1`), a core.excludesfile matching
# catalog-info.yaml (a tracked file this fixture actually commits) drops it
# from the tree, and i18n.commitEncoding writes an `encoding` header into the
# commit object -- each changes the SHA from byte-identical fixture content,
# or aborts staging outright.
gate_integrity_hostile_config_dir="$(mktemp -d -t golden-corpus-gate-integrity-hostileconfig.XXXXXX)"
gate_integrity_hostile_clean_corpus="${gate_integrity_hostile_config_dir}/clean"
gate_integrity_hostile_corpus="${gate_integrity_hostile_config_dir}/hostile"
mkdir -p "${gate_integrity_hostile_clean_corpus}" "${gate_integrity_hostile_corpus}"
(
	corpus_dir="${gate_integrity_hostile_clean_corpus}"
	corpus_fixtures=(deployable-config)
	die() { fail "$*"; }
	# shellcheck source=scripts/lib/golden-corpus-stage.sh
	. "${repo_root}/scripts/lib/golden-corpus-stage.sh"
	stage_minimal_corpus >/dev/null
)
gate_integrity_hostile_clean_head="$(git -C "${gate_integrity_hostile_clean_corpus}/deployable-config" rev-parse HEAD)"
gate_integrity_hostile_clean_tag="$(git -C "${gate_integrity_hostile_clean_corpus}/deployable-config" rev-parse v1.0.0-annotated)"

gate_integrity_hostile_excludes="${gate_integrity_hostile_config_dir}/globalignore"
printf 'catalog-info.yaml\n' >"${gate_integrity_hostile_excludes}"
(
	export GIT_CONFIG_GLOBAL="${gate_integrity_hostile_config_dir}/gitconfig"
	git config --file "${GIT_CONFIG_GLOBAL}" commit.gpgsign true
	git config --file "${GIT_CONFIG_GLOBAL}" core.excludesfile "${gate_integrity_hostile_excludes}"
	git config --file "${GIT_CONFIG_GLOBAL}" i18n.commitEncoding ISO-8859-1
	corpus_dir="${gate_integrity_hostile_corpus}"
	corpus_fixtures=(deployable-config)
	die() { fail "$*"; }
	# shellcheck source=scripts/lib/golden-corpus-stage.sh
	. "${repo_root}/scripts/lib/golden-corpus-stage.sh"
	stage_minimal_corpus >/dev/null
) || fail "deployable-config staging must succeed under a hostile global gpgsign/excludesfile/commitEncoding config"
[[ -d "${gate_integrity_hostile_corpus}/deployable-config/.git" ]] ||
	fail "deployable-config must produce a Git repository under a hostile global config"
gate_integrity_hostile_head="$(git -C "${gate_integrity_hostile_corpus}/deployable-config" rev-parse HEAD 2>&1)" ||
	fail "deployable-config staged HEAD must exist under a hostile global config, got: ${gate_integrity_hostile_head}"
[[ "${gate_integrity_hostile_head}" == "${gate_integrity_hostile_clean_head}" ]] ||
	fail "deployable-config staged HEAD under a hostile global gpgsign/excludesfile/commitEncoding config (${gate_integrity_hostile_head}) must match the clean staged HEAD (${gate_integrity_hostile_clean_head})"
gate_integrity_hostile_tag="$(git -C "${gate_integrity_hostile_corpus}/deployable-config" rev-parse v1.0.0-annotated 2>&1)" ||
	fail "deployable-config staged annotated tag must exist under a hostile global config, got: ${gate_integrity_hostile_tag}"
[[ "${gate_integrity_hostile_tag}" == "${gate_integrity_hostile_clean_tag}" ]] ||
	fail "deployable-config annotated tag under a hostile global config (${gate_integrity_hostile_tag}) must match the clean annotated tag (${gate_integrity_hostile_clean_tag})"
rm -rf "${gate_integrity_hostile_config_dir}"

gate_integrity_cases_completed=1
