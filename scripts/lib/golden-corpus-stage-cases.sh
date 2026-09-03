#!/usr/bin/env bash
# Functional cases for the deterministic Git fixture staged by the B-7 gate.
# This file is sourced by scripts/test-verify-golden-corpus-gate.sh after its
# matcher helpers and shared paths are initialized.
#
# The staged-pin comparison itself (#6401) lives in one place,
# golden_corpus_assert_staged_pin (scripts/lib/golden-corpus-gate-integrity.sh),
# shared with the live gate so this self-test and the live gate can never
# silently diverge on what "matches the pin" means.

# Every git command that reaches a fixture repository must route through
# golden_corpus_git (scripts/lib/golden-corpus-git.sh). A bare one reintroduces
# the developer's global configuration and environment, and it would be
# invisible to every case below: those stage under a PLANTED hostile config, so
# a call that escapes the planting still produces the right SHA on a machine
# whose real config is empty -- every CI runner, and most laptops. The failure
# appears only for the developer who has the offending setting, as fixture drift
# in a checkout that has none. This is a source check, not a behavior check, on
# purpose: it is the only assertion here that can fail on a machine where the
# behavior looks fine.
#
# The allowlist is of SAFE calls, not of dangerous ones. Enumerating the
# dangerous ones is the same mistake as enumerating the dangerous config keys --
# the defect this whole change exists to remove -- and it measurably failed
# twice here: a verb list of init/add/commit/tag/update-index missed reset, rm,
# checkout, update-ref, symbolic-ref, apply and cherry-pick, and a
# command-position anchor without the prefix run below missed every env-prefixed
# call, which is the shape of every commit site in these files.
#
# Safe means "cannot be perturbed into reporting the wrong thing": rev-parse,
# rev-list, cat-file and ls-tree read committed objects and refs, and
# `config --file` writes to an explicit file rather than into a fixture.
#
# status, diff, check-ignore and ls-files are deliberately NOT safe. They
# compare the working tree against the index or consult exclude rules, so a
# clean filter or an excludesfile changes their OUTPUT while the commit is
# untouched. Measured on git 2.55.0 under a hostile core.attributesFile: bare
# `git status --short` on a clean fixture prints ` M catalog-info.yaml`, because
# the index blob was written filter-free while the worktree comparison applies
# the filter. That one difference aborted golden_service_changed_since_mutate_owner
# with "mutation touched an unexpected path", so treating reads as automatically
# safe was wrong, not merely imprecise.
#
# golden-corpus-git.sh is excluded from the scan: it DEFINES the wrapper and is
# the one place a bare git call belongs.
stage_case_sq="'"
stage_case_git_pre="((command|exec|env|time|then|else|elif|do|if|while|until)[[:space:]]+|![[:space:]]*|[A-Za-z_][A-Za-z0-9_]*=(\"[^\"]*\"|${stage_case_sq}[^${stage_case_sq}]*${stage_case_sq}|[^[:space:]]*)[[:space:]]+)*"
stage_case_git_cmdpos="(^[[:space:]]*|\\\$\\([[:space:]]*|\`[[:space:]]*|[(){};|&][[:space:]]*)${stage_case_git_pre}git[[:space:]]"
stage_case_git_readonly='git([^|;&]*[[:space:]])?(rev-parse|rev-list|cat-file|ls-tree)([[:space:]]|$)|git([^|;&]*[[:space:]])?config[[:space:]]+--file'

# Scan the given paths and print every offending line. rg exits 0 with matches,
# 1 with none, and >1 on a real error; collapsing that into "no matches" is
# exactly how a guard stops guarding without anyone noticing, so a bad exit is
# a failure rather than a pass.
golden_corpus_scan_bare_git() {
	local out rc
	out="$(rg -n --no-filename "${stage_case_git_cmdpos}" "$@" 2>/dev/null)"
	rc=$?
	[[ "${rc}" -le 1 ]] ||
		fail "bare-git guard could not scan $* (rg exit ${rc}); a scan that fails to run is not a scan that found nothing"
	[[ -n "${out}" ]] || return 0
	printf '%s\n' "${out}" | rg -v '^[0-9]+:[[:space:]]*#' | rg -v "${stage_case_git_readonly}" || true
}

