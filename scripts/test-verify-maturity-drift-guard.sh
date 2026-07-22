#!/usr/bin/env bash
#
# test-verify-maturity-drift-guard.sh - hermetic tests for
# scripts/verify-maturity-drift-guard.sh, the gate that mechanically
# re-derives the live golden-corpus "supported" bar and fails when
# docs/public/languages/support-maturity.md drifts from it (#5400).
#
# Cases 1-9 build a scratch repo_root under mktemp and point the verifier at
# it via ESHU_MATURITY_DRIFT_GUARD_REPO_ROOT, so nothing here ever touches
# the committed matrix. Cases 10-11 run against the real repo tree: 10 proves
# the gate passes with the committed, already-correctly-graded matrix (the
# proof that this is not a vacuous check); 11 asserts a floor on the number
# of evaluated rows so a regex/glob regression that silently zeroes the scan
# cannot pass unnoticed.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-maturity-drift-guard.sh"

command -v rg >/dev/null 2>&1 || {
	echo "test-verify-maturity-drift-guard: rg is required" >&2
	exit 1
}
command -v jq >/dev/null 2>&1 || {
	echo "test-verify-maturity-drift-guard: jq is required" >&2
	exit 1
}

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0
record_pass() {
	PASS=$((PASS + 1))
	printf 'ok - %s\n' "$1"
}
record_fail() {
	FAIL=$((FAIL + 1))
	printf 'not ok - %s\n' "$1" >&2
}

# new_scratch_root creates a fresh scratch repo_root skeleton (the four
# directories the verifier expects to find its inputs under) and prints its
# path.
new_scratch_root() {
	local root
	root="$(mktemp -d "${tmp_root}/root.XXXXXX")"
	mkdir -p "${root}/scripts" "${root}/testdata/golden" "${root}/specs" \
		"${root}/docs/public/languages" "${root}/tests/fixtures/ecosystems"
	printf '%s' "${root}"
}

# write_gate_script writes a minimal scripts/verify-golden-corpus-gate.sh
# whose corpus_fixtures array is exactly the given fixture names (or, with no
# arguments, an empty array -- used by the empty-block fail-closed case).
write_gate_script() {
	local root="$1"
	shift
	{
		printf 'corpus_fixtures=(\n'
		local fixture
		for fixture in "$@"; do
			printf '\t%s\n' "${fixture}"
		done
		printf ')\n'
	} >"${root}/scripts/verify-golden-corpus-gate.sh"
}

write_sourced_gate_script() {
	local root="$1"
	shift
	mkdir -p "${root}/scripts/lib"
	printf 'source "${repo_root}/scripts/lib/golden-corpus-fixtures.sh"\n' \
		>"${root}/scripts/verify-golden-corpus-gate.sh"
	{
		printf 'corpus_fixtures=(\n'
		local fixture
		for fixture in "$@"; do
			printf '\t%s\n' "${fixture}"
		done
		printf ')\n'
	} >"${root}/scripts/lib/golden-corpus-fixtures.sh"
}

# write_ledger writes a minimal specs/language-feature-parity-ledger.v1.yaml
# with one "- language: <key>" entry per argument.
write_ledger() {
	local root="$1"
	shift
	: >"${root}/specs/language-feature-parity-ledger.v1.yaml"
	local key
	for key in "$@"; do
		printf -- '  - language: %s\n    docs_claim: docs/public/languages/%s.md\n' \
			"${key}" "${key}" >>"${root}/specs/language-feature-parity-ledger.v1.yaml"
	done
}

# write_matrix writes docs/public/languages/support-maturity.md with a
# well-formed header/separator plus the given raw data rows (each argument is
# one already-formatted "| Name | ... |" table line).
write_matrix() {
	local root="$1"
	shift
	{
		printf '| Parser | Parser Class | Grammar Routing | Normalization | Framework Or Root Evidence | Modeled Evidence | Query Surfacing | Real-Repo Validation | End-to-End Indexing |\n'
		printf '|--------|--------------|-----------------|---------------|----------------------------|------------------|-----------------|----------------------|---------------------|\n'
		local row
		for row in "$@"; do
			printf '%s\n' "${row}"
		done
	} >"${root}/docs/public/languages/support-maturity.md"
}

