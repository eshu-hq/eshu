#!/usr/bin/env bash
# test-pre-pr-docs-fastpath.sh — table-driven test suite for the pre-pr
# documentation-only fast-path classifier (#5721).
#
# Sources the real classifier (scripts/lib/pre-pr-docs-fastpath.sh) -- a test
# that re-implements or text-extracts the classifier proves nothing about it.
#
# Two layers, because the classifier has two ways to be wrong:
#
#   1. The ALLOWLIST table: synthetic path sets resolved against a fixture tree
#      built in a temp directory, so a "this path exists" case and a "this path
#      was deleted" case are genuinely different inputs. No Go toolchain and no
#      network.
#   2. The GIT BOUNDARY: pre_pr_resolve_lane_base against throwaway
#      repositories, including the shallow-clone shape where origin/main shares
#      no merge base with HEAD. The first version of this suite never crossed
#      that boundary, so it proved the allowlist and nothing about the input the
#      allowlist is fed -- which is where both severe defects lived.
#
# Run with:
#   bash scripts/lib/test-pre-pr-docs-fastpath.sh
set -uo pipefail

# The cases below build throwaway repositories with `git init` and `git commit`,
# and git reads the developer's ~/.gitconfig in every one of them. A global
# `commit.gpgsign = true` makes those commits ask for a signing key they will
# not get; a global `core.hooksPath` pointing at a rejecting pre-commit hook
# stops them outright. Either one reds this suite, and `make pre-pr` runs it on
# every run and fails the run when it reds -- so a contributor who signs their
# commits would be unable to push anything, and the summary line would blame the
# classifier rather than their git config. Pin all three knobs: GIT_CONFIG_GLOBAL
# and GIT_CONFIG_SYSTEM name the files, and GIT_CONFIG_NOSYSTEM covers the
# git versions that honour it instead. Do not remove this -- the fixtures are
# about the classifier, not about how anyone has configured git.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
lib="${repo_root}/scripts/lib/pre-pr-docs-fastpath.sh"

if [[ ! -f "${lib}" ]]; then
	echo "test-pre-pr-docs-fastpath: missing library at ${lib}" >&2
	exit 1
fi
# shellcheck source=/dev/null
source "${lib}"

failures=0
cases_run=0
scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT

fail() {
	echo "FAIL $*" >&2
	failures=$((failures + 1))
}

# ─── Fixture tree ─────────────────────────────────────────────────────────
# The classifier resolves path existence against PRE_PR_FASTPATH_ROOT, so every
# path a case expects to reach the allowlist must exist here. A FULL-expected
# case whose file is MISSING would pass for the wrong reason (absence forces
# full on its own) and would let an allowlist-widening bug survive, so the
# fail-closed cases below get real files too.
fixture_root="${scratch}/fixture"
make_fixture() {
	local p
	for p in "$@"; do
		mkdir -p "${fixture_root}/$(dirname "${p}")"
		: >"${fixture_root}/${p}"
	done
}
make_fixture \
	docs/public/reference/local-testing.md \
	docs/public/architecture.md \
	README.md \
	CLAUDE.md \
	AGENTS.md \
	specs/capability-matrix.v1.yaml \
	specs/capability-matrix/ask.v1.yaml \
	go/internal/capabilitycatalog/data/catalog.generated.json \
	go/internal/capabilitycatalog/data/surface-inventory.generated.json \
	go/internal/capabilitycatalog/data/loader-config.json \
	go/internal/capabilitycatalog/data/loader.go \
	go/internal/capabilitycatalog/data/nested/deep.generated.json \
	docsEVIL/manifest.yaml \
	docsEVIL/main.go \
	docs/examples/main.go \
	docs/examples/go.mod \
	specs/capability-matrix-legacy.v1.yaml \
	sdk/go/factschema/kinds.yaml \
	sdk/go/collector/go.mod \
	testdata/golden/e2e-20repo-snapshot.json \
	go/internal/query/handler.go \
	go/internal/query/README.md \
	go.mod
# shellcheck disable=SC2034  # read by the sourced classifier, not by this file.
PRE_PR_FASTPATH_ROOT="${fixture_root}"

