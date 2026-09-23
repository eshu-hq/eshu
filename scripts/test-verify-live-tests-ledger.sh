#!/usr/bin/env bash
# Static test for the live-test ledger (#6784): the verify script, the
# committed specs/live-tests.v1.yaml, and a seeded RED/GREEN pair proving
# the validator fails on a planted violation and passes on the clean tree.
# Fast, credential-free, Docker-free, network-free.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-live-tests-ledger.sh"
ledger="${repo_root}/specs/live-tests.v1.yaml"

fail() {
	printf 'test-verify-live-tests-ledger: %s\n' "$*" >&2
	exit 1
}

[[ "$(wc -l <"${BASH_SOURCE[0]}" | tr -d '[:space:]')" -lt 500 ]] ||
	fail "test script must stay under 500 lines"
[[ -x "${script}" ]] || fail "verify script missing or not executable"
[[ -f "${ledger}" ]] || fail "ledger missing"

# ── GREEN: the committed ledger validates on the clean tree ──────────────
out="$("${script}")" || fail "validator failed on the clean tree"
[[ "${out}" =~ ^live-tests\ ledger\ ok:\ [1-9][0-9]*\ rows,\ [1-9][0-9]*\ live\ files\ classified$ ]] ||
	fail "unexpected validator output: ${out}"

# ── RED: a planted unclassified live test fails ──────────────────────────
fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT
mkdir -p "${fixture}/go/orphan"
printf 'package orphan\n' >"${fixture}/go/orphan/orphan_live_test.go"
git -C "${fixture}" init -q && git -C "${fixture}" add go/orphan/orphan_live_test.go
cp "${ledger}" "${fixture}/ledger.yaml"
if REPO_ROOT="${fixture}" LEDGER_PATH="${fixture}/ledger.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a planted unclassified live test"
fi

# ── RED: a blank reason fails ────────────────────────────────────────────
cp "${ledger}" "${fixture}/ledger-blank.yaml"
python3 - "${fixture}/ledger-blank.yaml" <<'EOF'
import sys
path = sys.argv[1]
text = open(path).read()
old = "    reason: deterministic seeded answer-truth proof; runs on both backends in the live-backend CI job"
assert text.count(old) >= 1, "anchor reason not found"
open(path, "w").write(text.replace(old, "    reason: ", 1))
EOF
if REPO_ROOT="${repo_root}" LEDGER_PATH="${fixture}/ledger-blank.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a planted blank reason"
fi

# ── RED: a retired row naming a still-live file fails ────────────────────
cp "${ledger}" "${fixture}/ledger-retired-live.yaml"
python3 - "${fixture}/ledger-retired-live.yaml" <<'EOF'
import sys
path = sys.argv[1]
text = open(path).read()
old = "    class: scheduled"
assert text.count(old) >= 1, "anchor class not found"
open(path, "w").write(text.replace(old, "    class: retired", 1))
EOF
if REPO_ROOT="${repo_root}" LEDGER_PATH="${fixture}/ledger-retired-live.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a retired row naming a still-live file"
fi

# ── GREEN: a retired row naming an existing, repaired file passes ─────────
calm="$(mktemp -d)"
trap 'rm -rf "${fixture}" "${calm}"' EXIT
mkdir -p "${calm}/go/calm"
printf 'package calm\n' >"${calm}/go/calm/calm_live_test.go"
printf 'package calm\n\nimport "testing"\n\nfunc TestCalm(t *testing.T) {}\n' >"${calm}/go/calm/calm_test.go"
git -C "${calm}" init -q && git -C "${calm}" add go/calm/calm_live_test.go go/calm/calm_test.go
cat >"${calm}/ledger.yaml" <<'EOF'
version: live-tests/v1
updated_at: 2026-09-23
issue: 6784
parent_issue: 6788
owners:
  - graph
purpose: fixture
design: fixture
tests:
  - file: go/calm/calm_live_test.go
    tag: ~
    class: scheduled
    reason: fixture live proof
  - file: go/calm/calm_test.go
    tag: ~
    class: retired
    reason: repaired in fixture commit; live gate removed so it is no longer a live proof
EOF
REPO_ROOT="${calm}" LEDGER_PATH="${calm}/ledger.yaml" "${script}" >/dev/null 2>&1 ||
	fail "validator failed on a repaired-file retired row"

# ── RED: a row naming a missing file fails ───────────────────────────────
cp "${calm}/ledger.yaml" "${fixture}/ledger-stale.yaml"
python3 - "${fixture}/ledger-stale.yaml" <<'EOF'
import sys
path = sys.argv[1]
text = open(path).read()
anchor = "tests:\n"
assert anchor in text, "tests anchor not found"
open(path, "w").write(text.replace(anchor, "tests:\n  - file: go/calm/gone_live_test.go\n    tag: ~\n    class: scheduled\n    reason: planted stale row\n", 1))
EOF
if REPO_ROOT="${calm}" LEDGER_PATH="${fixture}/ledger-stale.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a row naming a missing file"
fi