# write_fixture_source creates a scratch fixture directory containing one
# file with the given extension, so extension-sniffing attribution has
# something real to find.
write_fixture_source() {
	local root="$1" fixture="$2" ext="$3"
	mkdir -p "${root}/tests/fixtures/ecosystems/${fixture}"
	printf 'placeholder\n' >"${root}/tests/fixtures/ecosystems/${fixture}/main.${ext}"
}

run_verifier() {
	local root="$1"
	shift
	ESHU_MATURITY_DRIFT_GUARD_REPO_ROOT="${root}" bash "${verifier}" "$@"
}

# ---------------------------------------------------------------------------
# Case 1: correctly graded scratch matrix passes clean.
# ---------------------------------------------------------------------------
root1="$(new_scratch_root)"
write_gate_script "${root1}" go_comprehensive python_comprehensive ruby_rails_app
write_ledger "${root1}" go python ruby
write_fixture_source "${root1}" go_comprehensive go
write_fixture_source "${root1}" python_comprehensive py
write_fixture_source "${root1}" ruby_rails_app rb
cat >"${root1}/testdata/golden/e2e-20repo-snapshot.json" <<'JSON'
{
  "graph": {
    "required_correlations": [
      {"id": "rc-1", "description": "symbol reference projected from the go_comprehensive fixture"}
    ]
  },
  "query_shapes": {
    "mcp": {
      "investigate_dead_code": {"arguments": {"repo_id": "orders-api", "language": "python"}}
    }
  }
}
JSON
write_matrix "${root1}" \
	'| ArgoCD | `DefaultEngine (yaml)` | - | - | unsupported | manifest evidence only | - | - | - |' \
	'| Go | `DefaultEngine (go)` | supported | supported | derived roots | net/http | supported | supported | supported |' \
	'| Python | `DefaultEngine (python)` | supported | supported | derived roots | FastAPI | supported | supported | supported |' \
	'| Ruby | `DefaultEngine (ruby)` | supported | supported | derived roots | Rails | supported | fixture-backed | fixture-backed |'
if out1="$(ESHU_MATURITY_DRIFT_GUARD_ROW_FLOOR=1 run_verifier "${root1}" 2>&1)"; then
	record_pass "case 1: correctly graded scratch matrix passes"
else
	record_fail "case 1: correctly graded scratch matrix should pass, got: ${out1}"
fi

# ---------------------------------------------------------------------------
# Case 2: over-graded drift (Ruby falsely promoted to supported/supported
# with no B-12 attribution) fails, in the over-graded direction.
# ---------------------------------------------------------------------------
root2="$(new_scratch_root)"
write_gate_script "${root2}" go_comprehensive python_comprehensive ruby_rails_app
write_ledger "${root2}" go python ruby
write_fixture_source "${root2}" go_comprehensive go
write_fixture_source "${root2}" python_comprehensive py
write_fixture_source "${root2}" ruby_rails_app rb
cp "${root1}/testdata/golden/e2e-20repo-snapshot.json" "${root2}/testdata/golden/e2e-20repo-snapshot.json"
write_matrix "${root2}" \
	'| Go | `DefaultEngine (go)` | supported | supported | derived roots | net/http | supported | supported | supported |' \
	'| Python | `DefaultEngine (python)` | supported | supported | derived roots | FastAPI | supported | supported | supported |' \
	'| Ruby | `DefaultEngine (ruby)` | supported | supported | derived roots | Rails | supported | supported | supported |'
if out2="$(ESHU_MATURITY_DRIFT_GUARD_ROW_FLOOR=1 run_verifier "${root2}" 2>&1)"; then
	record_fail "case 2: over-graded Ruby row should fail, but passed"
