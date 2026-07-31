#!/usr/bin/env bash
# verify-root-cause-evidence: an evidence doc that asserts WHY something broke
# must also record WHAT WAS OBSERVED that establishes it.
#
# The failure this exists to catch is not sloppy prose. It is a diagnosis
# written with the confidence of a finding when it is actually a guess, which
# then gets built on: an executor is dispatched, a fix is written, and the fix
# does nothing because the cause was never established. The repo already
# mandates proving a theory before implementing (CLAUDE.md, "Mandatory
# Prove-The-Theory-First"); this gate is the mechanical half for the documents
# that record such claims.
#
# Deliberate limits, stated so nobody mistakes a pass for a verified diagnosis:
#
#   * It cannot tell a real observation from a confident sentence typed after
#     the marker. It checks that evidence was WRITTEN DOWN, exactly as
#     verify-performance-evidence.sh checks a benchmark marker exists without
#     verifying the numbers.
#   * It only reads files. A cause asserted in a PR description, a review
#     comment, or chat is out of reach.
#   * It matches a deliberately narrow set of causal phrases. Prose can always
#     assert a cause in words this misses.
#
# What it does buy: an author who writes "the root cause is X" is forced to
# stop and answer "what did I see?" in the same edit, where a reviewer can
# read both together.
set -euo pipefail

repo_root="${ESHU_ROOT_CAUSE_EVIDENCE_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

# Same base resolution as verify-performance-evidence.sh, including the
# explicit destination refspec: `git fetch origin <branch>` without `:<dst>`
# only updates FETCH_HEAD, so under a shallow CI checkout origin/<base> never
# resolves and the base silently degrades to HEAD~1 -- the last commit only,
# which under-covers every multi-commit branch (eshu-hq/eshu#5542).
base="${ESHU_ROOT_CAUSE_EVIDENCE_BASE:-}"
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  git -C "$repo_root" fetch --no-tags --depth=1 origin \
    "$GITHUB_BASE_REF:refs/remotes/origin/$GITHUB_BASE_REF" >/dev/null 2>&1 || true
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base="origin/$GITHUB_BASE_REF"
  fi
fi
if [ -z "$base" ]; then
  if git -C "$repo_root" rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    base="HEAD~1"
  else
    printf 'verify-root-cause-evidence: no base commit available, skipping\n'
    exit 0
  fi
fi

diff_cache=""
if diff_cache="$(git -C "$repo_root" diff --unified=0 "$base"...HEAD 2>/dev/null)"; then
  :
else
  diff_cache="$(git -C "$repo_root" diff --unified=0 "$base" HEAD 2>/dev/null || true)"
fi

if [ -z "$diff_cache" ]; then
  printf 'verify-root-cause-evidence: no diff against %s, skipping\n' "$base"
  exit 0
fi

# Scoped to internal evidence docs on purpose. These are the files whose whole
# job is recording why something broke, so a causal claim in one is load
# bearing. Public reference docs and package READMEs describe how things work
# rather than diagnosing incidents, and sweeping them in would flag ordinary
# explanatory prose ("the index exists because lookups were slow") with no
# diagnosis behind it to check.
is_scoped_evidence_file() {
  local path="$1"
  case "$path" in
    testdata/*|*/testdata/*|vendor/*|*/vendor/*|generated/*|*/generated/*) return 1 ;;
  esac
  case "$path" in
    docs/internal/evidence/*.md) return 0 ;;
    docs/internal/evidence/**/*.md) return 0 ;;
    *) return 1 ;;
  esac
}

added_lines_for_file() {
  local rel="$1"
  printf '%s\n' "$diff_cache" | awk -v target="$rel" '
    substr($0, 1, 6) == "+++ b/" { cur = substr($0, 7); next }
    $0 == "+++ /dev/null" { cur = ""; next }
    substr($0, 1, 6) == "--- a/" { next }
    $0 == "--- /dev/null" { next }
    substr($0, 1, 1) == "+" { if (cur == target) print substr($0, 2); next }
  '
}

changed_files=()
while IFS= read -r file; do
  [ -n "$file" ] && changed_files+=("$file")
