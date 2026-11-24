#!/usr/bin/env bash
# Static test for the queue-contract coverage ledger (#11354): the verify
# script, the committed specs/queue-contract-coverage.v1.yaml, and a seeded
# RED/GREEN pair proving the validator fails on planted violations and
# passes on the clean tree.
# Fast, credential-free, Docker-free, network-free.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-queue-contract-coverage.sh"
ledger="${repo_root}/specs/queue-contract-coverage.v1.yaml"

fail() {
	printf 'test-verify-queue-contract-coverage: %s\n' "$*" >&2
	exit 1
}

[[ "$(wc -l <"${BASH_SOURCE[0]}" | tr -d '[:space:]')" -lt 500 ]] ||
	fail "test script must stay under 500 lines"
[[ -x "${script}" ]] || fail "verify script missing or not executable"
[[ -f "${ledger}" ]] || fail "ledger missing"
command -v rg >/dev/null 2>&1 || fail "rg not on PATH"
command -v python3 >/dev/null 2>&1 || fail "python3 not on PATH"

# ── GREEN: the committed ledger validates on the clean tree ──────────────
out="$("${script}")" || fail "validator failed on the clean tree"
[[ "${out}" =~ ^queue-contract-coverage\ ok:\ [1-9][0-9]*\ rows,\ [1-9][0-9]*\ symbols,\ [1-9][0-9]*\ proving\ tests$ ]] ||
	fail "unexpected validator output: ${out}"

probe="$(mktemp -d)"
trap 'rm -rf "${probe}"' EXIT

# Every RED case below must fail: REPO_ROOT stays the real tree so the
# code-side checks (file, Test func, symbol, tag, enrollment) run for real
# against a mutated ledger copy. Portable sed (no -i): the self-test also
# runs under Homebrew sed on macOS.
sub() { sed "$1" "$2" >"$2.tmp" && mv "$2.tmp" "$2"; }

cp "${ledger}" "${probe}/bad-test.yaml"
sub 's/TestProjectorDeadLetterGuardCellsLive/TestNoSuchQueueProof/' "${probe}/bad-test.yaml"
if REPO_ROOT="${repo_root}" LEDGER_PATH="${probe}/bad-test.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a row naming a missing Test func"
fi

cp "${ledger}" "${probe}/bad-file.yaml"
sub 's|projector_queue_dead_letter_live_test.go|no_such_queue_test.go|' "${probe}/bad-file.yaml"
if REPO_ROOT="${repo_root}" LEDGER_PATH="${probe}/bad-file.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a row naming a missing file"
fi

cp "${ledger}" "${probe}/bad-symbol.yaml"
printf '  noSuchQueueSymbolXYZ: claim:no-double-claim\n' >>"${probe}/bad-symbol.yaml"
if REPO_ROOT="${repo_root}" LEDGER_PATH="${probe}/bad-symbol.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a listed symbol absent from code"
fi

cp "${ledger}" "${probe}/bad-runner.yaml"
sub 's/runners: \[live-postgres-readiness\]/runners: [not-a-runner]/' "${probe}/bad-runner.yaml"
if REPO_ROOT="${repo_root}" LEDGER_PATH="${probe}/bad-runner.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with an unknown runner"
fi

cp "${ledger}" "${probe}/bad-class.yaml"
sub 's/class: live/class: eventually/' "${probe}/bad-class.yaml"
if REPO_ROOT="${repo_root}" LEDGER_PATH="${probe}/bad-class.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with an unknown class"
fi

cp "${ledger}" "${probe}/bad-tag.yaml"
sub 's/tag: ~/tag: live_queue_probe/' "${probe}/bad-tag.yaml"
if REPO_ROOT="${repo_root}" LEDGER_PATH="${probe}/bad-tag.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a build-tag mismatch"
fi

cp "${ledger}" "${probe}/bad-live-runner.yaml"
sub 's/runners: \[live-postgres-readiness\]/runners: [unit]/' "${probe}/bad-live-runner.yaml"
if REPO_ROOT="${repo_root}" LEDGER_PATH="${probe}/bad-live-runner.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a live row on a non-live runner"
fi

