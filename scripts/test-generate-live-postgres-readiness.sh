#!/usr/bin/env bash
# Hermetic RED/GREEN mirror for the live-postgres-readiness registration
# generator (#7859 P1): the shared registration surfaces derive from
# enrollment markers in the test files themselves, so adding a live test no
# longer hand-edits shared files. This mirror proves the derivation without
# Postgres, NornicDB, Docker, network, or a Go build: only bash, python3,
# git, and cp are used.
#
# Cases (each names its seed so a vacuous pass cannot hide):
#   green-clean   --check passes on the clean tree.
#   idempotent    generate twice into a scratch tree is cmp-clean.
#   stale-red     a marked but unregenerated test fails --check, then goes
#                 green after regeneration.
#   unregistered  a live test with no marker and no ledger row fails the
#                 chained ledger validator as unclassified.
#   orphan        a generated row naming a missing file fails the chained
#                 ledger validator, and --check flags the drift.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
generator="${repo_root}/scripts/generate-live-postgres-readiness.py"
ledger_validator="${repo_root}/scripts/verify-live-tests-ledger.sh"

fail() { printf 'test-generate-live-postgres-readiness: %s\n' "$*" >&2; exit 1; }

command -v python3 >/dev/null 2>&1 || fail "python3 is required"
command -v git >/dev/null 2>&1 || fail "git is required"
[[ -f "${generator}" ]] || fail "generator missing: ${generator}"
[[ -f "${ledger_validator}" ]] || fail "ledger validator missing: ${ledger_validator}"

# green-clean: the committed tree regenerates to itself.
out="$(python3 "${generator}" --check 2>&1)" || fail "clean tree --check failed: ${out}"
[[ "${out}" == *"registration current"* ]] || fail "clean tree acknowledgement missing: ${out}"
fragment_count=0
for fragment in "${repo_root}"/specs/live-tests.d/live-tests-readiness*.generated.yaml; do
  fragment_count=$((fragment_count + 1))
  lines="$(wc -l <"${fragment}")"
  (( lines < 500 )) || fail "${fragment##*/} exceeds the file cap: ${lines} lines"
done
[[ "${fragment_count}" == "4" ]] || fail "want four generated fragments, got ${fragment_count}"
REPO_ROOT="${repo_root}" python3 - <<'PY' || fail "generated proof rows differ from enrollment"
import os
import pathlib
import re
import sys

root = pathlib.Path(os.environ["REPO_ROOT"])
sys.path.insert(0, str(root / "scripts/lib"))
from live_postgres_readiness_gen_scan import collect
from live_postgres_readiness_gen_render import fragment_paths
from live_tests_registry import load_ledger_text

enrolled, bindings = collect(root)
expected = {path: row["reason"] for path, row in {**enrolled, **bindings}.items()}
actual = {}
for rel in fragment_paths():
    text = (root / rel).read_text(encoding="utf-8")
    for path, reason in re.findall(
            r'  - file: (\S+)\n    tag: ~\n    class: postgres_ci\n'
            r'    reason: "([^"]+)"\n    runner: live-postgres-readiness\n', text):
        if path in actual:
            raise SystemExit(f"duplicate generated proof: {path}")
        actual[path] = reason
if actual != expected:
    raise SystemExit(f"generated proof mismatch: actual={len(actual)} expected={len(expected)}")
flattened = load_ledger_text(root / "specs/live-tests.v1.yaml")
for path in actual:
    if flattened.count(f"  - file: {path}\n") != 1:
        raise SystemExit(f"flattened ledger missing or duplicates {path}")
print(f"generated proof rows: {len(actual)} exact paths and reasons")
PY

# Scratch tree: hardlinked go/ when the scratch shares the repo's
# filesystem (reads share inodes; added seed files stay in the scratch),
# else a full copy (cross-device /tmp, macOS cp without -l). Copied
# specs/scripts/workflow inputs and outputs, committed so the ledger
# validator's git ls-files sees the same universe.
scratch="$(mktemp -d)"
# #8673: keep fixture repos out of the host git-ai daemon's Trace2 tracking.
# The daemon keys on its Trace2 socket env; any git process that emits to it
# teaches the daemon the fixture path (fixtures and their gate runs inherit
# this env). GIT_TRACE2_EVENT=0 disables the event stream (same convention as
# test-verify-moved-file-refs.sh); PERF likewise. Teardown retries stay as
# belt-and-braces.
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
trap 'rm -rf "${scratch}"' EXIT
if ! cp -al "${repo_root}/go" "${scratch}/go" 2>/dev/null; then
  rm -rf "${scratch}/go"
  cp -r "${repo_root}/go" "${scratch}/go" ||
    fail "cannot stage a scratch go tree"