else
	if printf '%s' "${out2}" | rg -q "DRIFT \(over-graded\).*Ruby"; then
		record_pass "case 2: over-graded Ruby row fails with an over-graded drift message"
	else
		record_fail "case 2: failed as expected but wrong message: ${out2}"
	fi
fi

# ---------------------------------------------------------------------------
# Case 3: under-graded drift (Go falsely demoted to fixture-backed despite
# being live-supported) fails, in the under-graded direction.
# ---------------------------------------------------------------------------
root3="$(new_scratch_root)"
write_gate_script "${root3}" go_comprehensive python_comprehensive ruby_rails_app
write_ledger "${root3}" go python ruby
write_fixture_source "${root3}" go_comprehensive go
write_fixture_source "${root3}" python_comprehensive py
write_fixture_source "${root3}" ruby_rails_app rb
cp "${root1}/testdata/golden/e2e-20repo-snapshot.json" "${root3}/testdata/golden/e2e-20repo-snapshot.json"
write_matrix "${root3}" \
	'| Go | `DefaultEngine (go)` | supported | supported | derived roots | net/http | supported | fixture-backed | fixture-backed |' \
	'| Python | `DefaultEngine (python)` | supported | supported | derived roots | FastAPI | supported | supported | supported |' \
	'| Ruby | `DefaultEngine (ruby)` | supported | supported | derived roots | Rails | supported | fixture-backed | fixture-backed |'
if out3="$(ESHU_MATURITY_DRIFT_GUARD_ROW_FLOOR=1 run_verifier "${root3}" 2>&1)"; then
	record_fail "case 3: under-graded Go row should fail, but passed"
else
	if printf '%s' "${out3}" | rg -q "DRIFT \(under-graded\).*Go"; then
		record_pass "case 3: under-graded Go row fails with an under-graded drift message"
	else
		record_fail "case 3: failed as expected but wrong message: ${out3}"
	fi
fi

# ---------------------------------------------------------------------------
# Case 4: missing B-12 snapshot file fails closed.
# ---------------------------------------------------------------------------
root4="$(new_scratch_root)"
write_gate_script "${root4}" go_comprehensive
write_ledger "${root4}" go
write_fixture_source "${root4}" go_comprehensive go
write_matrix "${root4}" \
	'| Go | `DefaultEngine (go)` | supported | supported | derived roots | net/http | supported | supported | supported |'
if out4="$(run_verifier "${root4}" 2>&1)"; then
	record_fail "case 4: missing snapshot file should fail closed, but passed"
else
	if printf '%s' "${out4}" | rg -q "B-12 snapshot not found"; then
		record_pass "case 4: missing snapshot file fails closed with a clear message"
	else
		record_fail "case 4: failed as expected but wrong message: ${out4}"
	fi
fi

# ---------------------------------------------------------------------------
# Case 5: malformed (non-JSON) B-12 snapshot fails closed.
# ---------------------------------------------------------------------------
root5="$(new_scratch_root)"
write_gate_script "${root5}" go_comprehensive
write_ledger "${root5}" go
write_fixture_source "${root5}" go_comprehensive go
printf 'not { valid json' >"${root5}/testdata/golden/e2e-20repo-snapshot.json"
write_matrix "${root5}" \
	'| Go | `DefaultEngine (go)` | supported | supported | derived roots | net/http | supported | supported | supported |'
if out5="$(run_verifier "${root5}" 2>&1)"; then
	record_fail "case 5: malformed snapshot JSON should fail closed, but passed"
else
	if printf '%s' "${out5}" | rg -q "not valid JSON"; then
		record_pass "case 5: malformed snapshot JSON fails closed with a clear message"
	else
		record_fail "case 5: failed as expected but wrong message: ${out5}"
	fi
fi

