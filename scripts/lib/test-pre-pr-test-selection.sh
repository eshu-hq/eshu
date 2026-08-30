#!/usr/bin/env bash
# Behavioural tests for the focused Go test target selector.

set -uo pipefail

suite_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
selection_lib="${suite_root}/scripts/lib/pre-pr-test-selection.sh"

if [[ ! -f "${selection_lib}" ]]; then
	echo "test-pre-pr-test-selection: missing library at ${selection_lib}" >&2
	exit 1
fi
# shellcheck source=/dev/null
source "${selection_lib}"

failures=0
cases_run=0

assert_selection() {
	local name="$1" input="$2" want="$3" got
	cases_run=$((cases_run + 1))
	got="$(printf '%s' "${input}" | pre_pr_select_test_dirs)"
	if [[ "${got}" != "${want}" ]]; then
		echo "FAIL ${name}: want '${want}', got '${got}'" >&2
		failures=$((failures + 1))
		return
	fi
	echo "PASS ${name}"
}

assert_selection \
	"parent_parser_change_selects_tree" \
	$'./internal/parser\n' \
	'./internal/parser/...'

assert_selection \
	"child_parser_change_stays_focused" \
	$'./internal/parser/java\n' \
	'./internal/parser/java'

assert_selection \
	"parent_parser_tree_covers_child_without_duplicate" \
	$'./internal/parser\n./internal/parser/java\n./internal/runtime\n./internal/parser/java\n' \
	$'./internal/parser/...\n./internal/runtime'

assert_selection \
	"parent_parser_tree_wins_when_child_appears_first" \
	$'./internal/parser/java\n./internal/parser\n' \
	'./internal/parser/...'

assert_selection \
	"json_change_selects_the_dbtsql_black_box_engine_tests" \
	$'./internal/parser/json\n' \
	$'./internal/parser/json\n./internal/parser/dbtsql'

assert_selection \
	"cross_package_expansion_does_not_duplicate_an_already_changed_sibling" \
	$'./internal/parser/json\n./internal/parser/dbtsql\n' \
	$'./internal/parser/json\n./internal/parser/dbtsql'

assert_selection \
	"parser_tree_absorbs_the_cross_package_expansion" \
	$'./internal/parser\n./internal/parser/json\n' \
	'./internal/parser/...'

assert_selection \
	"cross_package_expansion_is_keyed_on_the_exact_dir" \
	$'./internal/parser/jsonnet\n' \
	'./internal/parser/jsonnet'

assert_selection \
	"unrelated_packages_are_preserved_and_deduplicated" \
	$'./internal/query\n./internal/runtime\n./internal/query\n' \
	$'./internal/query\n./internal/runtime'

assert_selection \
	"parser_name_prefix_is_not_the_parser_tree" \
	$'./internal/parserish\n' \
	'./internal/parserish'

assert_selection "empty_input_stays_empty" '' ''

echo ""
echo "test-pre-pr-test-selection: ${cases_run} case(s) run, ${failures} failure(s)"
if [[ "${failures}" -ne 0 ]]; then
	exit 1
fi
echo "test-pre-pr-test-selection: all tests passed"
