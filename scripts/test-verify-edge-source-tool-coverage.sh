#!/usr/bin/env bash
#
# test-verify-edge-source-tool-coverage.sh — structural and functional test
# mirror for verify-edge-source-tool-coverage.sh.
#
# Mirrors the shape of test-verify-telemetry-coverage.sh. Checks:
#   1. Structural assertions: the verifier exists, is executable, parses under
#      bash -n, and references the expected source files.
#   2. Functional injection test: inject a bogus EvidenceKind constant into a
#      temporary copy of models.go whose value has no matching prefix and whose
#      identifier is not in the classifier map → verifier must EXIT NONZERO.
#   3. Functional real-tree test: run the verifier against the actual repo files
#      → verifier must EXIT ZERO.
#
# The verifier accepts two env-var overrides so the mirror can inject fixtures
# without touching the real source files:
#   ESHU_SOURCE_TOOL_MODELS_FILE      — path to relationships/models.go
#   ESHU_SOURCE_TOOL_CLASSIFIER_FILE  — path to cross_repo_evidence_type.go
#
# Exit 0 if all checks pass; non-zero on first failure.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-edge-source-tool-coverage.sh"
models_file="${repo_root}/go/internal/relationships/models.go"
classifier_file="${repo_root}/go/internal/reducer/cross_repo_evidence_type.go"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0
TOTAL=0

out_file="${tmp_root}/verifier.out"
err_file="${tmp_root}/verifier.err"

record_pass() {
  PASS=$((PASS + 1))
  TOTAL=$((TOTAL + 1))
  printf 'ok - %s\n' "$1"
}

record_fail() {
  FAIL=$((FAIL + 1))
  TOTAL=$((TOTAL + 1))
  printf 'not ok - %s\n' "$1" >&2
  if [ -f "$out_file" ]; then
    printf '%s\n' '--- stdout ---' >&2
    sed -n '1,40p' "$out_file" >&2
  fi
  if [ -f "$err_file" ]; then
    printf '%s\n' '--- stderr ---' >&2
    sed -n '1,40p' "$err_file" >&2
  fi
}

# ---------------------------------------------------------------------------
# Section 1: Structural assertions
# ---------------------------------------------------------------------------

# 1a: verifier file exists and is executable.
if [ -f "$verifier" ] && [ -x "$verifier" ]; then
  record_pass "verifier exists and is executable"
else
  record_fail "verifier exists and is executable"
fi

# 1b: verifier parses cleanly under bash -n (syntax check).
if bash -n "$verifier" 2>"$err_file"; then
  record_pass "verifier passes bash -n syntax check"
else
  record_fail "verifier passes bash -n syntax check"
fi

# 1c: verifier references models.go (the EvidenceKind constant source).
if rg -q 'models[.]go' "$verifier" 2>/dev/null || rg -q 'ESHU_SOURCE_TOOL_MODELS_FILE' "$verifier" 2>/dev/null; then
  record_pass "verifier references models.go / ESHU_SOURCE_TOOL_MODELS_FILE"
else
  record_fail "verifier references models.go / ESHU_SOURCE_TOOL_MODELS_FILE"
fi

# 1d: verifier references cross_repo_evidence_type.go (the classifier source).
if rg -q 'cross_repo_evidence_type[.]go' "$verifier" 2>/dev/null || rg -q 'ESHU_SOURCE_TOOL_CLASSIFIER_FILE' "$verifier" 2>/dev/null; then
  record_pass "verifier references cross_repo_evidence_type.go / ESHU_SOURCE_TOOL_CLASSIFIER_FILE"
else
  record_fail "verifier references cross_repo_evidence_type.go / ESHU_SOURCE_TOOL_CLASSIFIER_FILE"
fi

# 1e: verifier contains the key behavior keywords: EvidenceKind, source_tool,
#     evidenceKindToSourceTool, sourceToolPrefixFallback.
for keyword in 'EvidenceKind' 'source_tool' 'evidenceKindToSourceTool' 'sourceToolPrefixFallback'; do
  if rg -q "$keyword" "$verifier" 2>/dev/null; then
    record_pass "verifier mentions $keyword"
  else
    record_fail "verifier mentions $keyword"
  fi
done

# 1f: verifier references the contract doc path.
if rg -q 'edge-source-tool-provenance' "$verifier" 2>/dev/null; then
  record_pass "verifier references the contract doc path"
else
  record_fail "verifier references the contract doc path"
fi

# ---------------------------------------------------------------------------
# Section 2: Functional injection test — bogus constant must fail.
# ---------------------------------------------------------------------------
# Build a temporary models.go that is a copy of the real one, with an extra
# constant appended whose value has no matching prefix and whose identifier is
# absent from the classifier map. The verifier must exit nonzero.

injected_models="${tmp_root}/models_injected.go"
cp "$models_file" "$injected_models"
# Append the bogus constant inside the const block by adding it before the
# closing parenthesis. Simpler: just append it at end-of-file after the const
# block; the rg pattern does not require Go validity, only the line format.
printf '%s\n' '' '	// Injected by test mirror: deliberately uncovered constant.' \
  '	EvidenceKindFakeUnmapped EvidenceKind = "FAKE_UNMAPPED_TOOL"' \
  >>"$injected_models"

injected_exit=0
ESHU_SOURCE_TOOL_MODELS_FILE="$injected_models" \
  ESHU_SOURCE_TOOL_CLASSIFIER_FILE="$classifier_file" \
  "$verifier" >"$out_file" 2>"$err_file" || injected_exit=$?

if [ "$injected_exit" -ne 0 ]; then
  record_pass "exits nonzero when injected uncovered constant is present"
else
  record_fail "exits nonzero when injected uncovered constant is present"
fi

# The error output must name the bogus identifier.
if rg -q 'EvidenceKindFakeUnmapped' "$err_file" 2>/dev/null; then
  record_pass "error output names the uncovered constant identifier"
else
  record_fail "error output names the uncovered constant identifier"
fi

# The error output must contain a fix-hint pointing at the classifier file path.
if rg -q 'cross_repo_evidence_type' "$err_file" 2>/dev/null; then
  record_pass "error output contains fix-hint pointing at classifier file"
else
  record_fail "error output contains fix-hint pointing at classifier file"
fi

# ---------------------------------------------------------------------------
# Section 3: Functional real-tree test — real files must pass.
# ---------------------------------------------------------------------------
real_exit=0
ESHU_SOURCE_TOOL_MODELS_FILE="$models_file" \
  ESHU_SOURCE_TOOL_CLASSIFIER_FILE="$classifier_file" \
  "$verifier" >"$out_file" 2>"$err_file" || real_exit=$?

if [ "$real_exit" -eq 0 ]; then
  record_pass "exits zero on the real repo tree (no coverage gap)"
else
  record_fail "exits zero on the real repo tree (no coverage gap)"
fi

# The success output must mention a constant count and "pass".
if rg -q 'EvidenceKind constants.*classified.*pass' "$out_file" 2>/dev/null; then
  record_pass "success output reports constant count and pass status"
else
  record_fail "success output reports constant count and pass status"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
if [ "${FAIL}" -ne 0 ]; then
  printf 'verify-edge-source-tool-coverage tests FAILED: %d/%d failed\n' "${FAIL}" "${TOTAL}" >&2
  exit 1
fi

printf 'verify-edge-source-tool-coverage tests passed: %d/%d\n' "${PASS}" "${TOTAL}"
