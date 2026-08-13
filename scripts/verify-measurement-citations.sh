#!/usr/bin/env bash
set -euo pipefail

# verify-measurement-citations: a diff that ADDS a line stating a measurement
# in trial/rate shape (e.g. "30/30 trials") or carrying an explicit
# "Measurement:" marker must cite a docs/internal/measurements.jsonl row id
# via a "ledger:<id>" token on the same line, and that id must exist in the
# ledger AND the cited row's own value/trials must agree with the claimed
# figure -- citing a real row for the WRONG number is worse than no gate,
# because the citation carries false authority a reader stops checking. The
# ledger itself must also stay append-only: a later commit that edits or
# deletes an existing row is rejected even if it adds no new prose claim.
# See docs/internal/measurement-ledger.md for the schema, the exact patterns
# this gate matches, and what it deliberately does not catch.
#
# Base-commit resolution mirrors scripts/verify-performance-evidence.sh:
# explicit env override, then CI's PR base, then the merge base with
# origin/main locally, with HEAD~1 only as a last resort.

repo_root="${ESHU_MEASUREMENT_CITATIONS_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  # Derive the repo root from the script's own location, NOT
  # `git rev-parse --show-toplevel`. Git hooks (pre-commit/pre-push) export
  # GIT_DIR, and with GIT_DIR set `git -C scripts rev-parse --show-toplevel`
  # returns the -C directory (<repo>/scripts) instead of the repo root --
  # confirmed live: a real `git push` (which exports GIT_DIR to the hook
  # process tree) resolved repo_root to .../scripts, so the ledger path
  # ("${repo_root}/docs/internal/measurements.jsonl") pointed at a path that
  # does not exist, silently emptying the extracted ledger-id index and
  # reporting a real, present citation as unknown. A manual invocation of the
  # same hook script (no GIT_DIR exported) always passed, which is what made
  # this non-reproducible outside a real push. The script always lives at
  # <repo>/scripts/, so dirname/.. is the repo root and is both worktree- and
  # hook-safe. verify-telemetry-coverage.sh carries the identical fix for the
  # identical bug; this adopts that fix rather than repeating the
  # misdiagnosis.
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi

base="${ESHU_MEASUREMENT_CITATIONS_BASE:-}"
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  # An explicit destination refspec is required: `git fetch origin <branch>`
  # with no `:<dst>` only ever updates FETCH_HEAD, never
  # refs/remotes/origin/<branch> -- real git behavior independent of any
  # configured remote.origin.fetch. Under actions/checkout@v5's narrow setup
  # `origin/$GITHUB_BASE_REF` then never resolved and `base` fell through to
  # HEAD~1, so CI scanned only the last commit of a multi-commit PR and passed
  # green on everything before it. verify-performance-evidence.sh carried the
  # identical bug until #5869; this adopts that fix rather than repeating it.
  git -C "$repo_root" fetch --no-tags --depth=1 origin \
    "$GITHUB_BASE_REF:refs/remotes/origin/$GITHUB_BASE_REF" >/dev/null 2>&1 || true
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base="origin/$GITHUB_BASE_REF"
  fi
fi
# Fall back to the merge base with origin/main, NOT HEAD~1 -- the same trap
# fixed in verify-performance-evidence.sh and verify-root-cause-evidence.sh. A
# HEAD~1 default scopes the gate to the last commit alone, so an uncited
# measurement claim added in an earlier commit of a multi-commit branch escapes
# whenever the tip commit is innocuous. scripts/dev/precommit-go.sh pins
# origin/main for its own call, but a direct invocation gets this default.
if [ -z "$base" ]; then
  if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
    merge_base="$(git -C "$repo_root" merge-base origin/main HEAD 2>/dev/null || true)"
    # A merge base equal to HEAD means the branch adds no commits of its own,
    # so the window would be empty -- narrower than HEAD~1. Leave base unset.
    if [ -n "$merge_base" ] &&
      [ "$merge_base" != "$(git -C "$repo_root" rev-parse HEAD 2>/dev/null)" ]; then
      base="$merge_base"
    fi
  fi