# assert_lane <case-name> <expected-lane: fast|full> <path...>
# Classifies the given path set with a TRUSTED status (the git commands that
# produced it exited 0) and checks the resulting lane.
assert_lane() {
	local name="$1" expected="$2"
	shift 2
	cases_run=$((cases_run + 1))
	pre_pr_classify_docs_fastpath ok "$@"
	if [[ "${PRE_PR_FASTPATH_LANE}" != "${expected}" ]]; then
		fail "${name}: want lane=${expected}, got lane=${PRE_PR_FASTPATH_LANE} (paths: $*)"
		return
	fi
	echo "PASS ${name}"
}

# assert_lane_untrusted <case-name> <status> <path...>
# Classifies with a NON-"ok" status and requires the full lane plus a
# non-empty operator-facing reason.
assert_lane_untrusted() {
	local name="$1" status="$2"
	shift 2
	cases_run=$((cases_run + 1))
	pre_pr_classify_docs_fastpath "${status}" "$@"
	if [[ "${PRE_PR_FASTPATH_LANE}" != "full" ]]; then
		fail "${name}: want lane=full for status '${status}', got ${PRE_PR_FASTPATH_LANE}"
		return
	fi
	if [[ -z "${PRE_PR_FASTPATH_FORCED_FULL_REASON}" ]]; then
		fail "${name}: lane=full but no reason recorded for status '${status}'"
		return
	fi
	echo "PASS ${name}"
}

# assert_triggers_are <case-name> <expected-triggers-CSV> <path...>
# Checks the EXACT trigger set, in order. Requirement #11 says pre-pr prints
# the offending pathS: a classifier that stops at the first one still returns
# lane=full, so only an exact-set assertion catches it.
assert_triggers_are() {
	local name="$1" want="$2"
	shift 2
	cases_run=$((cases_run + 1))
	pre_pr_classify_docs_fastpath ok "$@"
	local got=""
	local t
	for t in "${PRE_PR_FASTPATH_TRIGGERS[@]:-}"; do
		[[ -n "${t}" ]] || continue
		got="${got:+${got},}${t}"
	done
	if [[ "${got}" != "${want}" ]]; then
		fail "${name}: want triggers '${want}', got '${got}'"
		return
	fi
	echo "PASS ${name}"
}

# ─── Table: paths that MUST classify fast ─────────────────────────────────

assert_lane "docs_nested_page_is_fast" fast "docs/public/reference/local-testing.md"
assert_lane "root_readme_is_fast" fast "README.md"
assert_lane "root_claude_md_is_fast" fast "CLAUDE.md"
assert_lane "root_agents_md_is_fast" fast "AGENTS.md"
assert_lane "capability_matrix_root_is_fast" fast "specs/capability-matrix.v1.yaml"
assert_lane "capability_matrix_row_is_fast" fast "specs/capability-matrix/ask.v1.yaml"
assert_lane "capabilitycatalog_generated_json_is_fast" fast \
	"go/internal/capabilitycatalog/data/catalog.generated.json"
assert_lane "multiple_docs_and_root_md_is_fast" fast \
	"docs/public/architecture.md" "README.md" "specs/capability-matrix.v1.yaml"

# A git diff that SUCCEEDED and listed nothing means there is nothing to build.
# This is the only empty-input shape that may be fast; the failed-diff shape is
# asserted full further down. The two used to be one case, which pinned the
# single most dangerous input in the system as intended behaviour.
assert_lane "successful_empty_changeset_is_fast" fast

# ─── Table: paths that MUST classify full (fail-closed) ───────────────────

assert_lane "go_source_file_is_full" full "go/internal/query/handler.go"
assert_lane "generated_openapi_go_is_full" full "go/internal/query/openapi.gen.go"
assert_lane "go_mod_is_full" full "go.mod"
assert_lane "go_sum_is_full" full "go.sum"
assert_lane "makefile_is_full" full "Makefile"
assert_lane "dockerfile_is_full" full "Dockerfile"
assert_lane "dockerfile_suffixed_is_full" full "Dockerfile.runtime"
assert_lane "scripts_dir_is_full" full "scripts/dev/pre-pr.sh"
assert_lane "github_workflow_is_full" full ".github/workflows/test.yml"
assert_lane "non_matrix_spec_yaml_is_full" full "specs/fact-kind-registry.v1.yaml"
assert_lane "nested_markdown_not_root_is_full" full "go/internal/query/README.md"
assert_lane "one_go_file_among_docs_is_full" full \
	"docs/public/architecture.md" "go/internal/query/handler.go"
assert_lane "unrecognized_novel_path_is_full" full "tools/newthing/manifest.xyz"
assert_lane "empty_path_argument_is_full" full ""

