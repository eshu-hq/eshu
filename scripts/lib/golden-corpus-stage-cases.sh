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
# git reads GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL, GIT_COMMITTER_NAME,
# GIT_COMMITTER_EMAIL, GIT_AUTHOR_DATE and GIT_COMMITTER_DATE from the
# environment, and those OUTRANK the `git config user.*` that
# stage_deterministic_git_fixture sets. A developer shell that exports any of
# them therefore produces a different author, committer, or date, hence a
# different commit SHA, from byte-identical fixture content -- the tree hash
# is unchanged, only the commit metadata differs.
#
# That reads as fixture drift and is not. It cost one investigation a wrong
# diagnosis, a wrongly-edited cassette pin, and several hours: the staged SHA
# was reproduced by hand in the same contaminated shell, which agreed with the
# gate and looked like corroboration when it was one fault counted twice.
#
# The cases above cannot catch it. They stage in the ambient environment, and CI
# runners export none of these, so the pins match there whether or not staging is
# hermetic. This case contaminates all six variables deliberately and requires
# the pins to hold anyway -- including deployable-config, which has no
# cassette-pinned commit_sha but must still produce the same commit and
# annotated-tag SHA as a clean stage, since
# golden-corpus-service-changed-since.sh commits into this same fixture.
stage_case_hostile_corpus="${stage_case_dir}/hostile"
mkdir -p "${stage_case_hostile_corpus}"
# The contamination is confined to this subshell on purpose -- that is the whole
# point of the case, and it is why the pin assertions afterwards run in a clean
# environment. shellcheck reads the subshell-local export as a probable mistake.
# shellcheck disable=SC2030,SC2031
(
	export GIT_CONFIG_GLOBAL="${stage_case_git_config}"
	export GIT_AUTHOR_NAME="Contaminating Author"
	export GIT_AUTHOR_EMAIL="contaminating-author@example.invalid"
	export GIT_COMMITTER_NAME="Contaminating Committer"
	export GIT_COMMITTER_EMAIL="contaminating-committer@example.invalid"
	export GIT_AUTHOR_DATE="2001-01-01T00:00:00Z"
	export GIT_COMMITTER_DATE="2001-01-01T00:00:00Z"
	corpus_dir="${stage_case_hostile_corpus}"
	corpus_fixtures=(container-ci-lineage github_actions_workflows deployable-config)
	die() { printf 'golden-corpus-stage-case: %s\n' "$*" >&2; exit 1; }
	# shellcheck source=scripts/lib/golden-corpus-stage.sh
	. "${repo_root}/scripts/lib/golden-corpus-stage.sh"
	stage_minimal_corpus >/dev/null
) || fail "hostile stage case: staging failed under a contaminated git identity, so the pins below would report a mismatch that is really a staging error"

golden_corpus_assert_staged_pin "container-ci-lineage" \
	"${stage_case_hostile_corpus}/container-ci-lineage" \
	"ci_cd_run:github_actions:acme:container-ci-lineage" "9100" fail
golden_corpus_assert_staged_pin "github_actions_workflows" \
	"${stage_case_hostile_corpus}/github_actions_workflows" \
	"ci_cd_run:github_actions:acme:github_actions_workflows" "9200" fail

# deployable-config has no cassette-pinned commit_sha (only its SHA-1 length is
# checked above), so golden_corpus_assert_staged_pin cannot cover it. Assert
# its hostile-staged HEAD and annotated tag directly against the clean stage
# captured earlier -- the only way this equality holds under a contaminated
# identity and contaminated dates is if every commit and tag site here pins
# both inline, closing the same gap for the fixture
# golden-corpus-service-changed-since.sh commits into.
stage_case_hostile_deployable_head="$(git -C "${stage_case_hostile_corpus}/deployable-config" rev-parse HEAD)"
[[ "${stage_case_hostile_deployable_head}" == "${stage_case_deployable_head}" ]] ||
	fail "deployable-config staged HEAD under a contaminated git identity (${stage_case_hostile_deployable_head}) must match the clean staged HEAD (${stage_case_deployable_head})"

stage_case_hostile_deployable_tag="$(git -C "${stage_case_hostile_corpus}/deployable-config" rev-parse v1.0.0-annotated)"
stage_case_clean_deployable_tag="$(git -C "${stage_case_deployable_repo}" rev-parse v1.0.0-annotated)"
[[ "${stage_case_hostile_deployable_tag}" == "${stage_case_clean_deployable_tag}" ]] ||
	fail "deployable-config annotated tag under a contaminated git identity (${stage_case_hostile_deployable_tag}) must match the clean annotated tag (${stage_case_clean_deployable_tag})"

rm -rf "${stage_case_dir}"
stage_cases_completed=1