fi
# Last resort only: shallow clone, no origin remote, or a fresh fixture repo.
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
old_ledger_path="$(mktemp "${TMPDIR:-/tmp}/eshu-measurement-ledger-old.XXXXXX")"
missing_or_changed_path="$(mktemp "${TMPDIR:-/tmp}/eshu-measurement-ledger-missing.XXXXXX")"
cleanup() { rm -f "${ledger_ids_path}" "${diff_path}" "${old_ledger_path}" "${missing_or_changed_path}"; }
trap cleanup EXIT

if [ -f "${ledger_abs_path}" ]; then
  # Extract each row's top-level "id" value. The schema is intentionally flat
  # (see the agent guide), so no other field is named "id". rg exits 1 for a
  # ledger with zero id rows -- a legitimate empty state -- but exit 2+ means a
  # real read/write failure (a bad regex, an unreadable file, or the redirect
  # itself failing under disk pressure). A silently truncated or empty
  # extraction here makes every valid citation in the diff look unknown, which
  # is a false NEGATIVE this gate must never produce quietly: it would report
  # "not in ledger" against a row that is actually present. Swallowing only
  # exit 1 keeps the "no rows yet" case working without hiding a genuine I/O
  # failure behind a misleading citation error.
  extract_status=0
  rg -o '"id"[[:space:]]*:[[:space:]]*"[^"]+"' "${ledger_abs_path}" \
    | sed -E 's/.*"id"[[:space:]]*:[[:space:]]*"([^"]+)"/\1/' \
    >"${ledger_ids_path}" || extract_status=$?
  if [ "${extract_status}" -gt 1 ]; then
    printf 'verify-measurement-citations: failed to read %s (exit %s); refusing to report an unknown citation against a possibly-truncated ledger index\n' \
      "${ledger_rel_path}" "${extract_status}" >&2
    exit 2
  fi
fi

is_known_ledger_id() {
  local id="$1"
  [ -s "${ledger_ids_path}" ] || return 1
  rg -qxF -- "${id}" "${ledger_ids_path}"
}

# ledger_row_line prints the raw JSON line for ledger row id $1, or nothing
# if the id is absent (callers only use this after is_known_ledger_id has
# already confirmed the id exists).
ledger_row_line() {
  local id="$1"
  [ -f "${ledger_abs_path}" ] || return 0
  # `|| true`: under `set -o pipefail`, a no-match `rg` here would otherwise
  # make this function's own return status non-zero even though `head`
  # succeeds, aborting the caller's `row_line="$(ledger_row_line ...)"`
  # under `set -e` instead of yielding an empty (not-found) result. See
  # extract_ratio's comment for the same failure shape.
  rg -F -- "\"id\":\"${id}\"" "${ledger_abs_path}" | head -n1 || true
}

# ledger_row_field prints field $2's raw JSON value (a bare number or the
# literal `null`) from row-JSON line $1, or nothing if the field is absent.
ledger_row_field() {
  local row="$1" field="$2"
  # `|| true` for the same pipefail reason as extract_ratio and
  # ledger_row_line: a field genuinely absent from $1 must yield an empty
  # result, not abort the script.
  printf '%s' "${row}" \
    | rg -o "\"${field}\"[[:space:]]*:[[:space:]]*(-?[0-9]+(\\.[0-9]+)?|null)" \
    | sed -E "s/.*\"${field}\"[[:space:]]*:[[:space:]]*//" \
    || true
}

violations=()