# Prove the guard can fire before trusting that it did not.
#
# Without this, a wrong regex, a glob matching no files, or an unset repo_root
# all produce silence, and silence is what a passing guard looks like. Each line
# below is a real shape a future author writes; the env-prefixed one is the
# shape of every commit site in these files, and it evaded an earlier version of
# this guard.
stage_case_guard_probe="$(mktemp -t golden-corpus-guard-probe.XXXXXX)"
# The probe lines are BUILT rather than written literally, because the real scan
# below globs *golden-corpus*.sh and this file matches that glob: a literal
# `git -C ...` example here is found by the scan it is meant to test, and the
# suite fails on its own sample data. Substituting the command name keeps the
# examples out of the scanned text.
stage_case_guard_g="git"
{
	printf '\t%s -C "$r" commit -m x\n' "${stage_case_guard_g}"
	printf '\tGIT_AUTHOR_DATE="2026-08-04T12:00:00Z" %s -C "$r" commit -m initial\n' "${stage_case_guard_g}"
	printf '\t%s -C "$r" \\\n' "${stage_case_guard_g}"
	printf '\t%s --git-dir="$d" add -A\n' "${stage_case_guard_g}"
	printf '\t%s -c '"'"'user.name=A B'"'"' -C "$r" commit -m x\n' "${stage_case_guard_g}"
	printf '\tcommand %s -C "$r" add -A\n' "${stage_case_guard_g}"
	printf '\texec %s -C "$r" reset --hard\n' "${stage_case_guard_g}"
	printf '\tenv FOO=1 %s -C "$r" add -A\n' "${stage_case_guard_g}"
	printf '\tout="$(%s -C "$r" update-ref refs/heads/m HEAD)"\n' "${stage_case_guard_g}"
	printf '\t( %s -C "$r" checkout -- . )\n' "${stage_case_guard_g}"
	printf '\tif %s -C "$r" apply p; then echo y; fi\n' "${stage_case_guard_g}"
	printf '\tthen %s -C "$r" rm --cached f\n' "${stage_case_guard_g}"
	printf '\tdo %s -C "$r" clean -fd\n' "${stage_case_guard_g}"
	printf '\ttrue && %s -C "$r" stash\n' "${stage_case_guard_g}"
	printf '\ttrue ; %s -C "$r" symbolic-ref HEAD refs/heads/m\n' "${stage_case_guard_g}"
	printf '\t%s -C "$r" status --short\n' "${stage_case_guard_g}"
} >"${stage_case_guard_probe}"
stage_case_guard_expected="$(grep -c . "${stage_case_guard_probe}")"
stage_case_guard_found="$(golden_corpus_scan_bare_git "${stage_case_guard_probe}" | grep -c . || true)"
[[ "${stage_case_guard_found}" == "${stage_case_guard_expected}" ]] ||
	fail "the bare-git guard caught ${stage_case_guard_found} of ${stage_case_guard_expected} planted violations, so it cannot be trusted to report zero in the real scan"
rm -f "${stage_case_guard_probe}"

stage_case_bare_git="$(golden_corpus_scan_bare_git \
	-g '*golden-corpus*.sh' -g '!golden-corpus-git.sh' "${repo_root}/scripts")"
[[ -z "${stage_case_bare_git}" ]] ||
	fail "these golden-corpus git calls reach a repository without routing through golden_corpus_git, so a developer's config or environment can change what gets committed: ${stage_case_bare_git}"

stage_case_dir="$(mktemp -d -t golden-corpus-stage-case.XXXXXX)"
stage_case_corpus="${stage_case_dir}/corpus"
stage_case_git_config="${stage_case_dir}/gitconfig"
stage_case_git_attributes="${stage_case_dir}/gitattributes"
stage_case_git_excludes="${stage_case_dir}/gitexcludes"
mkdir -p "${stage_case_corpus}"
printf 'Dockerfile\n' >"${stage_case_git_excludes}"
# init.defaultObjectFormat is planted as a config knob, and GIT_DEFAULT_HASH is
# exported into each staging subshell below. They select the object format by
# two different routes -- config file and environment -- and golden_corpus_git
# closes both.
#
# The three `[[ "${#...head}" -eq 40 ]]` assertions further down are NOT teeth
# for either one, and saying otherwise here would be the kind of tested-looking
# claim this file keeps having to correct. Measured on git 2.55.0, deleting one
# thing at a time and running the whole suite:
#
#   -u GIT_DEFAULT_HASH removed from the wrapper   -> suite green
#     (`--object-format=sha1` at each init site outranks the variable)
#   --object-format=sha1 removed from stage.sh     -> suite green
#     (the wrapper drops GIT_DEFAULT_HASH, so git falls back to its own
#      default, which is SHA-1)
#
# Each covers for the other, so no single deletion can move a staged HEAD to 64
# characters and no assertion can fire. Removing BOTH aborts the suite before
# any assertion is reached (exit 129, no output), so even that does not
# demonstrate them. Treat the three length checks as belt-and-braces that would
# only ever catch a future change removing both layers at once, not as coverage
# of either line.
#
# The plants stay because they are real hostile inputs the wrapper is supposed
# to neutralize, and because the staged pin assertions immediately below WOULD
# fail on a SHA-256 fixture -- a 64-character HEAD cannot match a 40-character
# cassette pin. That is where the actual protection is measured.
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
	export GIT_DEFAULT_HASH=sha256
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
[[ -z "$(golden_corpus_git -C "${stage_case_repo}" status --porcelain)" ]] ||
	fail "container-ci-lineage staged Git history must include its complete working tree"
stage_case_head="$(git -C "${stage_case_repo}" rev-parse HEAD)"
[[ "${#stage_case_head}" -eq 40 ]] ||
	fail "container-ci-lineage staged HEAD must use SHA-1, got ${#stage_case_head} characters"
golden_corpus_assert_staged_pin "container-ci-lineage" "${stage_case_repo}" \
	"ci_cd_run:github_actions:acme:container-ci-lineage" "9100" fail

stage_case_input_repo="${stage_case_corpus}/github_actions_workflows"
[[ -d "${stage_case_input_repo}/.git" ]] ||
	fail "github_actions_workflows staging must create deterministic Git history"
[[ -z "$(golden_corpus_git -C "${stage_case_input_repo}" status --porcelain)" ]] ||
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
	export GIT_DEFAULT_HASH=sha256
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