# ---------------------------------------------------------------------------
# Case 6: an empty corpus_fixtures array fails closed (parse-regression
# guard, not a legitimate "nothing staged" state).
# ---------------------------------------------------------------------------
root6="$(new_scratch_root)"
write_gate_script "${root6}"
write_ledger "${root6}" go
cat >"${root6}/testdata/golden/e2e-20repo-snapshot.json" <<'JSON'
{"graph": {"required_correlations": []}, "query_shapes": {}}
JSON
write_matrix "${root6}" \
	'| Go | `DefaultEngine (go)` | supported | supported | derived roots | net/http | supported | fixture-backed | fixture-backed |'
if out6="$(run_verifier "${root6}" 2>&1)"; then
	record_fail "case 6: empty corpus_fixtures array should fail closed, but passed"
else
	if printf '%s' "${out6}" | rg -q "parsed zero corpus_fixtures"; then
		record_pass "case 6: empty corpus_fixtures array fails closed with a clear message"
	else
		record_fail "case 6: failed as expected but wrong message: ${out6}"
	fi
fi

# ---------------------------------------------------------------------------
# Case 6b: a corpus_fixtures array extracted into the sourced inventory is
# still authoritative for maturity grading.
# ---------------------------------------------------------------------------
root6b="$(new_scratch_root)"
write_sourced_gate_script "${root6b}" go_comprehensive
write_ledger "${root6b}" go
write_fixture_source "${root6b}" go_comprehensive go
cat >"${root6b}/testdata/golden/e2e-20repo-snapshot.json" <<'JSON'
{"graph": {"required_correlations": [{"id": "rc-go", "source_label": "Function", "relationship": "CALLS", "target_label": "Function"}]}, "query_shapes": {}}
JSON
write_matrix "${root6b}" \
	'| Go | `DefaultEngine (go)` | supported | supported | derived roots | net/http | supported | fixture-backed | fixture-backed |'
if out6b="$(ESHU_MATURITY_DRIFT_GUARD_ROW_FLOOR=1 run_verifier "${root6b}" 2>&1)"; then
	record_pass "case 6b: sourced corpus fixture inventory is graded"
else
	record_fail "case 6b: sourced corpus fixture inventory should pass: ${out6b}"
fi

# ---------------------------------------------------------------------------
# Case 7: a staged fixture with no directory on disk fails closed.
# ---------------------------------------------------------------------------
root7="$(new_scratch_root)"
write_gate_script "${root7}" go_comprehensive missing_fixture
write_ledger "${root7}" go
write_fixture_source "${root7}" go_comprehensive go
cat >"${root7}/testdata/golden/e2e-20repo-snapshot.json" <<'JSON'
{"graph": {"required_correlations": []}, "query_shapes": {}}
JSON
write_matrix "${root7}" \
	'| Go | `DefaultEngine (go)` | supported | supported | derived roots | net/http | supported | fixture-backed | fixture-backed |'
if out7="$(run_verifier "${root7}" 2>&1)"; then
	record_fail "case 7: staged fixture missing on disk should fail closed, but passed"
else
	if printf '%s' "${out7}" | rg -q "staged corpus fixture not found on disk"; then
		record_pass "case 7: staged fixture missing on disk fails closed with a clear message"
	else
		record_fail "case 7: failed as expected but wrong message: ${out7}"
	fi
fi

# ---------------------------------------------------------------------------
# Case 8: a matrix row whose name does not transliterate to a known ledger
# key fails closed (renamed row / ledger drift), rather than being silently
# skipped.
# ---------------------------------------------------------------------------
root8="$(new_scratch_root)"
write_gate_script "${root8}" go_comprehensive
write_ledger "${root8}" go
write_fixture_source "${root8}" go_comprehensive go
cat >"${root8}/testdata/golden/e2e-20repo-snapshot.json" <<'JSON'
{"graph": {"required_correlations": [{"id": "rc-1", "description": "go_comprehensive"}]}, "query_shapes": {}}
JSON
write_matrix "${root8}" \
	'| Go | `DefaultEngine (go)` | supported | supported | derived roots | net/http | supported | supported | supported |' \
	'| Nonexistent Lang | `DefaultEngine (nope)` | supported | supported | derived roots | nothing | supported | fixture-backed | fixture-backed |'
