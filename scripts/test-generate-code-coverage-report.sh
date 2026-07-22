#!/usr/bin/env bash
# Static and fixture-backed mirror for the public Go code-coverage report.
# Fast, credential-free, Docker-free: it proves the generator contract, workflow
# wiring, README badge/link, docs nav entry, and fixture rendering shape.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
generator="${repo_root}/scripts/generate-code-coverage-report.sh"
workflow="${repo_root}/.github/workflows/code-coverage-report.yml"
report="${repo_root}/docs/public/reference/code-coverage.md"
shield="${repo_root}/docs/public/reference/code-coverage-shield.json"
registry="${repo_root}/specs/ci-gates.v1.yaml"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0

record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

require() {
	local label="$1" needle="$2" file="$3"
	if rg --fixed-strings --quiet -- "${needle}" "${file}"; then
		record_pass "${label}"
	else
		record_fail "missing ${label}: ${needle}"
	fi
}

require_jq() {
	if ! command -v jq >/dev/null 2>&1; then
		printf 'skip - jq is not installed; cannot validate generated shield JSON\n' >&2
		exit 0
	fi
}

require_jq

if [[ -f "${generator}" && -x "${generator}" ]]; then
	record_pass "generator exists and is executable"
else
	record_fail "generator must exist and be executable"
fi
bash -n "${generator}" 2>/dev/null && record_pass "generator has valid shell syntax" || record_fail "generator shell syntax is invalid"

fixture_profile="${tmp_root}/coverage.out"
cat >"${fixture_profile}" <<'PROFILE'
mode: count
go/internal/covered/covered.go:1.1,2.1 2 1
go/internal/uncovered/uncovered.go:1.1,2.1 4 0
go/internal/generated/service.pb.go:1.1,2.1 8 0
go/internal/fixtures/testdata/sample.go:1.1,2.1 8 0
PROFILE

out_report="${tmp_root}/code-coverage.md"
out_shield="${tmp_root}/code-coverage-shield.json"
if ESHU_CODE_COVERAGE_RUN_TESTS=0 \
	ESHU_CODE_COVERAGE_PROFILE_IN="${fixture_profile}" \
	ESHU_CODE_COVERAGE_REPORT_OUT="${out_report}" \
	ESHU_CODE_COVERAGE_SHIELD_OUT="${out_shield}" \
	"${generator}" >/dev/null 2>"${tmp_root}/generator.err"; then
	record_pass "generator renders from a fixture coverage profile"
else
	record_fail "generator failed on fixture coverage profile"
	sed -n '1,80p' "${tmp_root}/generator.err" >&2 || true
fi

canonical_profile_a="${tmp_root}/canonical-a.out"
canonical_profile_b="${tmp_root}/canonical-b.out"
cat >"${canonical_profile_a}" <<'PROFILE'
mode: count
go/internal/concurrent/concurrent.go:1.1,2.1 3333 1
go/internal/concurrent/concurrent.go:3.1,4.1 6667 0
PROFILE
cat >"${canonical_profile_b}" <<'PROFILE'
mode: count
go/internal/concurrent/concurrent.go:1.1,2.1 3334 1
go/internal/concurrent/concurrent.go:3.1,4.1 6666 0
PROFILE
canonical_report_a="${tmp_root}/canonical-a.md"
canonical_report_b="${tmp_root}/canonical-b.md"
canonical_shield_a="${tmp_root}/canonical-a.json"
canonical_shield_b="${tmp_root}/canonical-b.json"
ESHU_CODE_COVERAGE_RUN_TESTS=0 \
	ESHU_CODE_COVERAGE_PROFILE_IN="${canonical_profile_a}" \
	ESHU_CODE_COVERAGE_REPORT_OUT="${canonical_report_a}" \
	ESHU_CODE_COVERAGE_SHIELD_OUT="${canonical_shield_a}" \
	"${generator}" >/dev/null
ESHU_CODE_COVERAGE_RUN_TESTS=0 \
	ESHU_CODE_COVERAGE_PROFILE_IN="${canonical_profile_b}" \
	ESHU_CODE_COVERAGE_REPORT_OUT="${canonical_report_b}" \
	ESHU_CODE_COVERAGE_SHIELD_OUT="${canonical_shield_b}" \
	"${generator}" >/dev/null
if cmp -s "${canonical_report_a}" "${canonical_report_b}" && cmp -s "${canonical_shield_a}" "${canonical_shield_b}"; then
	record_pass "equivalent rounded coverage renders byte-identically"
