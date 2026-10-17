#!/usr/bin/env bash
# Hermetic RED/GREEN mirror for the live-postgres-readiness registration
# generator (#7859 P1): the six shared registration surfaces derive from
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

# Scratch tree: hardlinked go/ when the scratch shares the repo's
# filesystem (reads share inodes; added seed files stay in the scratch),
# else a full copy (cross-device /tmp, macOS cp without -l). Copied
# specs/scripts/workflow inputs and outputs, committed so the ledger
# validator's git ls-files sees the same universe.
scratch="$(mktemp -d)"
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

# idempotent: two consecutive generations are byte-identical.
python3 "${generator}" --root "${scratch}" >/dev/null ||
  fail "first scratch generation failed"
find "${scratch}/specs" "${scratch}/scripts" "${scratch}/.github" -type f | sort >"${scratch}/first.txt"
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
[[ "${out}" == *"${seed_file##*/}"* || "${out}" == *"live-tests-readiness.generated.yaml"* ]] ||
  fail "stale seed not named: ${out}"
python3 "${generator}" --root "${scratch}" >/dev/null ||
  fail "regeneration after seeding failed"
[[ "$(scratch_check)" == *"registration current"* ]] ||
  fail "regenerated scratch tree is not green"
git -C "${scratch}" add -A
git -C "${scratch}" -c user.email=t@t -c user.name=t commit -qm reseeded >/dev/null

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

printf 'test-generate-live-postgres-readiness: GREEN (clean, idempotent, stale/unregistered/orphan/quote RED)\n'
