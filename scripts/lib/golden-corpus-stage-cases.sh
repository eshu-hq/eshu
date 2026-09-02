#!/usr/bin/env bash
# Functional cases for the deterministic Git fixture staged by the B-7 gate.
# This file is sourced by scripts/test-verify-golden-corpus-gate.sh after its
# matcher helpers and shared paths are initialized.
#
# The staged-pin comparison itself (#6401) lives in one place,
# golden_corpus_assert_staged_pin (scripts/lib/golden-corpus-gate-integrity.sh),
# shared with the live gate so this self-test and the live gate can never
# silently diverge on what "matches the pin" means.

stage_case_dir="$(mktemp -d -t golden-corpus-stage-case.XXXXXX)"
stage_case_corpus="${stage_case_dir}/corpus"
stage_case_git_config="${stage_case_dir}/gitconfig"
mkdir -p "${stage_case_corpus}"
git config --file "${stage_case_git_config}" init.defaultObjectFormat sha256

(
	export GIT_CONFIG_GLOBAL="${stage_case_git_config}"
	corpus_dir="${stage_case_corpus}"
	corpus_fixtures=(container-ci-lineage github_actions_workflows deployable-config deployable-source)
	die() { printf 'golden-corpus-stage-case: %s\n' "$*" >&2; exit 1; }
	# shellcheck source=scripts/lib/golden-corpus-stage.sh
	. "${repo_root}/scripts/lib/golden-corpus-stage.sh"
	stage_minimal_corpus >/dev/null
)

# shellcheck source=scripts/lib/golden-corpus-gate-integrity.sh
. "${repo_root}/scripts/lib/golden-corpus-gate-integrity.sh"

stage_case_repo="${stage_case_corpus}/container-ci-lineage"
[[ -d "${stage_case_repo}/.git" ]] ||
	fail "container-ci-lineage staging must create deterministic Git history"
[[ -z "$(git -C "${stage_case_repo}" status --porcelain)" ]] ||
	fail "container-ci-lineage staged Git history must include its complete working tree"
stage_case_head="$(git -C "${stage_case_repo}" rev-parse HEAD)"
[[ "${#stage_case_head}" -eq 40 ]] ||
	fail "container-ci-lineage staged HEAD must use SHA-1, got ${#stage_case_head} characters"
golden_corpus_assert_staged_pin "container-ci-lineage" "${stage_case_repo}" \
	"ci_cd_run:github_actions:acme:container-ci-lineage" "9100" fail

stage_case_input_repo="${stage_case_corpus}/github_actions_workflows"
[[ -d "${stage_case_input_repo}/.git" ]] ||
	fail "github_actions_workflows staging must create deterministic Git history"
[[ -z "$(git -C "${stage_case_input_repo}" status --porcelain)" ]] ||
	fail "github_actions_workflows staged Git history must include its complete working tree"
stage_case_input_head="$(git -C "${stage_case_input_repo}" rev-parse HEAD)"
[[ "${#stage_case_input_head}" -eq 40 ]] ||
	fail "github_actions_workflows staged HEAD must use SHA-1, got ${#stage_case_input_head} characters"
golden_corpus_assert_staged_pin "github_actions_workflows" "${stage_case_input_repo}" \
	"ci_cd_run:github_actions:acme:github_actions_workflows" "9200" fail

stage_case_deployable_repo="${stage_case_corpus}/deployable-config"
stage_case_deployable_head="$(git -C "${stage_case_deployable_repo}" rev-parse HEAD)"
[[ "${#stage_case_deployable_head}" -eq 40 ]] ||
	fail "deployable-config staged HEAD must use SHA-1, got ${#stage_case_deployable_head} characters"

# Staging must be hermetic against an inherited Git identity.
#
# git reads GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL, GIT_COMMITTER_NAME and
# GIT_COMMITTER_EMAIL from the environment, and those OUTRANK the `git config
# user.*` that stage_deterministic_git_fixture sets. A developer shell that
# exports any of them therefore produces a different author or committer, hence
# a different commit SHA, from byte-identical fixture content -- the tree hash
# is unchanged, only the commit metadata differs.
#
# That reads as fixture drift and is not. It cost one investigation a wrong
# diagnosis, a wrongly-edited cassette pin, and several hours: the staged SHA
# was reproduced by hand in the same contaminated shell, which agreed with the
# gate and looked like corroboration when it was one fault counted twice.
#
# The cases above cannot catch it. They stage in the ambient environment, and CI
# runners export none of these, so the pins match there whether or not staging is
# hermetic. This case contaminates all four variables deliberately and requires
# the pins to hold anyway.
stage_case_hostile_corpus="${stage_case_dir}/hostile"
mkdir -p "${stage_case_hostile_corpus}"
(
	export GIT_CONFIG_GLOBAL="${stage_case_git_config}"
	export GIT_AUTHOR_NAME="Contaminating Author"
	export GIT_AUTHOR_EMAIL="contaminating-author@example.invalid"
	export GIT_COMMITTER_NAME="Contaminating Committer"
	export GIT_COMMITTER_EMAIL="contaminating-committer@example.invalid"
	corpus_dir="${stage_case_hostile_corpus}"
	corpus_fixtures=(container-ci-lineage github_actions_workflows)
	die() { printf 'golden-corpus-stage-case: %s\n' "$*" >&2; exit 1; }
	# shellcheck source=scripts/lib/golden-corpus-stage.sh
	. "${repo_root}/scripts/lib/golden-corpus-stage.sh"
	stage_minimal_corpus >/dev/null
)

golden_corpus_assert_staged_pin "container-ci-lineage" \
	"${stage_case_hostile_corpus}/container-ci-lineage" \
	"ci_cd_run:github_actions:acme:container-ci-lineage" "9100" fail
golden_corpus_assert_staged_pin "github_actions_workflows" \
	"${stage_case_hostile_corpus}/github_actions_workflows" \
	"ci_cd_run:github_actions:acme:github_actions_workflows" "9200" fail

rm -rf "${stage_case_dir}"
stage_cases_completed=1