# The ledger's own schema doc calls it append-only: existing rows are never
# edited or renumbered, because a citation's whole value depends on the row
# it points at staying exactly what it was when the citation was written. The
# scan below only ever inspects ADDED lines, and the ledger file itself is
# skipped by that scan (a new row is not a "claim" needing a citation) -- so
# nothing previously noticed a later commit silently editing or deleting an
# existing row's line while adding no new measurement-shaped prose. Compare
# every row present at the diff base against the current ledger by exact
# line content; a missing or changed line is a violation regardless of
# whether anything else in the diff would otherwise trip the gate.
#
# `comm` does that comparison in a single pass (two `sort`s plus one `comm`,
# independent of ledger size) instead of one `rg` subprocess spawn per
# historical row. The ledger only grows over its lifetime, so an
# unmeasured per-row-subprocess design would get linearly slower on every
# push forever; this keeps the common case (nothing changed) at a small
# constant number of subprocesses, and pays the per-row `rg` lookup below
# only for rows `comm` actually flags as missing or changed -- which should
# be zero on almost every push.
if git -C "${repo_root}" show "${base}:${ledger_rel_path}" >"${old_ledger_path}" 2>/dev/null; then
  if [ -f "${ledger_abs_path}" ]; then
    comm -23 <(sort "${old_ledger_path}") <(sort "${ledger_abs_path}") >"${missing_or_changed_path}" || true
  else
    sort "${old_ledger_path}" >"${missing_or_changed_path}"
  fi
  while IFS= read -r old_line; do
    [ -n "${old_line}" ] || continue
    old_id="$(printf '%s' "${old_line}" \
      | rg -o '"id"[[:space:]]*:[[:space:]]*"[^"]+"' \
      | sed -E 's/.*"id"[[:space:]]*:[[:space:]]*"([^"]+)"/\1/' || true)"
    [ -n "${old_id}" ] || continue
    if [ -f "${ledger_abs_path}" ] && rg -qF -- "\"id\":\"${old_id}\"" "${ledger_abs_path}"; then
      violations+=("${ledger_rel_path}: row '${old_id}' was modified; the ledger is append-only -- add a new row instead of editing an existing one")
    else
      violations+=("${ledger_rel_path}: row '${old_id}' was deleted; the ledger is append-only and existing rows must never be removed")
    fi
  done <"${missing_or_changed_path}"
fi

if git -C "$repo_root" diff -U0 --no-color "$base"...HEAD >"${diff_path}" 2>/dev/null; then
  :
else
  git -C "$repo_root" diff -U0 --no-color "$base" HEAD >"${diff_path}"
fi

# Narrow by design: only these two shapes count as a "measurement-shaped
# claim" that needs a citation. See the agent guide for the rationale and the
# blind spots this leaves (bare durations, percentages, single-run counts,
# prose restating a number without "trials"/"runs"/"Measurement:").
#
# The "Measurement:" trigger is PROSE-ONLY, deliberately: it matches a line
# starting (after optional indentation) with the bare word "Measurement:",
# never a Markdown heading ("# Measurement:", "## Measurement:", ...) or a
# source-code comment marker ("# Measurement:", "// Measurement:", "--
# Measurement:", ...). An earlier version of this pattern accepted exactly
# one optional leading "#" and not two or more -- an artifact of how the
# regex was first written, not a real decision, and unconnected to any actual
# usage: every existing "Measurement:" marker in this repo
# (go/internal/storage/cypher/evidence-3559-reconciliation.md,
# go/internal/reducer/evidence-3617-atomic-publish-fence.md) is mid-sentence
# prose, not a heading or a comment. Comment/heading markers are excluded on
# purpose, not merely unhandled -- see the Citation gate section of
# docs/internal/measurement-ledger.md for the full included/excluded set.
claim_pattern='[0-9]+/[0-9]+[[:space:]]+(trials|runs)\b|^[[:space:]]*Measurement:'
citation_pattern='ledger:([A-Za-z0-9][A-Za-z0-9_-]*)'

# extract_ratio prints "<N> <M>" for the FIRST "<N>/<M> trials|runs" match in
# $1, or nothing if the line has no such match (e.g. a "Measurement:" line
# whose figure isn't in that shape).
extract_ratio() {
  # The trailing `|| true` is load-bearing under `set -o pipefail`: when $1
  # has no ratio-shaped match, the inner `rg -o` stages legitimately exit 1
  # (no match), and pipefail then makes the whole pipeline's status
  # non-zero even though `sed` itself succeeds -- which, under `set -e`,
  # would abort the entire script from inside a command substitution
  # (`ratio="$(extract_ratio ...)"`) instead of just yielding an empty
  # result, the correct outcome for "this line has no ratio to check".
  printf '%s' "$1" \
    | rg -o '[0-9]+/[0-9]+[[:space:]]+(trials|runs)\b' \
    | head -n1 \
    | rg -o '^[0-9]+/[0-9]+' \
    | sed -E 's#([0-9]+)/([0-9]+)#\1 \2#' \
    || true
}