# Deletion and rename. `git diff --name-only` prints a removed path exactly like
# a modified one, and the go:embed directives in
# go/internal/capabilitycatalog/{load,surfaces_load}.go name the generated JSON
# by pattern -- deleting it fails `go build` with "no matching files found"
# while its CONTENT genuinely cannot. Allowlisted-but-absent must be full.
assert_lane "deleted_generated_json_is_full" full \
	"go/internal/capabilitycatalog/data/removed-catalog.generated.json"
assert_lane "deleted_docs_page_is_full" full "docs/public/reference/removed-page.md"
assert_lane "deleted_root_md_is_full" full "MOVED.md"

# Allowlist-boundary cases. Each of these is one character away from an
# allowlisted pattern, and each file below EXISTS in the fixture tree, so a
# widened pattern would classify it fast rather than being saved by absence.
assert_lane "capabilitycatalog_data_non_generated_is_full" full \
	"go/internal/capabilitycatalog/data/loader-config.json"
assert_lane "capabilitycatalog_data_go_file_is_full" full \
	"go/internal/capabilitycatalog/data/loader.go"
assert_lane "capabilitycatalog_data_nested_generated_is_full" full \
	"go/internal/capabilitycatalog/data/nested/deep.generated.json"
assert_lane "docs_prefixed_sibling_dir_is_full" full "docsEVIL/manifest.yaml"
assert_lane "docs_prefixed_sibling_go_is_full" full "docsEVIL/main.go"
assert_lane "capability_matrix_prefixed_sibling_is_full" full \
	"specs/capability-matrix-legacy.v1.yaml"
assert_lane "sdk_path_is_full" full "sdk/go/factschema/kinds.yaml"
assert_lane "sdk_go_mod_is_full" full "sdk/go/collector/go.mod"
assert_lane "testdata_path_is_full" full "testdata/golden/e2e-20repo-snapshot.json"

# docs/** is a prefix allowlist, so a Go file parked under it would otherwise
# ride the fast path and skip the build it can break. There are none today
# (`rg --files docs -g '*.go'` returns 0 files); these two pin the invariant so
# adding one cannot silently widen the fast path.
assert_lane "go_file_under_docs_is_full" full "docs/examples/main.go"
assert_lane "go_mod_under_docs_is_full" full "docs/examples/go.mod"

# ─── Table: an untrustworthy path list can never be fast ──────────────────

assert_lane_untrusted "failed_diff_empty_changeset_is_full" \
	"a git diff against origin/main failed, so the changed-path list is incomplete"
assert_lane_untrusted "failed_diff_with_docs_paths_is_full" \
	"a git diff against origin/main failed, so the changed-path list is incomplete" \
	"docs/public/architecture.md"
assert_lane_untrusted "head_tilde_one_base_is_full" \
	"origin/main does not resolve, so the base fell back to HEAD~1" \
	"docs/public/architecture.md"
assert_lane_untrusted "missing_status_is_full" ""

# ─── Trigger reporting ────────────────────────────────────────────────────

assert_triggers_are "trigger_names_the_go_file" "go/internal/query/handler.go" \
	"docs/public/architecture.md" "go/internal/query/handler.go"
assert_triggers_are "trigger_names_go_mod" "go.mod" "README.md" "go.mod"
assert_triggers_are "trigger_names_unrecognized_path" "tools/newthing/manifest.xyz" \
	"tools/newthing/manifest.xyz"
# ALL offending paths, not just the first: the operator has to see every path
# that cost them the full lane.
assert_triggers_are "triggers_report_every_offending_path" \
	"go/internal/query/handler.go,Makefile,tools/newthing/manifest.xyz" \
	"docs/public/architecture.md" "go/internal/query/handler.go" \
	"README.md" "Makefile" "tools/newthing/manifest.xyz"