# The anchor reason carries no '#' or colon, so unquoting it is YAML-safe;
# appending ' #' then reproduces the #7425 truncation the validator rejects.
cp "${ledger}" "${probe}/bad-hash.yaml"
sub 's/^    reason: "Admission-timeout Fail/    reason: Admission-timeout Fail/' "${probe}/bad-hash.yaml"
sub 's/proved against a recording execer with no live Postgres"$/proved against a recording execer with no live Postgres #planted truncation/' "${probe}/bad-hash.yaml"
if REPO_ROOT="${repo_root}" LEDGER_PATH="${probe}/bad-hash.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with an unquoted '#' truncation"
fi
err="$(REPO_ROOT="${repo_root}" LEDGER_PATH="${probe}/bad-hash.yaml" "${script}" 2>&1)" || true
[[ "${err}" == *"unquoted '#'"* ]] || fail "truncation rejection names no cause: ${err}"

cp "${ledger}" "${probe}/bad-dupe.yaml"
python3 - "${probe}/bad-dupe.yaml" <<'PYEOF'
import sys
path = sys.argv[1]
text = open(path).read()
start = text.index("  - contract_ref: claim:no-double-claim")
end = text.index("\n\n", start)
block = text[start:end] + "\n"
anchor = "\nsymbols:\n"
assert text.count(anchor) == 1, "symbols anchor not found"
open(path, "w").write(text.replace(anchor, "\n" + block + anchor.lstrip("\n")))
PYEOF
if REPO_ROOT="${repo_root}" LEDGER_PATH="${probe}/bad-dupe.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a duplicate ledger row"
fi

cp "${ledger}" "${probe}/bad-rowref.yaml"
sub 's/^  claimProjectorWorkQuery: .*/  claimProjectorWorkQuery: claim:no-such-row/' "${probe}/bad-rowref.yaml"
if REPO_ROOT="${repo_root}" LEDGER_PATH="${probe}/bad-rowref.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a symbol mapped to an unknown row"
fi

grep -v '^  enqueueProjectorWorkQuery: ' "${ledger}" >"${probe}/bad-uncovered.yaml"
if REPO_ROOT="${repo_root}" LEDGER_PATH="${probe}/bad-uncovered.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a row mapping no covered symbol"
fi

cp "${ledger}" "${probe}/bad-unblock.yaml"
awk '!done && /^    blocking: true$/ { sub(/blocking: true/, "blocking: false"); done = 1 } { print }' \
	"${probe}/bad-unblock.yaml" >"${probe}/bad-unblock.tmp" && mv "${probe}/bad-unblock.tmp" "${probe}/bad-unblock.yaml"
if REPO_ROOT="${repo_root}" LEDGER_PATH="${probe}/bad-unblock.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a non-blocking row"
fi

# ── RED: a live row whose proving file lost its enrollment marker fails ───
# A fixture tree carries one proving file plus a one-row ledger, so the
# marker check runs without the full checkout. The ledger is built with
# printf, not a heredoc: the heredoc-budget gate caps heredoc bodies.
fixture="$(mktemp -d)"
trap 'rm -rf "${probe}" "${fixture}"' EXIT
mkdir -p "${fixture}/go/q"
cp "${repo_root}/go/internal/storage/postgres/projector_queue_dead_letter_live_test.go" \
	"${fixture}/go/q/dead_letter_live_test.go"
grep -v 'eshu:readiness live-postgres-readiness' \
	"${fixture}/go/q/dead_letter_live_test.go" >"${fixture}/go/q/stripped.go"
rm "${fixture}/go/q/dead_letter_live_test.go"
printf '%s\n' \
	'version: queue-contract-coverage/v1' \
	'updated_at: 2026-11-24' \
	'issue: 11354' \
	'owners:' \
	'  - storage' \
	'purpose: fixture' \
	'design: fixture' \
	'rows:' \
	'  - contract_ref: deadletter:replay-state' \
	'    surface: fixture projector dead-letter guard' \
	'    class: live' \
	'    runners: [live-postgres-readiness]' \
	'    blocking: true' \
	'    tag: ~' \
	'    reason: "fixture live proof"' \
	'    proving_tests:' \
	'      - file: go/q/stripped.go' \
	'        tests: [TestProjectorDeadLetterGuardCellsLive]' \
	'symbols:' \
	'  fixtureQueueSymbolQuery: deadletter:replay-state' \
	>"${fixture}/ledger.yaml"
printf 'package q\n\nconst fixtureQueueSymbolQuery = `SELECT 1`\n' >"${fixture}/go/q/symbols.go"
if REPO_ROOT="${fixture}" LEDGER_PATH="${fixture}/ledger.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed with a live row whose file lost its marker"
fi