current_file=""
# in_body tracks whether the reader is inside a file section's diff BODY
# (context/added/removed lines, each unconditionally prefixed by ' '/'+'/'-')
# or still in that section's HEADER block (diff --git/index/mode/---/+++,
# never prefixed by +/-/space). current_file is set ONLY from a line that
# starts with the literal, unforgeable "diff --git a/" -- never from
# pattern-matching "+++ b/" anywhere in the stream. An ADDED line whose own
# CONTENT happens to read "++ b/scripts/verify-measurement-citations.sh"
# renders in the diff as "+++ b/scripts/verify-measurement-citations.sh",
# indistinguishable BY CONTENT from a genuine file-header line -- content
# matching on "+++ b/" would let that forged line switch current_file to the
# exempt gate script and silently exempt every claim after it. It cannot
# forge "diff --git a/", because a "+"-prefixed body line's raw text always
# starts with "+", never with the bare word "diff". Everything else in a
# header block is likewise recognized purely by POSITION (in_body==0, before
# the section's "@@ " hunk marker), never by re-matching its content.
in_body=0

# Read the pre-captured diff from a real file (not a heredoc or here-string):
# this repo's Homebrew bash 5.3.15 deadlocks on a `<<<` here-string feeding a
# while-read loop once the body crosses a few hundred bytes. A plain `< file`
# redirection never touches that pipe-buffer path.
while IFS= read -r line; do
  case "$line" in
    "diff --git a/"*)
      rest="${line#diff --git a/}"
      current_file="${rest##* b/}"
      in_body=0
      continue
      ;;
  esac

  if [ "${in_body}" -eq 0 ]; then
    case "$line" in
      "@@ "*)
        in_body=1
        continue
        ;;
      *)
        # Header/metadata line for the current section (index, mode,
        # similarity/rename, ---, +++, or a blank separator) -- never body
        # content, regardless of what it says.
        continue
        ;;
    esac
  fi

  case "$line" in
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
    continue
  fi

  # Membership alone does not prove the claim is CORRECT: citing a real row
  # for the wrong number is the exact drift the ledger exists to prevent, and
  # it is worse than no citation at all, because the reader sees "(ledger:...)"
  # and stops checking. Verify the claimed figure against the cited row's own
  # value/trials wherever the claim's shape is comparable.
  row_line="$(ledger_row_line "${citation}")"
  ratio="$(extract_ratio "${payload}")"
  if [ -n "${ratio}" ]; then
    claimed_value="${ratio%% *}"
    claimed_trials="${ratio##* }"
    row_value="$(ledger_row_field "${row_line}" value)"
    row_trials="$(ledger_row_field "${row_line}" trials)"
    if [ "${claimed_value}" != "${row_value}" ] || [ "${claimed_trials}" != "${row_trials}" ]; then
      violations+=("${current_file}: cites ledger:${citation} for '${claimed_value}/${claimed_trials}', but that row states value=${row_value:-<missing>} trials=${row_trials:-<missing>} -> ${payload}")
    fi
  else
    # A "Measurement:" line with no "<N>/<M> trials|runs" ratio to check
    # against the row's structured fields. This gate cannot verify an
    # arbitrary duration, percentage, or count against value/unit, so it
    # requires the prose to drop the figure rather than let an unverifiable
    # number pass silently -- see docs/internal/measurement-ledger.md's
    # Citation gate blind spots. Strip the citation token (parenthesized or
    # not) before checking for a leftover digit, so the ledger id's own
    # digits never trigger a false positive.
    marker_figure="${payload#*Measurement:}"
    marker_figure="${marker_figure%%(*}"
    marker_figure="$(printf '%s' "${marker_figure}" | sed -E 's/ledger:[A-Za-z0-9_-]+//g')"
    if printf '%s' "${marker_figure}" | rg -q '[0-9]'; then
      violations+=("${current_file}: Measurement: line restates a figure this gate cannot verify against ledger:${citation} (only \"<N>/<M> trials\"/\"<N>/<M> runs\" is checked); drop the number and keep only the citation, or use that shape -> ${payload}")
    fi
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
  printf 'docs/internal/measurement-ledger.md for the documented blind spots.\n'
} >&2

exit 1