else
	record_fail "equivalent rounded coverage must not churn checked-in artifacts"
fi

boundary_profile_a="${tmp_root}/boundary-a.out"
boundary_profile_b="${tmp_root}/boundary-b.out"
cat >"${boundary_profile_a}" <<'PROFILE'
mode: count
go/internal/concurrent/concurrent.go:1.1,2.1 7574 1
go/internal/concurrent/concurrent.go:3.1,4.1 2426 0
PROFILE
cat >"${boundary_profile_b}" <<'PROFILE'
mode: count
go/internal/concurrent/concurrent.go:1.1,2.1 7576 1
go/internal/concurrent/concurrent.go:3.1,4.1 2424 0
PROFILE
boundary_report_a="${tmp_root}/boundary-a.md"
boundary_report_b="${tmp_root}/boundary-b.md"
boundary_shield_a="${tmp_root}/boundary-a.json"
boundary_shield_b="${tmp_root}/boundary-b.json"
ESHU_CODE_COVERAGE_RUN_TESTS=0 \
	ESHU_CODE_COVERAGE_PROFILE_IN="${boundary_profile_a}" \
	ESHU_CODE_COVERAGE_REPORT_OUT="${boundary_report_a}" \
	ESHU_CODE_COVERAGE_SHIELD_OUT="${boundary_shield_a}" \
	"${generator}" >/dev/null
ESHU_CODE_COVERAGE_RUN_TESTS=0 \
	ESHU_CODE_COVERAGE_PROFILE_IN="${boundary_profile_b}" \
	ESHU_CODE_COVERAGE_REPORT_OUT="${boundary_report_b}" \
	ESHU_CODE_COVERAGE_SHIELD_OUT="${boundary_shield_b}" \
	"${generator}" >/dev/null
if cmp -s "${boundary_report_a}" "${boundary_report_b}" && cmp -s "${boundary_shield_a}" "${boundary_shield_b}"; then
	record_pass "scheduler-equivalent profiles across a tenth boundary render byte-identically"
else
	record_fail "scheduler-equivalent profiles across a tenth boundary must not churn checked-in artifacts"
fi

require "fixture total coverage" "Total Go code coverage: **33%**" "${out_report}"
require "fixture package drilldown boundary" "raw Go coverage profile" "${out_report}"
require "fixture generated exclusion" "Generated Go files" "${out_report}"
if [[ "$(wc -l <"${out_report}")" -lt 500 ]]; then
	record_pass "fixture report stays under the file cap"
else
	record_fail "fixture report exceeds the file cap"
fi
if jq -e '.schemaVersion == 1 and .label == "go coverage" and .message == "33%"' "${out_shield}" >/dev/null; then
	record_pass "fixture shield JSON has the expected endpoint shape"
else
	record_fail "fixture shield JSON shape mismatch"
fi

[[ -f "${report}" ]] && record_pass "committed report exists" || record_fail "committed report is missing"
if [[ "$(wc -l <"${report}")" -lt 500 ]]; then
	record_pass "committed report stays under the file cap"
else
	record_fail "committed report exceeds the file cap"
fi
require "report generated marker" "GENERATED by generate-code-coverage-report" "${report}"
require "report separates proof types" "not a replacement for replay, golden-corpus, or full-corpus proof" "${report}"
require "docs nav entry" "reference/code-coverage.md" "${repo_root}/docs/mkdocs.yml"
require "README coverage badge" "code-coverage-shield.json" "${repo_root}/README.md"
require "README proof caveat" "one signal, not a replacement" "${repo_root}/README.md"

[[ -f "${workflow}" ]] && record_pass "coverage workflow exists" || record_fail "coverage workflow is missing"
require "workflow runs test mirror" "scripts/test-generate-code-coverage-report.sh" "${workflow}"
require "workflow runs generator" "scripts/generate-code-coverage-report.sh" "${workflow}"
require "workflow writes coverage profile at workspace root" 'ESHU_CODE_COVERAGE_PROFILE_OUT: ${{ github.workspace }}/go-code-coverage.out' "${workflow}"
require "workflow uploads report artifact" "code-coverage-report" "${workflow}"
require "workflow safe for forks" "permissions:" "${workflow}"

require "ci gate registry entry" "id: code-coverage-report" "${registry}"
require "ci gate registry workflow ref" "code-coverage-report.yml" "${registry}"

if [[ "${FAIL}" -ne 0 ]]; then
	printf 'generate-code-coverage-report tests FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
	exit 1
fi

printf 'generate-code-coverage-report tests passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