if out8="$(run_verifier "${root8}" 2>&1)"; then
	record_fail "case 8: unresolvable row name should fail closed, but passed"
else
	if printf '%s' "${out8}" | rg -q "not a known language ledger key"; then
		record_pass "case 8: unresolvable row name fails closed with a clear message"
	else
		record_fail "case 8: failed as expected but wrong message: ${out8}"
	fi
fi

# ---------------------------------------------------------------------------
# Case 9: a row filter sanity check -- a "-" Query Surfacing row with
# mismatched-looking grades is never evaluated, so it does not itself cause a
# failure (already implicit in case 1's ArgoCD row, asserted explicitly here
# with a row shaped to look drifted if it were wrongly evaluated).
# ---------------------------------------------------------------------------
root9="$(new_scratch_root)"
write_gate_script "${root9}" go_comprehensive
write_ledger "${root9}" go
write_fixture_source "${root9}" go_comprehensive go
cat >"${root9}/testdata/golden/e2e-20repo-snapshot.json" <<'JSON'
{"graph": {"required_correlations": [{"id": "rc-1", "description": "go_comprehensive"}]}, "query_shapes": {}}
JSON
write_matrix "${root9}" \
	'| Go | `DefaultEngine (go)` | supported | supported | derived roots | net/http | supported | supported | supported |' \
	'| Kubernetes | `DefaultEngine (yaml)` | - | - | unsupported | workload evidence only | - | supported | supported |'
if out9="$(ESHU_MATURITY_DRIFT_GUARD_ROW_FLOOR=1 run_verifier "${root9}" 2>&1)"; then
	record_pass "case 9: a '-' Query Surfacing row is never evaluated, even with supported-looking grades"
else
	record_fail "case 9: '-' Query Surfacing row should be filtered out, but the gate failed: ${out9}"
fi

# ---------------------------------------------------------------------------
# Case 10: the real repo tree passes with the committed, already-correctly-
# graded matrix (#5336's hand-grading). Proves this is not a vacuous check.
# ---------------------------------------------------------------------------
if out10="$(bash "${verifier}" 2>&1)"; then
	record_pass "case 10: real repo tree passes with the committed matrix (${out10#*: })"
else
	record_fail "case 10: real repo tree should pass, got: ${out10}"
fi

# ---------------------------------------------------------------------------
# Case 11: the real repo tree evaluates at least 20 rows (23 today), guarding
# against a regex/glob regression silently zeroing the table scan.
# ---------------------------------------------------------------------------
if out11="$(ESHU_MATURITY_DRIFT_GUARD_ROW_FLOOR=20 bash "${verifier}" 2>&1)"; then
	record_pass "case 11: real repo tree clears the 20-row evaluation floor"
else
	record_fail "case 11: real repo tree should clear the 20-row floor, got: ${out11}"
fi

# ---------------------------------------------------------------------------
# Case 12: an artificially low real-tree row count trips the floor guard
# itself (proves the floor check is live, not dead code).
# ---------------------------------------------------------------------------
root12="$(new_scratch_root)"
write_gate_script "${root12}" go_comprehensive
write_ledger "${root12}" go
write_fixture_source "${root12}" go_comprehensive go
cat >"${root12}/testdata/golden/e2e-20repo-snapshot.json" <<'JSON'
{"graph": {"required_correlations": [{"id": "rc-1", "description": "go_comprehensive"}]}, "query_shapes": {}}
JSON
write_matrix "${root12}" \
	'| Go | `DefaultEngine (go)` | supported | supported | derived roots | net/http | supported | supported | supported |'
if out12="$(ESHU_MATURITY_DRIFT_GUARD_ROW_FLOOR=5 run_verifier "${root12}" 2>&1)"; then
	record_fail "case 12: a 1-row scratch matrix under a floor of 5 should fail closed, but passed"
