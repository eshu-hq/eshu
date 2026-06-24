#!/usr/bin/env bash
set -euo pipefail

# scripts/test-verify-skill-roundtrip.sh is the test mirror for
# verify-skill-roundtrip.sh. It builds a temp "skillgen repo" (a temp
# skill-fragments/, expected/, specs/) and exercises the gate across
# the five failure modes plus the all-clean baseline:
#
#   1. all-clean         gen populates expected/; check passes
#   2. drift             one byte appended to an expected/ file; check fails with content_mismatch
#   3. missing-baseline  one expected/ file deleted; check fails with missing
#   4. missing-fragment  one skill-fragments/ file deleted; check re-renders and fails with content_mismatch
#   5. invalid-frontmatter fragment byte_citation is malformed; check fails to load the fragment
#
# The mirror pre-builds the skillgen binary once and reuses it across
# scenarios via the ESHU_SKILL_ROUNDTRIP_BIN env-var override so the
# five scenarios share a single Go build.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-skill-roundtrip.sh"
go_module_dir="${repo_root}/go"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

# Pre-build the binary once and reuse it across scenarios.
bin_path="${tmp_root}/skillgen"
(cd "$go_module_dir" && go build -o "$bin_path" ./cmd/skillgen/...)

# Initialize a temp skillgen repo: skill-fragments/, expected/, specs/.
# Runs `gen` against the temp repo to populate the baseline expected/.
# Prints the temp dir path on stdout for the caller to capture.
init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}/skill-fragments" "${dir}/expected" "${dir}/specs"

  # The seven S1 fragment ids with minimal valid frontmatter. The
  # byte_citation points at a fixture file the test mirror writes below
  # so the citation target exists in the temp repo.
  for id in operating-standard truth-labels capability-profiles reducer-invariant local-first bundle-reproduction per-collector-matrix; do
    cat >"${dir}/skill-fragments/${id}.md" <<MD
---
id: ${id}
version: 1.0.0
byte_citation: ${dir}/specs/fixture-citations.md#1-1
description: |
  Test fragment ${id}.
---

# ${id} body
MD
  done

  # The editorial overlay catalog for the test repo: four implemented
  # collectors and one partial row, so capability-aware rendering can
  # exercise both states without depending on the real catalog.
  cat >"${dir}/specs/surface-inventory.v1.yaml" <<YAML
version: v1
surfaces:
  - category: collector
    name: git
    readiness: implemented
  - category: collector
    name: documentation
    readiness: implemented
  - category: collector
    name: oci_registry
    readiness: implemented
  - category: collector
    name: aws
    readiness: implemented
  - category: collector
    name: gcp
    readiness: partial
YAML

  # The byte_citation target file. The S2 byte-citation validator only
  # parses the format; it does not walk the file system. The file
  # exists so the citation anchor is well-formed.
  printf '# fixture citations\n' >"${dir}/specs/fixture-citations.md"

  # Run gen to populate the expected/ directory with the canonical
  # baseline for this fixture repo.
  "${bin_path}" gen \
    -fragments "${dir}/skill-fragments" \
    -expected "${dir}/expected" \
    -catalog "${dir}/specs/surface-inventory.v1.yaml" >/dev/null

  printf '%s\n' "${dir}"
}

run_gate() {
  local dir="$1"
  ESHU_SKILL_ROUNDTRIP_BIN="${bin_path}" \
  ESHU_SKILL_ROUNDTRIP_REPO_ROOT="${dir}" \
  ESHU_SKILL_ROUNDTRIP_FRAGMENTS="${dir}/skill-fragments" \
  ESHU_SKILL_ROUNDTRIP_EXPECTED="${dir}/expected" \
  ESHU_SKILL_ROUNDTRIP_CATALOG="${dir}/specs/surface-inventory.v1.yaml" \
    "${verifier}" >/tmp/eshu-skill-roundtrip.out 2>/tmp/eshu-skill-roundtrip.err
}

expect_pass() {
  local name="$1"
  local dir="$2"
  if ! run_gate "${dir}"; then
    printf 'expected gate to pass in %s\n' "${name}" >&2
    sed -n '1,80p' /tmp/eshu-skill-roundtrip.err >&2
    sed -n '1,80p' /tmp/eshu-skill-roundtrip.out >&2
    exit 1
  fi
}

expect_fail() {
  local name="$1"
  local dir="$2"
  local needle="${3:-drifted}"
  if run_gate "${dir}"; then
    printf 'expected gate to fail in %s\n' "${name}" >&2
    sed -n '1,80p' /tmp/eshu-skill-roundtrip.out >&2
    exit 1
  fi
  if ! rg -q "${needle}" /tmp/eshu-skill-roundtrip.out /tmp/eshu-skill-roundtrip.err; then
    printf 'expected gate output to mention %q in %s\n' "${needle}" "${name}" >&2
    sed -n '1,80p' /tmp/eshu-skill-roundtrip.out >&2
    sed -n '1,80p' /tmp/eshu-skill-roundtrip.err >&2
    exit 1
  fi
}

# Scenario 1: all-clean baseline. Run gen, then check exits 0.
clean_repo="$(init_repo all-clean)"
expect_pass all-clean "${clean_repo}"

# Scenario 2: drift. Run gen, then hand-edit one byte in expected/,
# then check exits non-zero with content_mismatch in the report.
drift_repo="$(init_repo drift)"
claude_file="${drift_repo}/expected/claude-code/.claude/skills/eshu/SKILL.md"
printf 'garbage\n' >>"${claude_file}"
expect_fail drift "${drift_repo}" "content_mismatch"

# Scenario 3: missing baseline. Run gen, then delete one expected/
# file; check exits non-zero with missing in the report.
missing_repo="$(init_repo missing-baseline)"
rm "${missing_repo}/expected/claude-code/.claude/skills/eshu/SKILL.md"
expect_fail missing-baseline "${missing_repo}" "missing"

# Scenario 4: missing fragment. Run gen, then delete one
# skill-fragments/ file; check re-renders without it and exits
# non-zero with content_mismatch for the hosts whose baseline
# referenced the deleted fragment.
frag_repo="$(init_repo missing-fragment)"
rm "${frag_repo}/skill-fragments/operating-standard.md"
expect_fail missing-fragment "${frag_repo}" "content_mismatch"

# Scenario 5: invalid frontmatter. Run gen, then replace one
# fragment's byte_citation with a malformed anchor (path#a-b, where
# a/b are not positive integers); check fails to load the fragment
# and exits non-zero with byte citation in the error.
badfm_repo="$(init_repo invalid-frontmatter)"
cat >"${badfm_repo}/skill-fragments/operating-standard.md" <<MD
---
id: operating-standard
version: 1.0.0
byte_citation: path#a-b
description: |
  Test fragment with malformed citation.
---

# operating-standard body
MD
expect_fail invalid-frontmatter "${badfm_repo}" "byte citation"

printf 'verify-skill-roundtrip tests passed\n'