fi
# The chained ledger validator cross-checks sibling runners, workflows, and
# the registry, so stage the full scripts/specs/workflows inputs (go/ above
# is the only large tree; docs/ is unstaged — add it if a check needs it).
cp -r "${repo_root}/specs" "${scratch}/specs"
cp -r "${repo_root}/scripts" "${scratch}/scripts"
cp -r "${repo_root}/.github" "${scratch}/.github"
git -C "${scratch}" init -q
git -C "${scratch}" add -A
git -C "${scratch}" -c user.email=t@t -c user.name=t commit -qm seed
scratch_check() { python3 "${generator}" --root "${scratch}" --check 2>&1; }
[[ "$(scratch_check)" == *"registration current"* ]] ||
  fail "scratch tree is not green before seeding"

# Missing, duplicate, and orphan fragments must fail before any runner can
# silently lose a proof; regeneration removes only generator-owned orphans.
part4="${scratch}/specs/live-tests.d/live-tests-readiness-part4.generated.yaml"
mv "${part4}" "${part4}.saved"
out="$(scratch_check)" && fail "missing fragment passed --check"
[[ "${out}" == *"part4"* ]] || fail "missing fragment not named: ${out}"
mv "${part4}.saved" "${part4}"
manifest="${scratch}/specs/live-tests.v1.yaml"
cp "${manifest}" "${manifest}.saved"
printf '%s\n' '  - live-tests.d/live-tests-readiness-part4.generated.yaml' >>"${manifest}"
out="$(REPO_ROOT="${scratch}" LEDGER_PATH="${manifest}" bash "${ledger_validator}" 2>&1)" &&
  fail "duplicate fragment reference passed ledger validation"
[[ "${out}" == *"duplicate fragment reference"* ]] || fail "duplicate reference misclassified: ${out}"
mv "${manifest}.saved" "${manifest}"
orphan_part="${scratch}/specs/live-tests.d/live-tests-readiness-part5.generated.yaml"
cp "${part4}" "${orphan_part}"
out="$(scratch_check)" && fail "orphan generated fragment passed --check"
[[ "${out}" == *"orphan"* && "${out}" == *"part5"* ]] || fail "orphan fragment not named: ${out}"
python3 "${generator}" --root "${scratch}" >/dev/null || fail "orphan cleanup failed"
[[ ! -e "${orphan_part}" ]] || fail "orphan generated fragment survived regeneration"

REPO_ROOT="${repo_root}" python3 - <<'PY' || fail "boundary and cap seeds did not fail closed"
import os
import pathlib
import sys

root = pathlib.Path(os.environ["REPO_ROOT"])
sys.path.insert(0, str(root / "scripts/lib"))
from live_postgres_readiness_gen_scan import collect
from live_postgres_readiness_gen_render import FRAGMENT_BOUNDARIES, render_fragments

enrolled, bindings = collect(root)
missing = enrolled.copy()
missing.pop(FRAGMENT_BOUNDARIES[0])
try:
    render_fragments(missing, bindings)
except ValueError as error:
    if "boundary missing" not in str(error):
        raise
else:
    raise SystemExit("deleted boundary passed")
oversized = enrolled.copy()
for index in range(5):
    oversized[f"go/000-seeded-{index}_live_test.go"] = {"reason": "seeded cap proof"}
try:
    render_fragments(oversized, bindings)
except ValueError as error:
    if "add a lexical boundary" not in str(error):
        raise
else:
    raise SystemExit("oversized fragment passed")
print("boundary and oversized fragment seeds rejected")
PY

# idempotent: two consecutive generations are byte-identical.
python3 "${generator}" --root "${scratch}" >/dev/null ||
  fail "first scratch generation failed"
cp -r "${scratch}/specs" "${scratch}/specs-first"
cp -r "${scratch}/scripts" "${scratch}/scripts-first"
cp -r "${scratch}/.github" "${scratch}/github-first"
python3 "${generator}" --root "${scratch}" >/dev/null ||
  fail "second scratch generation failed"
