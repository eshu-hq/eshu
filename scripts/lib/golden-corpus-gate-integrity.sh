#!/usr/bin/env bash
# golden-corpus-gate-integrity.sh — staged-fixture pin proof and required-tool
# preflight shared by the live B-7 gate (scripts/verify-golden-corpus-gate.sh)
# and its Docker-free self-test (scripts/test-verify-golden-corpus-gate.sh, via
# golden-corpus-gate-integrity-cases.sh and golden-corpus-stage-cases.sh).
#
# Both proofs exist for the same reason (#6401): a defect the gate silently
# tolerated turned a correctly-attributable failure into a misleading one
# ~140s into a live run instead of an immediate, correctly-named one.
#
#   - A git-ignored file in a fixture SOURCE directory (e.g. .DS_Store) used
#     to leak through `cp -R` into the staged copy, which stage.sh then
#     `git add -A`-commits with `core.excludesfile /dev/null` and no local
#     .gitignore -- silently drifting the staged HEAD off the commit_sha the
#     cicdrun cassette pins, demoting a correlation from exact to derived and
#     failing two unrelated-looking assertions well into the run.
#   - A missing required tool (rg, jq, docker) used to be folded into a
#     downstream symptom (e.g. golden-corpus-changed-since.sh's
#     `(old=0, new=0)` precondition message) instead of failing immediately
#     and naming the actual missing binary.
#
# Requires from the caller: repo_root.

# golden_corpus_require_tools dies (via fail_fn, the name of a function taking
# one message argument) naming the first required binary missing from PATH,
# before any of them are actually invoked by the pipeline.
golden_corpus_require_tools() {
	local fail_fn="$1"
	shift
	local tool
	for tool in "$@"; do
		command -v "${tool}" >/dev/null 2>&1 ||
			"${fail_fn}" "required tool not found on PATH: ${tool} (a prerequisite for the golden-corpus gate)"
	done
}

# golden_corpus_require_gate_tools applies the preflight the LIVE gate needs
# for the mode it was actually invoked in. rg and jq are used in both modes.
# docker is required ONLY under compose mode: every docker call in this gate is
# already guarded by use_compose (golden-corpus-host-helpers.sh's pg() and
# golden-corpus-cleanup.sh's teardown), so demanding it under --no-compose --
# where CI brings the backends up separately and the host talks to Postgres
# through psql -- would fail a supported mode on a machine that legitimately
# has no docker.
golden_corpus_require_gate_tools() {
	local fail_fn="$1" use_compose="$2"
	golden_corpus_require_tools "${fail_fn}" rg jq
	if [[ "${use_compose}" -eq 1 ]]; then
		golden_corpus_require_tools "${fail_fn}" docker
	fi
	return 0
}

# golden_corpus_pinned_commit_sha prints the commit_sha the cicdrun cassette
# pins for one ci.run scope_id + run_id (e.g. run 9100 of
# ci_cd_run:github_actions:acme:container-ci-lineage).
golden_corpus_pinned_commit_sha() {
	local scope_id="$1" run_id="$2"
	jq -r --arg scope_id "${scope_id}" --arg run_id "${run_id}" '
		.scopes[]
		| select(.scope_id == $scope_id)
		| .facts[]
		| select(.fact_kind == "ci.run" and .payload.run_id == $run_id)
		| .payload.commit_sha
	' "${repo_root}/testdata/cassettes/cicdrun/supply-chain-demo.json"
}

# golden_corpus_assert_staged_pin dies (via fail_fn) unless the staged
# fixture's HEAD equals the cassette's pinned commit for run_id. The failure
# names the fixture, both SHAs, the usual cause, and lists the staged tree so
# the reader sees the culprit directly instead of re-deriving it from an
# unrelated failure much later in the run.
golden_corpus_assert_staged_pin() {
	local fixture="$1" staged_repo="$2" scope_id="$3" run_id="$4" fail_fn="$5"
	local staged_head expected
	staged_head="$(git -C "${staged_repo}" rev-parse HEAD 2>/dev/null)" ||
		"${fail_fn}" "${fixture}: staged repo has no HEAD at ${staged_repo}"
	expected="$(golden_corpus_pinned_commit_sha "${scope_id}" "${run_id}")"
	[[ -n "${expected}" ]] ||
		"${fail_fn}" "${fixture}: no pinned commit_sha for run ${run_id} (scope ${scope_id}) in testdata/cassettes/cicdrun/supply-chain-demo.json"
	# `[[ ... ]] && return 0` as a bare statement would trip errexit on the
	# false branch (the whole list's own exit status is nonzero), aborting the
	# caller silently before any diagnostic below ever runs -- the same
	# masked-assignment class of pitfall #5837's P2-1 fix documents elsewhere
	# in this gate's own test harness (golden-corpus-mirror-matcher.sh). An
	# `if` is exempt from errexit on either branch.
	if [[ "${staged_head}" == "${expected}" ]]; then
		return 0
	fi
	{
		printf 'golden-corpus-gate-integrity: %s staged HEAD %s does not match the run %s pinned commit %s\n' \
			"${fixture}" "${staged_head}" "${run_id}" "${expected}"
		printf 'golden-corpus-gate-integrity: usual cause is an extra file staged into the fixture directory (e.g. a git-ignored file such as .DS_Store copied by cp -R)\n'
		printf 'golden-corpus-gate-integrity: offending staged tree entries (git -C %s ls-tree -r HEAD --name-only):\n' "${staged_repo}"
		git -C "${staged_repo}" ls-tree -r HEAD --name-only
	} >&2
	"${fail_fn}" "${fixture} staged HEAD ${staged_head} must match the run ${run_id} pinned commit ${expected}"
}

# golden_corpus_assert_pinned_fixtures asserts both deterministic-history
# fixtures the B-7 gate keys correlation exactness off of: container-ci-lineage
# (run 9100) and github_actions_workflows (run 9200).
golden_corpus_assert_pinned_fixtures() {
	local corpus_dir="$1" fail_fn="$2"
	golden_corpus_assert_staged_pin "container-ci-lineage" "${corpus_dir}/container-ci-lineage" \
		"ci_cd_run:github_actions:acme:container-ci-lineage" "9100" "${fail_fn}"
	golden_corpus_assert_staged_pin "github_actions_workflows" "${corpus_dir}/github_actions_workflows" \
		"ci_cd_run:github_actions:acme:github_actions_workflows" "9200" "${fail_fn}"
}
