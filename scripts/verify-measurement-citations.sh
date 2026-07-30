#!/usr/bin/env bash
set -euo pipefail

# verify-measurement-citations: a diff that ADDS a line stating a measurement
# in trial/rate shape (e.g. "30/30 trials") or carrying an explicit
# "Measurement:" marker must cite a docs/internal/measurements.jsonl row id
# via a "ledger:<id>" token on the same line, and that id must exist in the
# ledger. See docs/internal/agent-guide.md#measurement-ledger for the schema,
# the exact patterns this gate matches, and what it deliberately does not
# catch.
#
# Base-commit resolution mirrors scripts/verify-performance-evidence.sh:
# explicit env override, then CI's PR base, then HEAD~1 locally.

repo_root="${ESHU_MEASUREMENT_CITATIONS_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

base="${ESHU_MEASUREMENT_CITATIONS_BASE:-}"
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  git -C "$repo_root" fetch --no-tags --depth=1 origin "$GITHUB_BASE_REF" >/dev/null 2>&1 || true
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base="origin/$GITHUB_BASE_REF"
  fi
fi
if [ -z "$base" ]; then
  if git -C "$repo_root" rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    base="HEAD~1"
  else
    printf 'verify-measurement-citations: no base commit available, skipping\n'
    exit 0
  fi
fi

ledger_rel_path="docs/internal/measurements.jsonl"
ledger_abs_path="${repo_root}/${ledger_rel_path}"

ledger_ids_path="$(mktemp "${TMPDIR:-/tmp}/eshu-measurement-ledger-ids.XXXXXX")"
diff_path="$(mktemp "${TMPDIR:-/tmp}/eshu-measurement-citations-diff.XXXXXX")"
cleanup() { rm -f "${ledger_ids_path}" "${diff_path}"; }
trap cleanup EXIT

if [ -f "${ledger_abs_path}" ]; then
  # Extract each row's top-level "id" value. The schema is intentionally flat
  # (see the agent guide), so no other field is named "id".
  rg -o '"id"[[:space:]]*:[[:space:]]*"[^"]+"' "${ledger_abs_path}" \
    | sed -E 's/.*"id"[[:space:]]*:[[:space:]]*"([^"]+)"/\1/' \
    >"${ledger_ids_path}" || true
fi

is_known_ledger_id() {
  local id="$1"
  [ -s "${ledger_ids_path}" ] || return 1
  rg -qxF -- "${id}" "${ledger_ids_path}"
}

if git -C "$repo_root" diff -U0 --no-color "$base"...HEAD >"${diff_path}" 2>/dev/null; then
  :
else
  git -C "$repo_root" diff -U0 --no-color "$base" HEAD >"${diff_path}"
fi

# Narrow by design: only these two shapes count as a "measurement-shaped
# claim" that needs a citation. See the agent guide for the rationale and the
# blind spots this leaves (bare durations, percentages, single-run counts,
# prose restating a number without "trials"/"runs"/"Measurement:").
claim_pattern='[0-9]+/[0-9]+[[:space:]]+(trials|runs)\b|Measurement:'
citation_pattern='ledger:([A-Za-z0-9][A-Za-z0-9_-]*)'

violations=()
current_file=""

# Read the pre-captured diff from a real file (not a heredoc or here-string):
# this repo's Homebrew bash 5.3.15 deadlocks on a `<<<` here-string feeding a
# while-read loop once the body crosses a few hundred bytes. A plain `< file`
# redirection never touches that pipe-buffer path.
while IFS= read -r line; do
  case "$line" in
    "+++ b/"*)
      current_file="${line#+++ b/}"
      continue
      ;;
    "+++ /dev/null")
      current_file=""
      continue
      ;;
    "--- "*)
      continue
      ;;
    "+"*)
      ;;
    *)
      continue
      ;;
  esac

  [ -n "${current_file}" ] || continue
  case "${current_file}" in
    "${ledger_rel_path}") continue ;;
    testdata/*|*/testdata/*) continue ;;
    # The gate's own script and test mirror necessarily contain the trigger
    # patterns as regex source and test fixtures (e.g. the claim_pattern
    # literal, or a fixture line like "0/30 trials failed"). Those are not
    # claims about Eshu's measured behavior, so they are exempt from citation.
    scripts/verify-measurement-citations.sh) continue ;;
    scripts/test-verify-measurement-citations.sh) continue ;;
  esac

  payload="${line:1}"
  printf '%s' "${payload}" | rg -q -e "${claim_pattern}" || continue

  citation="$(printf '%s' "${payload}" | rg -o "${citation_pattern}" -r '$1' | head -n1 || true)"
  if [ -z "${citation}" ]; then
    violations+=("${current_file}: added line states a measurement but cites no ledger row (ledger:<id>) -> ${payload}")
    continue
  fi
  if ! is_known_ledger_id "${citation}"; then
    violations+=("${current_file}: cites ledger id '${citation}', which is not in ${ledger_rel_path} -> ${payload}")
  fi
done <"${diff_path}"

if [ "${#violations[@]}" -eq 0 ]; then
  printf 'verify-measurement-citations: no uncited or unknown measurement claims found\n'
  exit 0
fi

{
  printf 'verify-measurement-citations: added lines state a measurement without a valid ledger citation.\n\n'
  for v in "${violations[@]}"; do
    printf '  - %s\n' "$v"
  done
  printf '\nAdd the measurement as a row in %s, then cite it inline with\n' "${ledger_rel_path}"
  printf '"ledger:<id>" on the same line, e.g.:\n\n'
  printf '  Measurement: 0/210 trials (ledger:5837-deadlock-plain-total)\n\n'
  printf 'This gate only recognizes "<N>/<M> trials", "<N>/<M> runs", and an explicit\n'
  printf '"Measurement:" marker. It does not catch every restated figure -- see\n'
  printf 'docs/internal/agent-guide.md#measurement-ledger for the documented blind spots.\n'
} >&2

exit 1
