#!/usr/bin/env bash
# test-pre-pr-docs-fastpath.sh — hermetic table-driven test suite for the
# pre-pr documentation-only fast-path classifier (#5721).
#
# Sources the real classifier (scripts/lib/pre-pr-docs-fastpath.sh) -- a
# test that re-implements or text-extracts the classifier proves nothing
# about it. No git, network, or Go toolchain dependency: every case passes an
# explicit synthetic path list straight to pre_pr_classify_docs_fastpath.
#
# Run with:
#   bash scripts/lib/test-pre-pr-docs-fastpath.sh
set -uo pipefail

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

# assert_lane <case-name> <expected-lane: fast|full> <path...>
# Runs the classifier on the given synthetic path set and checks the
# resulting PRE_PR_FASTPATH_LANE matches expected.
assert_lane() {
	local name="$1" expected="$2"
	shift 2
	cases_run=$((cases_run + 1))
	pre_pr_classify_docs_fastpath "$@"
	if [[ "${PRE_PR_FASTPATH_LANE}" != "${expected}" ]]; then
		echo "FAIL ${name}: want lane=${expected}, got lane=${PRE_PR_FASTPATH_LANE} (paths: $*)" >&2
		failures=$((failures + 1))
		return
	fi
	echo "PASS ${name}"
}

# assert_triggers_contain <case-name> <needle-path> <path...>
# Runs the classifier and checks PRE_PR_FASTPATH_TRIGGERS names needle-path,
# so a full classification can be tied to the SPECIFIC offending path rather
# than merely "some path forced full" -- required for the operator-visibility
# acceptance criterion (make pre-pr must print which paths triggered full).
assert_triggers_contain() {
	local name="$1" needle="$2"
	shift 2
	cases_run=$((cases_run + 1))
	pre_pr_classify_docs_fastpath "$@"
	local t found=0
	for t in "${PRE_PR_FASTPATH_TRIGGERS[@]:-}"; do
		[[ "${t}" == "${needle}" ]] && found=1 && break
	done
	if [[ "${found}" -ne 1 ]]; then
		echo "FAIL ${name}: expected PRE_PR_FASTPATH_TRIGGERS to contain '${needle}', got: ${PRE_PR_FASTPATH_TRIGGERS[*]:-<empty>}" >&2
		failures=$((failures + 1))
		return
	fi
	echo "PASS ${name}"
}

# ─── Table: paths that MUST classify fast ─────────────────────────────────

assert_lane "docs_nested_page_is_fast"      fast "docs/public/reference/local-testing.md"
assert_lane "root_readme_is_fast"           fast "README.md"
assert_lane "root_claude_md_is_fast"        fast "CLAUDE.md"
assert_lane "root_agents_md_is_fast"        fast "AGENTS.md"
assert_lane "capability_matrix_root_is_fast" fast "specs/capability-matrix.v1.yaml"
assert_lane "capability_matrix_row_is_fast" fast "specs/capability-matrix/ask.v1.yaml"
assert_lane "capabilitycatalog_generated_json_is_fast" fast \
	"go/internal/capabilitycatalog/data/catalog.generated.json"
assert_lane "multiple_docs_and_root_md_is_fast" fast \
	"docs/public/architecture.md" "README.md" "specs/capability-matrix.v1.yaml"
assert_lane "empty_changeset_is_fast" fast

# ─── Table: paths that MUST classify full (fail-closed) ───────────────────

assert_lane "go_source_file_is_full"        full "go/internal/query/handler.go"
assert_lane "generated_openapi_go_is_full"  full "go/internal/query/openapi.gen.go"
assert_lane "go_mod_is_full"                full "go.mod"
assert_lane "go_sum_is_full"                full "go.sum"
assert_lane "makefile_is_full"              full "Makefile"
assert_lane "dockerfile_is_full"            full "Dockerfile"
assert_lane "dockerfile_suffixed_is_full"   full "Dockerfile.runtime"
assert_lane "scripts_dir_is_full"           full "scripts/dev/pre-pr.sh"
assert_lane "github_workflow_is_full"       full ".github/workflows/test.yml"
assert_lane "non_matrix_spec_yaml_is_full"  full "specs/fact-kind-registry.v1.yaml"
assert_lane "nested_markdown_not_root_is_full" full "go/internal/query/README.md"
assert_lane "one_go_file_among_docs_is_full" full \
	"docs/public/architecture.md" "go/internal/query/handler.go"
assert_lane "unrecognized_novel_path_is_full" full "tools/newthing/manifest.xyz"

# Ties the full classification of a real code file to the exact reported
# trigger, not merely "the lane went full" -- pins the operator-visibility
# contract (pre-pr must print WHICH paths forced full).
assert_triggers_contain "trigger_names_the_go_file" "go/internal/query/handler.go" \
	"docs/public/architecture.md" "go/internal/query/handler.go"
assert_triggers_contain "trigger_names_go_mod" "go.mod" \
	"README.md" "go.mod"
assert_triggers_contain "trigger_names_unrecognized_path" "tools/newthing/manifest.xyz" \
	"tools/newthing/manifest.xyz"

# Fast classification must report zero triggers.
cases_run=$((cases_run + 1))
pre_pr_classify_docs_fastpath "docs/public/architecture.md" "README.md"
if [[ ${#PRE_PR_FASTPATH_TRIGGERS[@]} -ne 0 ]]; then
	echo "FAIL fast_lane_has_no_triggers: expected zero triggers, got: ${PRE_PR_FASTPATH_TRIGGERS[*]}" >&2
	failures=$((failures + 1))
else
	echo "PASS fast_lane_has_no_triggers"
fi

echo ""
echo "test-pre-pr-docs-fastpath: ${cases_run} case(s) run, ${failures} failure(s)"
if [[ "${failures}" -ne 0 ]]; then
	exit 1
fi
echo "test-pre-pr-docs-fastpath: all tests passed"
