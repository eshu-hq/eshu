#!/usr/bin/env bash
# Functional cases for the deterministic Git fixture staged by the B-7 gate.
# This file is sourced by scripts/test-verify-golden-corpus-gate.sh after its
# matcher helpers and shared paths are initialized.

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

stage_case_repo="${stage_case_corpus}/container-ci-lineage"
[[ -d "${stage_case_repo}/.git" ]] ||
	fail "container-ci-lineage staging must create deterministic Git history"
[[ -z "$(git -C "${stage_case_repo}" status --porcelain)" ]] ||
	fail "container-ci-lineage staged Git history must include its complete working tree"

stage_case_head="$(git -C "${stage_case_repo}" rev-parse HEAD)"
[[ "${#stage_case_head}" -eq 40 ]] ||
	fail "container-ci-lineage staged HEAD must use SHA-1, got ${#stage_case_head} characters"
stage_case_run_commit="$({
	jq -r '
    .scopes[]
    | select(.scope_id == "ci_cd_run:github_actions:acme:container-ci-lineage")
    | .facts[]
    | select(.fact_kind == "ci.run" and .payload.run_id == "9100")
    | .payload.commit_sha
  ' "${repo_root}/testdata/cassettes/cicdrun/supply-chain-demo.json"
})"
[[ "${stage_case_head}" = "${stage_case_run_commit}" ]] ||
	fail "container-ci-lineage staged HEAD ${stage_case_head} must match run 9100 commit ${stage_case_run_commit}"

stage_case_input_repo="${stage_case_corpus}/github_actions_workflows"
[[ -d "${stage_case_input_repo}/.git" ]] ||
	fail "github_actions_workflows staging must create deterministic Git history"
[[ -z "$(git -C "${stage_case_input_repo}" status --porcelain)" ]] ||
	fail "github_actions_workflows staged Git history must include its complete working tree"

stage_case_input_head="$(git -C "${stage_case_input_repo}" rev-parse HEAD)"
[[ "${#stage_case_input_head}" -eq 40 ]] ||
	fail "github_actions_workflows staged HEAD must use SHA-1, got ${#stage_case_input_head} characters"
stage_case_input_run_commit="$({
	jq -r '
    .scopes[]
    | select(.scope_id == "ci_cd_run:github_actions:acme:github_actions_workflows")
    | .facts[]
    | select(.fact_kind == "ci.run" and .payload.run_id == "9200")
    | .payload.commit_sha
  ' "${repo_root}/testdata/cassettes/cicdrun/supply-chain-demo.json"
})"
[[ "${stage_case_input_head}" = "${stage_case_input_run_commit}" ]] ||
	fail "github_actions_workflows staged HEAD ${stage_case_input_head} must match run 9200 commit ${stage_case_input_run_commit}"

stage_case_deployable_repo="${stage_case_corpus}/deployable-config"
stage_case_deployable_head="$(git -C "${stage_case_deployable_repo}" rev-parse HEAD)"
[[ "${#stage_case_deployable_head}" -eq 40 ]] ||
	fail "deployable-config staged HEAD must use SHA-1, got ${#stage_case_deployable_head} characters"

rm -rf "${stage_case_dir}"
stage_cases_completed=1