else
	if printf '%s' "${out12}" | rg -q "evaluated only 1 matrix row"; then
		record_pass "case 12: 1-row scratch matrix under a floor of 5 fails closed with a clear message"
	else
		record_fail "case 12: failed as expected but wrong message: ${out12}"
	fi
fi

# ---------------------------------------------------------------------------
# Case 13: POLYGLOT fixture -- the TEXTUAL fixture-name signal must NOT be
# trusted for a fixture that attributes to more than one language. A single
# fixture "polyglot-app" carries both .go and .py, so it attributes to both
# go and python. The B-12 blob structurally attributes only go
# ("language": "go") and mentions the fixture name once. Correct result: go
# is live-supported (STRUCTURED), python is NOT (its only signal is the
# ambiguous shared fixture-name mention). A matrix that marks python
# supported off that shared mention is over-graded and must fail.
# ---------------------------------------------------------------------------
root13="$(new_scratch_root)"
write_gate_script "${root13}" polyglot-app
write_ledger "${root13}" go python
write_fixture_source "${root13}" polyglot-app go
write_fixture_source "${root13}" polyglot-app py
cat >"${root13}/testdata/golden/e2e-20repo-snapshot.json" <<'JSON'
{
  "graph": {
    "required_correlations": [
      {"id": "rc-1", "description": "symbol reference projected from the polyglot-app fixture"}
    ]
  },
  "query_shapes": {
    "mcp": {
      "investigate_dead_code": {"arguments": {"repo_id": "polyglot-app", "language": "go"}}
    }
  }
}
JSON
write_matrix "${root13}" \
	'| Go | `DefaultEngine (go)` | supported | supported | derived roots | net/http | supported | supported | supported |' \
	'| Python | `DefaultEngine (python)` | supported | supported | derived roots | FastAPI | supported | supported | supported |'
if out13="$(ESHU_MATURITY_DRIFT_GUARD_ROW_FLOOR=1 run_verifier "${root13}" 2>&1)"; then
	record_fail "case 13: polyglot fixture-name should NOT mark python live-supported, but the gate passed"
else
	if printf '%s' "${out13}" | rg -q "DRIFT \(over-graded\).*Python"; then
		record_pass "case 13: polyglot TEXTUAL signal is not trusted -- python over-grade fails, go (STRUCTURED) is fine"
	else
		record_fail "case 13: failed as expected but wrong message: ${out13}"
	fi
fi

# ---------------------------------------------------------------------------
# Case 14: same polyglot setup as case 13, but with the CORRECT grading --
# go supported (clears STRUCTURED even though its fixture is polyglot),
# python fixture-backed. The gate must pass, proving the polyglot guard does
# not falsely demote a language that has its own structured B-12 evidence.
# ---------------------------------------------------------------------------
root14="$(new_scratch_root)"
write_gate_script "${root14}" polyglot-app
write_ledger "${root14}" go python
write_fixture_source "${root14}" polyglot-app go
write_fixture_source "${root14}" polyglot-app py
cp "${root13}/testdata/golden/e2e-20repo-snapshot.json" "${root14}/testdata/golden/e2e-20repo-snapshot.json"
write_matrix "${root14}" \
	'| Go | `DefaultEngine (go)` | supported | supported | derived roots | net/http | supported | supported | supported |' \
	'| Python | `DefaultEngine (python)` | supported | supported | derived roots | FastAPI | supported | fixture-backed | fixture-backed |'
if out14="$(ESHU_MATURITY_DRIFT_GUARD_ROW_FLOOR=1 run_verifier "${root14}" 2>&1)"; then
	record_pass "case 14: polyglot fixture with correct grading passes (go STRUCTURED live-supported, python fixture-backed)"
else
	record_fail "case 14: correctly graded polyglot setup should pass, got: ${out14}"
fi

printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
[ "${FAIL}" -eq 0 ]