# ── GREEN: the same fixture with the marker intact passes ─────────────────
cp "${repo_root}/go/internal/storage/postgres/projector_queue_dead_letter_live_test.go" \
	"${fixture}/go/q/dead_letter_live_test.go"
rm "${fixture}/go/q/stripped.go"
python3 - "${fixture}/ledger.yaml" <<'PYEOF'
import sys
path = sys.argv[1]
text = open(path).read()
old = "      - file: go/q/stripped.go"
assert text.count(old) == 1, "anchor file entry not found"
open(path, "w").write(text.replace(old, "      - file: go/q/dead_letter_live_test.go"))
PYEOF
if ! REPO_ROOT="${fixture}" LEDGER_PATH="${fixture}/ledger.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator failed on the intact-marker fixture tree"
fi

# ── RED: a fake row whose proving file touches live Postgres fails ───────
# A fake proof must never open live Postgres: `sql.Open(` or the readiness
# marker in its file is a planted violation the validator must reject.
fixture2="$(mktemp -d)"
trap 'rm -rf "${probe}" "${fixture}" "${fixture2}"' EXIT
mkdir -p "${fixture2}/go/q"
printf '%s\n' \
	'package q' \
	'' \
	'// sql.Open( would dial live Postgres: this fake proof is not isolated.' \
	'' \
	'func TestFakeQueueProofIsolatesLiveCalls() {}' \
	>"${fixture2}/go/q/fake_live_test.go"
printf '%s\n' \
	'const fixtureFakeProofQueryA = `SELECT 1`' \
	>"${fixture2}/go/q/symbols.go"
printf '%s\n' \
	'version: queue-contract-coverage/v1' \
	'updated_at: 2026-11-24' \
	'issue: 11354' \
	'owners:' \
	'  - storage' \
	'purpose: fixture' \
	'design: fixture' \
	'rows:' \
	'  - contract_ref: fake:proof-isolation' \
	'    surface: fixture fake proof isolation' \
	'    class: fake' \
	'    runners: [unit]' \
	'    blocking: true' \
	'    tag: ~' \
	'    reason: "fixture fake row with live touch"' \
	'    proving_tests:' \
	'      - file: go/q/fake_live_test.go' \
	'        tests: [TestFakeQueueProofIsolatesLiveCalls]' \
	'symbols:' \
	'  fixtureFakeProofQueryA: fake:proof-isolation' \
	>"${fixture2}/ledger-a.yaml"
if REPO_ROOT="${fixture2}" LEDGER_PATH="${fixture2}/ledger-a.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed a fake row whose file touches live Postgres"
fi

# ── RED: an env_gated row whose file has no DSN skip fails ───────────────
# An env_gated proof must skip without an operator DSN; a file with neither
# t.Skip( nor DSN would run live SQL unconditionally. The fake row above is
# dropped from this ledger so each RED case names exactly one violation.
printf '%s\n' \
	'package q' \
	'' \
	'func TestEnvGatedQueueProofRequiresSkip() {}' \
	>"${fixture2}/go/q/env_gated_test.go"
printf '%s\n' \
	'const fixtureFakeProofQueryB = `SELECT 2`' \
	>>"${fixture2}/go/q/symbols.go"
printf '%s\n' \
	'version: queue-contract-coverage/v1' \
	'updated_at: 2026-11-24' \
	'issue: 11354' \
	'owners:' \
	'  - storage' \
	'purpose: fixture' \
	'design: fixture' \
	'rows:' \
	'  - contract_ref: gated:proof-skip' \
	'    surface: fixture env_gated proof skip' \
	'    class: env_gated' \
	'    runners: [manual-dsn]' \
	'    blocking: true' \
	'    tag: ~' \
	'    reason: "fixture env_gated row with no skip"' \
	'    proving_tests:' \
	'      - file: go/q/env_gated_test.go' \
	'        tests: [TestEnvGatedQueueProofRequiresSkip]' \
	'symbols:' \
	'  fixtureFakeProofQueryB: gated:proof-skip' \
	>"${fixture2}/ledger-b.yaml"
if REPO_ROOT="${fixture2}" LEDGER_PATH="${fixture2}/ledger-b.yaml" "${script}" >/dev/null 2>&1; then
	fail "validator passed an env_gated row whose file has no DSN skip"
fi

printf 'test-verify-queue-contract-coverage: RED/GREEN pair passed\n'
