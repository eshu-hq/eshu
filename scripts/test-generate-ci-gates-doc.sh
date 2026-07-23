#!/usr/bin/env bash
#
# test-generate-ci-gates-doc.sh - prove scripts/generate-ci-gates-doc.sh is
# hermetic, idempotent, and produces a CI gates reference that actually
# reflects specs/ci-gates.v1.yaml. Mirrors the test-generate-* shape this
# repo already uses for the operator dashboard generator.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
generator="${repo_root}/scripts/generate-ci-gates-doc.sh"
parser="${repo_root}/scripts/lib/ci-gates-doc-parse.awk"
registry="${repo_root}/specs/ci-gates.v1.yaml"
expected_path="${repo_root}/docs/public/reference/ci-gates.md"

command -v rg >/dev/null 2>&1 || {
	echo "test-generate-ci-gates-doc: rg is required" >&2
	exit 1
}
command -v awk >/dev/null 2>&1 || {
	echo "test-generate-ci-gates-doc: awk is required" >&2
	exit 1
}

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

# Case 1: the generator produces a well-formed markdown table (a header row
# plus at least one gate row, every row with the same column count).
out1="${tmp_root}/run1.md"
ESHU_CI_GATES_DOC_OUTPUT_PATH="${out1}" bash "${generator}" >/dev/null
if [[ -s "${out1}" ]] && rg -q '^\| Gate id \| Name \|' "${out1}"; then
	record_pass "generator produces a markdown table with the expected header"
else
	record_fail "generator output is missing or has no table header"
fi

# Case 2: idempotency — re-running the generator with the same inputs
# produces byte-identical output.
out2="${tmp_root}/run2.md"
ESHU_CI_GATES_DOC_OUTPUT_PATH="${out2}" bash "${generator}" >/dev/null
if cmp -s "${out1}" "${out2}"; then
	record_pass "generator is idempotent on a clean re-run"
else
	record_fail "generator output is not byte-for-byte deterministic across two runs"
fi

# Case 3: no drift — the committed artifact matches a fresh regeneration.
if [[ -f "${expected_path}" ]] && cmp -s "${out1}" "${expected_path}"; then
	record_pass "committed artifact matches a fresh regeneration"
else
	record_fail "committed artifact diverges from a fresh regeneration (run: bash scripts/generate-ci-gates-doc.sh)"
fi

# Case 4: row count matches the registry's own gate count. This is the
# cross-link between "the source of truth changed" and "the artifact kept up".
gate_count="$(rg -c '^  - id: ' "${registry}")"
row_count="$(rg -c '^\| `' "${expected_path}")"
if [[ "${gate_count}" == "${row_count}" ]]; then
	record_pass "table row count (${row_count}) matches the registry gate count"
else
	record_fail "table has ${row_count} rows but the registry defines ${gate_count} gates"
fi

# Case 5: every one of the five Ifá gates named in the testing-story docs
# appears in the table, by exact id.
missing_ifa=0
for gate_id in ifa-contract-layer ifa-determinism ifa-dead-letter-matrix ifa-fault-injection ifa-load-saturation; do
	if ! rg -q "^\| \`${gate_id}\` \|" "${expected_path}"; then
		missing_ifa=$((missing_ifa + 1))
		printf '  missing gate row: %s\n' "${gate_id}" >&2
	fi
done
if [[ "${missing_ifa}" -eq 0 ]]; then
	record_pass "all five Ifá gates have a table row"
else
	record_fail "${missing_ifa} Ifá gate(s) missing a table row"
fi

# Case 6: an alias entry (id-and-reason-only registry record) renders as its
# own distinct row shape instead of a blank/guessed one.
if rg -q '^\| `no-ai-attribution-message` \| \*\(alias' "${expected_path}"; then
	record_pass "an alias registry entry renders as an alias row"
else
	record_fail "no-ai-attribution-message did not render as an alias row"
fi

# Case 7: a CI-only gate (no local: block) falls back to its ci_only_reason
# in the command cell instead of an unexplained em dash.
if rg -q '^\| `reducer-contention` \|.*CI-only: needs Postgres' "${expected_path}"; then
	record_pass "a CI-only registry entry explains itself in the command cell"
else
	record_fail "reducer-contention did not render its ci_only_reason"
fi

# Case 8: a command containing a literal "|" (the authz-scoped-route-tests
# regex alternation) is escaped, not left to break the table structure. Every
# data row must have exactly 9 unescaped "|" table-cell separators for 8
# columns; count separators after collapsing escaped "\|" pairs.
bad_rows=0
while IFS= read -r row; do
	collapsed="${row//\\|/}"
	seps="$(printf '%s' "${collapsed}" | tr -dc '|' | wc -c | tr -d ' ')"
	if [[ "${seps}" -ne 9 ]]; then
		bad_rows=$((bad_rows + 1))
	fi
done < <(rg '^\| `' "${expected_path}")
if [[ "${bad_rows}" -eq 0 ]]; then
	record_pass "every data row has exactly 8 columns (pipe-in-command escaped correctly)"
else
	record_fail "${bad_rows} row(s) have the wrong column count (an unescaped '|' likely broke the table)"
fi

# Case 9 (negative case): a registry with zero gate records must fail the
# parser loudly, never emit a silently empty table.
empty_registry="${tmp_root}/empty-registry.yaml"
printf 'version: v1\ngates:\n' >"${empty_registry}"
if awk -f "${parser}" "${empty_registry}" >/dev/null 2>"${tmp_root}/empty-registry.err"; then
	record_fail "parser must exit non-zero on a registry with zero gate records"
else
	if rg -q 'no gate records found' "${tmp_root}/empty-registry.err"; then
		record_pass "parser fails closed on a registry with zero gate records"
	else
		record_fail "parser failed but without the expected 'no gate records found' message"
	fi
fi

# Case 10 (regression): a non_gate_workflows `  - file:` entry carries its own
# `reason:`. It must NOT bleed into the last gate/alias record (which stays open
# because no `  - id:` follows it). Regression for the prepr-stamp-verify row
# rendering refresh-cassettes.yml's "scheduled/manual cassette refresh" reason.
leak_registry="${tmp_root}/leak-registry.yaml"
printf 'version: v1\ngates:\n  - id: g1\n    name: G1\n    category: hygiene\n    tier: pre-commit\n    blocking: true\n    local:\n      command: "echo hi"\nhygiene_hooks:\n  - id: alias-last\n    reason: "ALIAS_OWN_REASON"\nnon_gate_workflows:\n  - file: some.yml\n    reason: "FILE_LEAK_REASON"\n' >"${leak_registry}"
leak_out="$(awk -f "${parser}" "${leak_registry}")"
alias_row="$(printf '%s\n' "${leak_out}" | rg '^\| `alias-last` \|' || true)"
if printf '%s' "${alias_row}" | rg -q 'ALIAS_OWN_REASON' \
	&& ! printf '%s' "${alias_row}" | rg -q 'FILE_LEAK_REASON' \
	&& ! printf '%s\n' "${leak_out}" | rg -q 'some.yml'; then
	record_pass "a non_gate_workflows reason does not bleed into the last alias row"
else
	record_fail "non_gate_workflows reason leaked into the alias row (or a file entry was rendered): ${alias_row}"
fi

if [[ "${FAIL}" -ne 0 ]]; then
	printf 'test-generate-ci-gates-doc FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
	exit 1
fi

printf 'test-generate-ci-gates-doc passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
