#!/usr/bin/env bash
#
# golden-corpus-stage.sh — minimal corpus staging for the golden corpus gate
# orchestrator (scripts/verify-golden-corpus-gate.sh). Extracted into a lib
# chunk so the orchestrator stays under the 500-line cap.
#
# stage_minimal_corpus() copies (never symlinks — the filesystem discovery
# walker does not follow symlinks, so a symlinked fixture collapses to a
# single scope and breaks cross-repo edges) each entry of corpus_fixtures
# (scripts/lib/golden-corpus-fixtures.sh) into corpus_dir, then gives
# selected fixtures real Git history. deployable-config needs it so localGitRefs
# can discover tags for the B-12 query_shape.http branches endpoint assertion
# and the PINS_SUBMODULE (#5420 Phase 5) gitlink coverage. container-ci-lineage
# needs a deterministic HEAD so its static workflow-image facts can join the
# matching CI run at the commit-scoped exact tier. github_actions_workflows uses
# the same mechanism to pin the input-only derived classifier branch (#5830).
#
# Requires (set by the orchestrator before the call): repo_root, corpus_dir,
# corpus_fixtures (array), and the die() function.
#
# stage_strip_ignored_files (#6401) removes every path this repository's own
# .gitignore hides for a fixture's SOURCE directory from the freshly `cp -R`'d
# COPY, before anything commits it. Without this, a git-ignored file such as
# .DS_Store or *.swp survives `cp -R` into the staged copy, and
# stage_deterministic_git_fixture's fresh `git init` (core.excludesfile
# /dev/null, no local .gitignore) then `git add -A`-commits it -- silently
# drifting the staged HEAD off the commit_sha
# testdata/cassettes/cicdrun/supply-chain-demo.json pins for container-ci-lineage
# and github_actions_workflows, which golden_corpus_assert_pinned_fixtures
# (scripts/lib/golden-corpus-gate-integrity.sh) now catches immediately after
# staging. Deliberately narrow: only IGNORED paths are removed. An
# untracked-but-not-ignored file (a fixture addition still in progress) and an
# uncommitted edit to a tracked file both survive -- `git ls-files --others
# --ignored --exclude-standard` reports neither of those categories.
stage_strip_ignored_files() {
	local fixture="$1" dest="$2" src_rel="tests/fixtures/ecosystems/${1}"
	local ignored_list ignored_status ignored_path rel
	ignored_list="$(mktemp -t golden-corpus-ignored.XXXXXX)" ||
		die "stage_strip_ignored_files: failed to create temporary file"
	# -z: git C-quotes pathnames containing non-ASCII bytes, backslashes,
	# newlines or control characters unless asked for NUL-delimited output, and a
	# quoted name would make the rm below target a rendered string rather than the
	# copied file -- leaving exactly the ignored file this function exists to drop.
	# core.excludesfile=/dev/null: --exclude-standard otherwise consults the
	# developer's GLOBAL excludes, which would strip an untracked fixture addition
	# this function promises to keep. Repository ignore rules only.
	# The exit status is captured rather than piped: a process substitution would
	# hide a git failure (an unreadable or damaged index), and the loop would then
	# succeed having removed nothing, silently staging an unfiltered copy.
	# `if !` rather than `cmd; status=$?`: this file is sourced by a script under
	# `set -e`, where a failing git aborts the shell BEFORE the status line runs,
	# so the die below and the temp-file cleanup would never execute and the
	# fail-closed behaviour this block exists for would be inert.
	# `|| ignored_status=$?` rather than a bare command followed by `$?`: this
	# file is sourced under `set -e`, where a failing git aborts the shell before
	# the status line runs, so the die below and the temp-file cleanup would
	# never execute and this fail-closed block would be inert. The `||` form also
	# keeps git's REAL exit code -- inside an `if !` branch `$?` is the
	# negation's status (always 0), which would print "exit 0" in a message whose
	# only job is to report why git failed.
	ignored_status=0
	git -C "${repo_root}" -c core.excludesfile=/dev/null \
		ls-files -z --others --ignored --exclude-standard -- "${src_rel}" \
		>"${ignored_list}" || ignored_status=$?
	if [[ "${ignored_status}" -ne 0 ]]; then
		rm -f "${ignored_list}"
		die "stage_strip_ignored_files: git ls-files failed for ${src_rel} (exit ${ignored_status}); refusing to stage an unfiltered copy"
	fi
	while IFS= read -r -d '' ignored_path; do
		[[ -n "${ignored_path}" ]] || continue
		rel="${ignored_path#"${src_rel}"/}"
		[[ "${rel}" != "${ignored_path}" ]] || continue
		[[ "${rel}" != /* && "${rel}" != *..* ]] || continue
		rm -rf -- "${dest:?stage_strip_ignored_files: dest must not be empty}/${rel}"
	done <"${ignored_list}"
	rm -f "${ignored_list}"
}

stage_deterministic_git_fixture() {
	local fixture_path="$1"
	git -C "${fixture_path}" -c init.defaultBranch=main init --object-format=sha1 >/dev/null 2>&1
	git -C "${fixture_path}" config user.email "gate@eshu.local" >/dev/null 2>&1
	git -C "${fixture_path}" config user.name "Golden Gate" >/dev/null 2>&1
	git -C "${fixture_path}" config commit.gpgsign false >/dev/null 2>&1
	git -C "${fixture_path}" config core.autocrlf false >/dev/null 2>&1
	git -C "${fixture_path}" config core.excludesfile /dev/null >/dev/null 2>&1
	git -C "${fixture_path}" add -A >/dev/null 2>&1
	# Identity is set inline, NOT left to the `git config user.*` above.
	#
	# git prefers GIT_AUTHOR_* and GIT_COMMITTER_* from the environment over
	# config, so a shell that exports any of the four produces a different
	# author or committer -- and therefore a different commit SHA -- from
	# byte-identical fixture content. The tree hash is unchanged; only the
	# commit metadata moves. That presents as fixture drift and is not, which
	# cost one investigation a wrong diagnosis and a wrongly-edited cassette
	# pin before the cause was found. The dates below were always immune for
	# exactly this reason; the identity fields now are too.
	GIT_AUTHOR_NAME="Golden Gate" \
		GIT_AUTHOR_EMAIL="gate@eshu.local" \
		GIT_COMMITTER_NAME="Golden Gate" \
		GIT_COMMITTER_EMAIL="gate@eshu.local" \
		GIT_AUTHOR_DATE="2026-08-04T12:00:00Z" \
		GIT_COMMITTER_DATE="2026-08-04T12:00:00Z" \
		git -C "${fixture_path}" commit -m "initial" >/dev/null 2>&1
}

stage_minimal_corpus() {
	local fixture src
	for fixture in "${corpus_fixtures[@]}"; do
		src="${repo_root}/tests/fixtures/ecosystems/${fixture}"
		[[ -d "${src}" ]] || die "corpus fixture not found: ${src}"
		cp -R "${src}" "${corpus_dir}/${fixture}"
		stage_strip_ignored_files "${fixture}" "${corpus_dir}/${fixture}"
		# deployable-config needs a git repo so localGitRefs can discover tags
		# for the B-12 query_shape.http branches endpoint assertion.
		if [[ "${fixture}" = "deployable-config" ]]; then
			git -C "${corpus_dir}/${fixture}" -c init.defaultBranch=main init --object-format=sha1 >/dev/null 2>&1
			git -C "${corpus_dir}/${fixture}" config user.email "gate@eshu.local" >/dev/null 2>&1
			git -C "${corpus_dir}/${fixture}" config user.name "Golden Gate" >/dev/null 2>&1
			# submodule PINS_SUBMODULE non-vacuous coverage (issue #5420 Phase 5): a
			# pinned submodule SHA is a git gitlink (tree mode 160000), which only
			# exists in a real git tree -- unlike CODEOWNERS, a plain file copy is not
			# enough. Declare the submodule in .gitmodules pointing at the in-corpus
			# deployable-source repository (its URL normalizes, via
			# repositoryidentity with ESHU_GITHUB_ORG=acme below, to the exact same
			# repo_id the filesystem-synthesized "https://github.com/acme/deployable-source.git"
			# remote produces for that fixture's own Repository node -- see
			# go/internal/collector/submodule/resolve.go), then register the gitlink
			# via `git update-index --cacheinfo` rather than a real nested checkout:
			# gitSubmoduleGitlinkSHA (go/internal/collector/gitrepo/gitsubmodule/git_submodule_pinned_sha.go)
			# reads the pin from the committed tree via `git ls-tree HEAD --
			# <path>`, never the working directory, so no submodule checkout is
			# needed for the pin to resolve.
			printf '[submodule "vendor/deployable-source"]\n\tpath = vendor/deployable-source\n\turl = https://github.com/acme/deployable-source.git\n' \
				>"${corpus_dir}/${fixture}/.gitmodules"
			git -C "${corpus_dir}/${fixture}" add -A >/dev/null 2>&1
			git -C "${corpus_dir}/${fixture}" update-index --add --cacheinfo \
				160000,5420542054205420542054205420542054205420,vendor/deployable-source >/dev/null 2>&1
			# Inline identity AND dates for the same reason as
			# stage_deterministic_git_fixture: environment variables
			# outrank the `git config user.*` set above.
			#
			# The dates matter here even though this fixture's HEAD is not
			# pin-asserted today -- only its SHA-1 length is checked. Leaving
			# one commit in the staging path reading GIT_AUTHOR_DATE from the
			# caller's shell would make "staging is hermetic" true of some
			# commits and not others, which is the kind of partial guarantee
			# nobody remembers the shape of when this fixture does get pinned.
			GIT_AUTHOR_NAME="Golden Gate" \
				GIT_AUTHOR_EMAIL="gate@eshu.local" \
				GIT_COMMITTER_NAME="Golden Gate" \
				GIT_COMMITTER_EMAIL="gate@eshu.local" \
				GIT_AUTHOR_DATE="2026-08-04T12:00:00Z" \
				GIT_COMMITTER_DATE="2026-08-04T12:00:00Z" \
				git -C "${corpus_dir}/${fixture}" commit -m "initial" >/dev/null 2>&1
			# Annotated tag for peeled-SHA coverage.
			#
			# A tag object records a tagger name, email and date, all read from
			# GIT_COMMITTER_*, so an annotated tag is no more hermetic than a
			# commit until those are pinned too.
			GIT_COMMITTER_NAME="Golden Gate" \
				GIT_COMMITTER_EMAIL="gate@eshu.local" \
				GIT_COMMITTER_DATE="2026-08-04T12:00:00Z" \
				git -C "${corpus_dir}/${fixture}" tag -a v1.0.0-annotated -m "annotated tag" HEAD >/dev/null 2>&1
			# Lightweight tag.
			git -C "${corpus_dir}/${fixture}" tag lightweight HEAD >/dev/null 2>&1
		fi
		if [[ "${fixture}" = "container-ci-lineage" ]]; then
			stage_deterministic_git_fixture "${corpus_dir}/${fixture}"
		fi
		if [[ "${fixture}" = "github_actions_workflows" ]]; then
			stage_deterministic_git_fixture "${corpus_dir}/${fixture}"
		fi
	done
	printf 'staged: %s\n' "${corpus_fixtures[*]}"
}
