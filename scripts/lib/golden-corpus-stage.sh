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

stage_deterministic_git_fixture() {
	local fixture_path="$1"
	git -C "${fixture_path}" -c init.defaultBranch=main init >/dev/null 2>&1
	git -C "${fixture_path}" config user.email "gate@eshu.local" >/dev/null 2>&1
	git -C "${fixture_path}" config user.name "Golden Gate" >/dev/null 2>&1
	git -C "${fixture_path}" config commit.gpgsign false >/dev/null 2>&1
	git -C "${fixture_path}" config core.autocrlf false >/dev/null 2>&1
	git -C "${fixture_path}" config core.excludesfile /dev/null >/dev/null 2>&1
	git -C "${fixture_path}" add -A >/dev/null 2>&1
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
		# deployable-config needs a git repo so localGitRefs can discover tags
		# for the B-12 query_shape.http branches endpoint assertion.
		if [[ "${fixture}" = "deployable-config" ]]; then
			git -C "${corpus_dir}/${fixture}" -c init.defaultBranch=main init >/dev/null 2>&1
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
			# gitSubmoduleGitlinkSHA (go/internal/collector/git_submodule_pinned_sha.go)
			# reads the pin from the committed tree via `git ls-tree HEAD --
			# <path>`, never the working directory, so no submodule checkout is
			# needed for the pin to resolve.
			printf '[submodule "vendor/deployable-source"]\n\tpath = vendor/deployable-source\n\turl = https://github.com/acme/deployable-source.git\n' \
				>"${corpus_dir}/${fixture}/.gitmodules"
			git -C "${corpus_dir}/${fixture}" add -A >/dev/null 2>&1
			git -C "${corpus_dir}/${fixture}" update-index --add --cacheinfo \
				160000,5420542054205420542054205420542054205420,vendor/deployable-source >/dev/null 2>&1
			git -C "${corpus_dir}/${fixture}" commit -m "initial" >/dev/null 2>&1
			# Annotated tag for peeled-SHA coverage.
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