# ── RED: an unknown class fails ──────────────────────────────────────────
cp "${ledger}" "${fixture}/ledger-badclass.yaml"
python3 - "${fixture}/ledger-badclass.yaml" <<'EOF'
import sys
path = sys.argv[1]
text = open(path).read()
old = "    class: scheduled"
assert text.count(old) >= 1, "anchor class not found"
open(path, "w").write(text.replace(old, "    class: eventually", 1))
EOF
if REPO_ROOT="${repo_root}" LEDGER_PATH="${fixture}/ledger-badclass.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with an unknown class"
fi

# ── RED: an invalid backends pin fails ─────────────────────────────────
cp "${ledger}" "${fixture}/ledger-badbackends.yaml"
python3 - "${fixture}/ledger-badbackends.yaml" <<'EOF'
import sys
path = sys.argv[1]
lines = open(path).read().splitlines(keepends=True)
idx = next(i for i, l in enumerate(lines) if l.startswith("    reason: "))
lines.insert(idx + 1, "    backends: oracle\n")
open(path, "w").write("".join(lines))
EOF
if REPO_ROOT="${repo_root}" LEDGER_PATH="${fixture}/ledger-badbackends.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with an invalid backends pin"
fi

# ── RED: a tag mismatch fails ────────────────────────────────────────────
cp "${ledger}" "${fixture}/ledger-badtag.yaml"
python3 - "${fixture}/ledger-badtag.yaml" <<'EOF'
import sys
path = sys.argv[1]
text = open(path).read()
old = "    tag: integration"
assert text.count(old) >= 1, "anchor tag not found"
open(path, "w").write(text.replace(old, "    tag: live_nornicdb_answer_truth", 1))
EOF
if REPO_ROOT="${repo_root}" LEDGER_PATH="${fixture}/ledger-badtag.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a tag mismatch"
fi

# ── RED: a duplicate row fails ───────────────────────────────────────────
cp "${ledger}" "${fixture}/ledger-dupe.yaml"
python3 - "${fixture}/ledger-dupe.yaml" <<'EOF'
import sys
path = sys.argv[1]
lines = open(path).read().splitlines(keepends=True)
idx = next(i for i, l in enumerate(lines) if l.startswith("  - file: "))
block = lines[idx:idx + 4]
assert len(block) == 4 and block[1].startswith("    tag: "), "anchor row not found"
lines[idx:idx] = block
open(path, "w").write("".join(lines))
EOF
if REPO_ROOT="${repo_root}" LEDGER_PATH="${fixture}/ledger-dupe.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a duplicate ledger row"
fi

# ── RED: a non-retired row for a repaired file fails ─────────────────────
cp "${calm}/ledger.yaml" "${fixture}/ledger-extra.yaml"
python3 - "${fixture}/ledger-extra.yaml" <<'EOF'
import sys
path = sys.argv[1]
text = open(path).read()
old = "    class: retired"
assert text.count(old) == 1, "anchor retired row not found"
open(path, "w").write(text.replace(old, "    class: scheduled", 1))
EOF
if REPO_ROOT="${calm}" LEDGER_PATH="${fixture}/ledger-extra.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a non-retired row naming a non-live file"
fi

# ── RED: a live-tagged Test func outside *_live_test.go fails ────────────
wild="$(mktemp -d)"
trap 'rm -rf "${fixture}" "${calm}" "${wild}"' EXIT
mkdir -p "${wild}/go/wild"
printf 'package wild\n' >"${wild}/go/wild/wild_live_test.go"
printf '//go:build live_wild_probe\n\npackage wild\n\nimport "testing"\n\nfunc TestWild(t *testing.T) {}\n' >"${wild}/go/wild/wild_test.go"
git -C "${wild}" init -q && git -C "${wild}" add go/wild/wild_live_test.go go/wild/wild_test.go
cat >"${wild}/ledger.yaml" <<'EOF'
version: live-tests/v1
updated_at: 2026-09-23
issue: 6784
parent_issue: 6788
owners:
  - graph
purpose: fixture
design: fixture
tests:
  - file: go/wild/wild_live_test.go
    tag: ~
    class: scheduled
    reason: fixture live proof
EOF
if REPO_ROOT="${wild}" LEDGER_PATH="${wild}/ledger.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with an unclassified nonstandard live file"
fi

printf 'test-verify-live-tests-ledger: RED/GREEN pair passed\n'