diff -r "${scratch}/specs-first" "${scratch}/specs" >/dev/null ||
  fail "specs generation is not idempotent"
diff -r "${scratch}/scripts-first" "${scratch}/scripts" >/dev/null ||
  fail "scripts generation is not idempotent"
diff -r "${scratch}/github-first" "${scratch}/.github" >/dev/null ||
  fail "workflow generation is not idempotent"
rm -rf "${scratch}/specs-first" "${scratch}/scripts-first" "${scratch}/github-first"

# stale-red: a marked seed test with unregenerated outputs fails --check,
# then goes green after regeneration (the re-derive-on-rebase loop).
seed_dir="${scratch}/go/internal/storage/postgres"
seed_file="${seed_dir}/seeded_derivation_probe_live_test.go"
printf '%s\n' \
  '// SPDX-License-Identifier: MIT' \
  'package postgres_test' \
  '' \
  '//eshu:readiness live-postgres-readiness' \
  '//eshu:readiness:reason #7859 seeded derivation probe: proves a marked test with stale generated output fails the drift gate.' \
  '//eshu:readiness:env ESHU_POSTGRES_DSN' \
  'import "testing"' \
  '' \
  'func TestSeededDerivationProbeLive(t *testing.T) { t.Skip("seed") }' >"${seed_file}"
out="$(scratch_check)" && fail "stale marked seed passed --check"
[[ "${out}" == *"${seed_file##*/}"* || "${out}" == *"live-tests-readiness"* ]] ||
  fail "stale seed not named: ${out}"
python3 "${generator}" --root "${scratch}" >/dev/null ||
  fail "regeneration after seeding failed"
[[ "$(scratch_check)" == *"registration current"* ]] ||
  fail "regenerated scratch tree is not green"
git -C "${scratch}" add -A
git -C "${scratch}" -c user.email=t@t -c user.name=t commit -qm reseeded >/dev/null

# shard-stable: enrolling one file changes exactly one generated module
# (#9149: a file's shard comes from its own name and the committed boundary
# table, never from sibling sizes, so a new file cannot spill a neighbor
# into another part). The early-sorting three-test seed forces a greedy
# spill on the old fill; the boundary cut keeps it in part1 alone.
stable_seed="${seed_dir}/aaa_seed_stable_shard_probe_live_test.go"
printf '%s\n' \
  '// SPDX-License-Identifier: MIT' \
  'package postgres_test' \
  '' \
  '//eshu:readiness live-postgres-readiness' \
  '//eshu:readiness:reason #9149 seeded stable-shard probe: proves one enrollment changes one generated module.' \
  '//eshu:readiness:env ESHU_POSTGRES_DSN' \
  'import "testing"' \
  '' \
  'func TestAaaStableShardProbeOneLive(t *testing.T) { t.Skip("seed") }' \
  'func TestAaaStableShardProbeTwoLive(t *testing.T) { t.Skip("seed") }' \
  'func TestAaaStableShardProbeThreeLive(t *testing.T) { t.Skip("seed") }' >"${stable_seed}"
python3 "${generator}" --root "${scratch}" >/dev/null ||
  fail "regeneration after stable-shard seeding failed"
changed_modules="$(git -C "${scratch}" status --porcelain -- 'scripts/lib/live_postgres_readiness_inventory_gen_*.py')"
changed_count="$(printf '%s\n' "${changed_modules}" | rg -c . || true)"
[[ "${changed_count}" == "1" ]] ||
  fail "stable-shard seed changed ${changed_count} generated modules, want exactly 1:${changed_modules:+$'\n'}${changed_modules}"
[[ "${changed_modules}" == *"part1"* ]] ||
  fail "stable-shard seed did not land in part1: ${changed_modules}"
rm "${stable_seed}"
python3 "${generator}" --root "${scratch}" >/dev/null ||
  fail "regeneration after stable-shard seed removal failed"
[[ "$(scratch_check)" == *"registration current"* ]] ||
  fail "tree not green after stable-shard seed removal"

