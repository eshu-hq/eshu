#!/usr/bin/env bash
# Functional cases for the deterministic Git fixture staged by the B-7 gate.
# This file is sourced by scripts/test-verify-golden-corpus-gate.sh after its
# matcher helpers and shared paths are initialized.
#
# The staged-pin comparison itself (#6401) lives in one place,
# golden_corpus_assert_staged_pin (scripts/lib/golden-corpus-gate-integrity.sh),
# shared with the live gate so this self-test and the live gate can never
# silently diverge on what "matches the pin" means.

# Every git command that MUTATES a fixture repository must route through
# golden_corpus_git (scripts/lib/golden-corpus-git.sh). A bare one reintroduces
# the developer's global configuration, and it would be invisible to every case
# below: those stage under a planted hostile config, so a call that escapes the
# planting still produces the right SHA on a machine whose real global config
# is empty -- which is every CI runner and most laptops. The failure appears
# only for the developer who has the offending setting, as fixture drift in a
# checkout that has none.
#
# So this is a source check, not a behavior check, on purpose. It is the only
# assertion here that can fail on a machine where the behavior looks fine.
#
# Scoped to the mutating verbs rather than to `git` outright, because reads
# (rev-parse, ls-tree, cat-file, check-ignore) cannot be perturbed into a wrong
# SHA by config, and the cases files must be free to call `git config --file`
# to PLANT the hostile config a case depends on. Comment lines are skipped so
# prose naming a command does not trip it.
# The verb must be git's own SUBCOMMAND, so the pattern walks only over `-C`
# and `-c` option pairs to reach it. An earlier version allowed any text in
# between and matched a fail() message that happened to contain both "git" and
# "tag" -- a guard that cries wolf gets deleted, which loses the guard.
stage_case_mutating_git='(^|[^_[:alnum:]])git([[:space:]]+-[Cc][[:space:]]+("[^"]*"|[^[:space:]]+))*[[:space:]]+(init|add|commit|tag|update-index)([[:space:]]|$)'
stage_case_bare_git="$(rg -n --no-filename "${stage_case_mutating_git}" \
	"${repo_root}"/scripts/lib/golden-corpus-*.sh \
	"${repo_root}/scripts/verify-golden-corpus-gate.sh" |
	rg -v '^[0-9]+:[[:space:]]*#' || true)"
[[ -z "${stage_case_bare_git}" ]] ||
	fail "these golden-corpus git calls mutate a repository without routing through golden_corpus_git, so a developer's global config reaches a fixture commit: ${stage_case_bare_git}"

stage_case_dir="$(mktemp -d -t golden-corpus-stage-case.XXXXXX)"
stage_case_corpus="${stage_case_dir}/corpus"
stage_case_git_config="${stage_case_dir}/gitconfig"
stage_case_git_attributes="${stage_case_dir}/gitattributes"
stage_case_git_excludes="${stage_case_dir}/gitexcludes"
mkdir -p "${stage_case_corpus}"
printf 'Dockerfile\n' >"${stage_case_git_excludes}"
git config --file "${stage_case_git_config}" init.defaultObjectFormat sha256

# A clean filter reached through core.attributesFile is the reason staging
# neutralizes the whole global config layer rather than enumerating keys.
#
# git applies a `clean` filter on `git add`, so the bytes that reach the index
# are not the bytes on disk. The fixture content is unchanged, the tree hash
# moves anyway, and the cassette pin no longer matches -- the gate then reports
# fixture drift against a checkout that has none. Measured on git 2.55.0 with
# the filter below: container-ci-lineage HEAD moved fe05491e -> f82875a3.
#
# core.attributesFile and filter.<name>.clean are two more keys, which is
# exactly why they are the wrong thing to enumerate. Neither appears in any
# `git config` line in golden-corpus-stage.sh, and no list of knobs written
# today survives the next git release adding one. Staging instead runs every
# fixture command through golden_corpus_git (scripts/lib/golden-corpus-git.sh),
# which switches the global and system config off wholesale; these two keys are
# in this file to give that decision a test that fails when it is undone.
printf 'Dockerfile filter=golden-corpus-mangle\ncatalog-info.yaml filter=golden-corpus-mangle\n' \
	>"${stage_case_git_attributes}"
git config --file "${stage_case_git_config}" core.attributesFile "${stage_case_git_attributes}"
git config --file "${stage_case_git_config}" filter.golden-corpus-mangle.clean "sed 's/^/# mangled /'"

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
	# The GIT_CONFIG_COUNT family is a separate vector from the config FILES,
	# and it outranks every one of them: git applies these pairs at
	# command-line precedence, above even the fixture's own .git/config. So
	# GIT_CONFIG_GLOBAL=/dev/null does not stop them and neither does a local
	# pin -- only unsetting GIT_CONFIG_COUNT does.
	#
	# The key planted here is core.excludesfile, NOT the core.attributesFile
	# used in the config file above, and the difference is what makes this
	# case worth anything. golden_corpus_git passes
	# `-c core.attributesFile=/dev/null`, which is also command-line
	# precedence, so an injected core.attributesFile loses to it and the case
	# would pass with the GIT_CONFIG_COUNT unset deleted -- a test that proves
	# nothing. Nothing overrides an injected core.excludesfile, which drops
	# Dockerfile from the staged tree and moves HEAD off its pin. Measured on
	# git 2.55.0: injected core.excludesfile beats a local
	# `core.excludesfile /dev/null`.
	export GIT_CONFIG_COUNT=1
	export GIT_CONFIG_KEY_0="core.excludesfile"
	export GIT_CONFIG_VALUE_0="${stage_case_git_excludes}"
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