# Fast classification must report zero triggers and no forced-full reason.
cases_run=$((cases_run + 1))
pre_pr_classify_docs_fastpath ok "docs/public/architecture.md" "README.md"
if [[ ${#PRE_PR_FASTPATH_TRIGGERS[@]} -ne 0 ]]; then
	fail "fast_lane_has_no_triggers: expected zero triggers, got: ${PRE_PR_FASTPATH_TRIGGERS[*]}"
elif [[ -n "${PRE_PR_FASTPATH_FORCED_FULL_REASON}" ]]; then
	fail "fast_lane_has_no_triggers: unexpected forced-full reason '${PRE_PR_FASTPATH_FORCED_FULL_REASON}'"
else
	echo "PASS fast_lane_has_no_triggers"
fi

# ─── Git boundary: pre_pr_resolve_lane_base ───────────────────────────────

# new_repo <name> — throwaway repository with one commit on `main`.
new_repo() {
	local dir="${scratch}/$1"
	mkdir -p "${dir}"
	git init -q -b main "${dir}"
	git -C "${dir}" config user.email test@example.invalid
	git -C "${dir}" config user.name test
	: >"${dir}/README.md"
	git -C "${dir}" add README.md
	git -C "${dir}" commit -qm "first"
	printf '%s\n' "${dir}"
}

# assert_base <case-name> <repo> <expected-base> <expected-status: ok|not-ok>
assert_base() {
	local name="$1" dir="$2" want_base="$3" want_status="$4"
	cases_run=$((cases_run + 1))
	pre_pr_resolve_lane_base "${dir}"
	if [[ "${PRE_PR_FASTPATH_BASE}" != "${want_base}" ]]; then
		fail "${name}: want base=${want_base}, got ${PRE_PR_FASTPATH_BASE}"
		return
	fi
	if [[ "${want_status}" == "ok" && "${PRE_PR_FASTPATH_BASE_STATUS}" != "ok" ]]; then
		fail "${name}: want status=ok, got '${PRE_PR_FASTPATH_BASE_STATUS}'"
		return
	fi
	if [[ "${want_status}" != "ok" && "${PRE_PR_FASTPATH_BASE_STATUS}" == "ok" ]]; then
		fail "${name}: want a non-ok status, got ok"
		return
	fi
	echo "PASS ${name}"
}

# No origin/main at all: the base falls back to HEAD~1, which shows only the
# last commit of a multi-commit branch. That is fine for "which packages do I
# test" and useless for "may I skip the Go lanes", so the status must not be ok.
repo_no_origin="$(new_repo repo-no-origin)"
assert_base "no_origin_main_falls_back_and_is_not_ok" "${repo_no_origin}" "HEAD~1" "not-ok"

# origin/main resolves and shares history with HEAD: the only trustworthy shape.
repo_ok="$(new_repo repo-ok)"
git -C "${repo_ok}" update-ref refs/remotes/origin/main HEAD
git -C "${repo_ok}" checkout -q -b feature
: >"${repo_ok}/docs.md"
git -C "${repo_ok}" add docs.md
git -C "${repo_ok}" commit -qm "second"
assert_base "resolved_origin_main_with_merge_base_is_ok" "${repo_ok}" "origin/main" "ok"

# origin/main resolves but shares NO merge base with HEAD -- the shallow-clone
# shape. `git diff origin/main...HEAD` exits 128 here, printing nothing on
# stdout, which is exactly how an empty path list used to become a FAST verdict.
repo_orphan="$(new_repo repo-orphan)"
git -C "${repo_orphan}" update-ref refs/remotes/origin/main HEAD
git -C "${repo_orphan}" checkout -q --orphan detached
git -C "${repo_orphan}" rm -q -rf . >/dev/null 2>&1
mkdir -p "${repo_orphan}/go/internal/q"
printf 'package q\n' >"${repo_orphan}/go/internal/q/handler.go"
git -C "${repo_orphan}" add go/internal/q/handler.go
git -C "${repo_orphan}" commit -qm "orphan"
assert_base "no_merge_base_is_not_ok" "${repo_orphan}" "origin/main" "not-ok"

# And prove the failing diff really does exit non-zero with an empty stdout, so
# the status above is guarding a real condition rather than a supposed one.
cases_run=$((cases_run + 1))
orphan_out="$(git -C "${repo_orphan}" diff --name-only 'origin/main...HEAD' 2>/dev/null)"
orphan_rc=$?
if [[ ${orphan_rc} -eq 0 || -n "${orphan_out}" ]]; then
	fail "no_merge_base_diff_fails_empty: want non-zero exit and empty stdout, got rc=${orphan_rc} out='${orphan_out}'"
else
	echo "PASS no_merge_base_diff_fails_empty"
fi

echo ""
echo "test-pre-pr-docs-fastpath: ${cases_run} case(s) run, ${failures} failure(s)"
if [[ "${failures}" -ne 0 ]]; then
	exit 1
fi
echo "test-pre-pr-docs-fastpath: all tests passed"