done < <(printf '%s\n' "$diff_cache" \
  | awk 'substr($0, 1, 6) == "+++ b/" { print substr($0, 7) }' | sort -u)

# Narrow by design. Every phrase here asserts that a cause is KNOWN. Hedged
# forms ("might be caused by", "suspected cause", "one theory is") are
# deliberately absent -- flagging those would punish exactly the honest
# labelling this gate is meant to encourage.
causal_pattern='(^|[^[:alnum:]])(root cause|root-caused|the cause (is|was)|caused by|is caused|the culprit|traced (it )?to|the bug (is|was)|the reason (is|was))([^[:alnum:]]|$)'

# The marker tolerates one parenthetical or bracketed qualifier before the
# colon -- "Root-Cause Evidence (#5717):" -- matching the convention already
# established by the performance markers. The colon stays mandatory, so a
# passing mention of the phrase in prose does not satisfy the gate.
marker_pattern='(^|[[:space:]])Root-Cause Evidence([[:space:]]*(\([^()]*\)|\[[^\[\]]*\]))?[[:space:]]*:'

# A marker with nothing substantive after it is not evidence. This is a real
# check the performance gate does not make, and it is the cheapest defence
# against "Root-Cause Evidence: confirmed".
min_evidence_chars=40

offenders=()
thin_markers=()

for file in "${changed_files[@]}"; do
  is_scoped_evidence_file "$file" || continue

  added="$(added_lines_for_file "$file")"
  [ -z "$added" ] && continue

  printf '%s\n' "$added" | rg -qi -e "$causal_pattern" || continue

  marker_lines="$(printf '%s\n' "$added" | rg -e "$marker_pattern" || true)"
  if [ -z "$marker_lines" ]; then
    offenders+=("$file")
    continue
  fi

  # Longest text after any marker colon on this file's added lines. Judging by
  # the longest rather than the first lets an author write a short pointer line
  # and the real evidence in a following marker without tripping the check.
  longest=0
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    tail_text="${line#*:}"
    tail_text="$(printf '%s' "$tail_text" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
    len="${#tail_text}"
    [ "$len" -gt "$longest" ] && longest="$len"
  done <<<"$marker_lines"

  if [ "$longest" -lt "$min_evidence_chars" ]; then
    thin_markers+=("$file (longest marker body: ${longest} chars, want >= ${min_evidence_chars})")
  fi
done

if [ "${#offenders[@]}" -eq 0 ] && [ "${#thin_markers[@]}" -eq 0 ]; then
  printf 'verify-root-cause-evidence: causal claims in evidence docs carry observed evidence\n'
  exit 0
fi

printf 'verify-root-cause-evidence: causal claim without recorded evidence\n\n'

if [ "${#offenders[@]}" -gt 0 ]; then
  printf 'These files assert a cause on added lines but record no observation:\n'
  for file in "${offenders[@]}"; do
    printf '  - %s\n' "$file"
  done
  printf '\n'
fi

if [ "${#thin_markers[@]}" -gt 0 ]; then
  printf 'These carry a marker with too little after it to be evidence:\n'
  for entry in "${thin_markers[@]}"; do
    printf '  - %s\n' "$entry"
  done
  printf '\n'
fi

printf 'Add a line naming what you OBSERVED, not what you concluded:\n\n'
printf '  Root-Cause Evidence: reproduced 3 of 4 runs on the same commit; the\n'
printf '  node is present in all four while the edge count is 0 in three.\n\n'
printf 'If the cause is not actually established, say so and the gate stops\n'
printf 'applying -- it only matches claims that a cause is KNOWN. Hedged wording\n'
printf '("the suspected cause", "one theory is") is deliberately unmatched,\n'
printf 'because labelling a guess as a guess is what this encourages.\n\n'
printf 'See CLAUDE.md, "Mandatory Prove-The-Theory-First".\n'

# Advisory while the real false-positive rate on this repo's prose is unknown.
# Flip to exit 1 in specs/ci-gates.v1.yaml (blocking: true) once the rate is
# measured rather than guessed -- which is the same standard this gate asks of
# everyone else.
exit 0