# shard-complete: every yaml-enrolled file is present in the assembled
# runner PACKAGES. Shards of one package must deep-merge: a shallow
# {**a, **b} assembler lets the last shard's table overwrite the earlier
# shards under their shared package key and silently drops most of the
# package from the runner (and from EXPECTED).
out="$(SCRATCH="${scratch}" PYTHONPATH="${scratch}/scripts/lib" python3 -c '
import os, re, sys
root = os.environ["SCRATCH"]
sys.path.insert(0, os.path.join(root, "scripts", "lib"))
from live_postgres_readiness_inventory_generated import PACKAGES
from live_postgres_readiness_inventory_helpers import HELPER_PROOFS
from live_postgres_readiness_gen_render import fragment_paths
yaml_text = "".join(open(os.path.join(root, rel)).read() for rel in fragment_paths())
enrolled = set(re.findall(r"- file: (\S+)", yaml_text))
covered = {path for files in PACKAGES.values() for path in files}
missing = sorted(enrolled - covered - set(HELPER_PROOFS))
if missing:
    print(f"shard-complete: {len(missing)} enrolled files missing from PACKAGES, first: {missing[:5]}")
    sys.exit(1)
print(f"shard-complete: {len(enrolled)} enrolled files all present in PACKAGES")
' 2>&1)" || fail "${out}"
[[ "${out}" == *"all present"* ]] || fail "shard-complete acknowledgement missing: ${out}"

# unregistered-red: a live test with no marker and no ledger row fails the
# chained ledger validator as unclassified (never silently unenrolled).
unreg_file="${seed_dir}/seeded_unregistered_probe_live_test.go"
printf '%s\n' \
  '// SPDX-License-Identifier: MIT' \
  'package postgres_test' \
  '' \
  'import "testing"' \
  '' \
  'func TestSeededUnregisteredProbeLive(t *testing.T) { t.Skip("seed") }' >"${unreg_file}"
out="$(REPO_ROOT="${scratch}" LEDGER_PATH="${scratch}/specs/live-tests.v1.yaml" \
  bash "${ledger_validator}" 2>&1)" && fail "unregistered seed passed the ledger validator"
[[ "${out}" == *"unclassified"* ]] || fail "unregistered seed misclassified: ${out}"
rm "${unreg_file}"

# orphan-red: a generated row naming a missing file fails the chained
# ledger validator, and --check flags the drift independently.
gen_fragment="${scratch}/specs/live-tests.d/live-tests-readiness.generated.yaml"
printf '%s\n' \
  '  - file: go/does/not-exist-orphan_live_test.go' \
  '    tag: ~' \
  '    class: postgres_ci' \
  '    reason: "seeded orphan"' \
  '    runner: live-postgres-readiness' >>"${gen_fragment}"
out="$(REPO_ROOT="${scratch}" LEDGER_PATH="${scratch}/specs/live-tests.v1.yaml" \
  bash "${ledger_validator}" 2>&1)" && fail "orphan seed passed the ledger validator"
[[ "${out}" == *"missing file"* ]] || fail "orphan seed misclassified: ${out}"
out="$(scratch_check)" && fail "orphan seed passed --check"
[[ "${out}" == *"live-tests-readiness.generated.yaml"* ]] || fail "orphan drift not named: ${out}"

# quote-red: a reason containing a double quote fails the scanner before any
# YAML renders (the generated reason scalar is double-quoted and unescaped).
quote_file="${seed_dir}/seeded_quote_probe_live_test.go"
printf '%s\n' \
  '// SPDX-License-Identifier: MIT' \
  'package postgres_test' \
  '' \
  '//eshu:readiness live-postgres-readiness' \
  '//eshu:readiness:reason #7859 seeded quote probe: proves a "quoted" reason fails the scanner.' \
  '//eshu:readiness:env ESHU_POSTGRES_DSN' \
  'import "testing"' \
  '' \
  'func TestSeededQuoteProbeLive(t *testing.T) { t.Skip("seed") }' >"${quote_file}"
out="$(python3 "${generator}" --root "${scratch}" 2>&1)" && fail "quoted reason passed the scanner"
[[ "${out}" == *"${quote_file##*/}"* ]] || fail "quoted reason not named: ${out}"
rm "${quote_file}"

printf 'test-generate-live-postgres-readiness: GREEN (exact rows, four capped fragments, idempotent, stable shards, missing/duplicate/orphan/boundary/oversize/stale/unregistered/quote RED)\n'
