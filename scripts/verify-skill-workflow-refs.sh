#!/usr/bin/env bash
#
# verify-skill-workflow-refs.sh - fail a skill doc under .agents/skills/**
# that names a .github/workflows/*.yml or *.yaml file that does not exist
# (#5855). GitHub Actions accepts both extensions for a workflow file
# (https://docs.github.com/actions/using-workflows/about-workflows), so a
# guard scoped to reference completeness must not have a blind spot for
# `.yaml`, even though every workflow this repo has today happens to be
# `.yml`.
#
# A skill's SKILL.md is read by agents as ground truth for "where does this
# gate run". A stale workflow name sends the next agent hunting for a CI
# file that was renamed or consolidated away, which is exactly what happened
# to `.github/workflows/verify-telemetry-coverage.yml` and
# `.github/workflows/verify-skill-roundtrip.yml`: both gates now run as
# matrix entries inside `.github/workflows/static-contract-gates.yml`, but
# the skill docs kept citing the old per-gate workflow filenames after the
# #4218 consolidation.
#
# Three reference shapes are scanned, all restricted to .agents/skills/**/*.md:
#
#   1. FULL PATH: a literal `.github/workflows/<name>.yml` (or `.yaml`) path,
#      anywhere in the file (inline code or prose). Checked directly: the
#      path must exist relative to the repo root.
#   2. BARE FILENAME (backticked): a lowercase, hyphen/underscore-safe
#      `<name>.yml` (or `.yaml`) token wrapped in single backticks with no
#      leading path (e.g. `verify-skill-roundtrip.yml`). Checked by joining
#      to `.github/workflows/`, then run through the same allowlist as shape
#      3 (see bare_yaml_name_is_allowlisted below) -- needed because a real
#      backtick-wrapped bare `.yaml` file, e.g. `values.yaml` (a Helm chart
#      file), is not a workflow either.
#   3. BARE FILENAME (plain prose): the same lowercase, hyphen/underscore-safe
#      `<name>.yml` (or `.yaml`) token with NEITHER backticks NOR a leading
#      path -- e.g.
#      "ACTIVATE when editing security-scan.yml" in
#      eshu-security-scan-gates/SKILL.md:6. Checked by joining to
#      `.github/workflows/`, same as shape 2, but first run through an
#      EXPLICIT, per-entry-commented allowlist (see
#      bare_yaml_name_is_allowlisted below) of known non-workflow lowercase
#      `<name>.yml`/`<name>.yaml` basenames -- Taskfile.yml, .golangci.yml,
#      mkdocs.yml, docker-compose*.yml, values.yaml -- that can legitimately
#      appear bare. This is not the blank-column `|| continue` silent
#      exemption #5855 itself fixed: each allowlist entry names a real,
#      non-workflow file with a one-line reason,
#      reviewable in a diff, not an unnamed catch-all skip.
#
# Shapes 2 and 3 are deliberately restricted to a lowercase-leading character
# class ([a-z][a-z0-9_-]*). This repo's own workflow basenames are all
# lowercase (build.yml, verify-telemetry-coverage.yml, ...), while generic
# non-workflow examples that also end in .yml — `Taskfile.yml`,
# `.golangci.yml` in golang-engineering's generic "how to discover a repo's
# verification entrypoint" checklist — start with an uppercase letter or a
# leading dot and are correctly left alone. A context-window ("is the word
# 'workflow' nearby") heuristic was tried first and rejected: it false-
# positived on that same checklist, where "CI workflows" sits one bullet
# away from `Taskfile.yml` and `.golangci.yml` with no blank line between
# bullets to bound the window.
#
# Shape 3 requires PCRE2 (`rg -P`/`--pcre2`) for the lookaround. The leading
# `(?<![A-Za-z0-9_./`-])` excludes any position where the character just
# before the match is itself part of a longer filename/path token --
# letter, digit, `_`, `.`, `/`, `-`, or a backtick. That character class is
# deliberately wider than "just / and backtick": without it, the hyphen in
# `docker-publish.yml` or `security-scan.yml` creates a regex word boundary
# right before "publish.yml" / "scan.yml" (`-` is a non-word character), so
# a narrower lookbehind would re-enter mid-token and misreport a SUFFIX of
# an already-covered full-path or backticked reference as its own bare
# citation. The trailing `(?!`)` excludes a token immediately followed by a
# backtick (the closing half of a shape-2 match). This repo already depends
# on `rg --pcre2` elsewhere (e.g. scripts/verify-ask-eshu-local-proof.sh),
# and CI installs ripgrep via apt, which ships with PCRE2 support compiled
# in.
#
# Exit 0 on success; non-zero with one line per dangling reference on
# failure. This covers three reference shapes, not "any reference" --
# a reference embedded inside a longer word, split across lines, or using a
# shape not listed above (e.g. a Markdown link target) is not scanned.
set -euo pipefail

repo_root="${ESHU_SKILL_WORKFLOW_REFS_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  # Derive from the script's own location, not `git rev-parse
  # --show-toplevel` — under a git hook with GIT_DIR exported, `-C scripts
  # rev-parse --show-toplevel` can resolve relative to GIT_DIR instead of the
  # repo root. The script always lives at <repo>/scripts/, so dirname/.. is
  # both worktree- and hook-safe.
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi

skills_dir="${ESHU_SKILL_WORKFLOW_REFS_SKILLS_DIR:-${repo_root}/.agents/skills}"
workflows_dir="${ESHU_SKILL_WORKFLOW_REFS_WORKFLOWS_DIR:-${repo_root}/.github/workflows}"

log() {
  printf 'verify-skill-workflow-refs: %s\n' "$*" >&2
}

command -v rg >/dev/null 2>&1 || {
  log "missing required tool: rg"
  exit 1
}
rg --pcre2-version >/dev/null 2>&1 || {
  log "missing required rg feature: pcre2 (needed for the bare-prose scan)"
  exit 1
}
[ -d "$skills_dir" ] || {
  log "skills dir not found: $skills_dir"
  exit 1
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

# full_path_refs emits one "<file>:<line>:<workflow-relative-path>" per full
# `.github/workflows/<name>.yml` (or `.yaml`) citation found under
# .agents/skills/**/*.md.
full_path_refs() {
  rg -n -o -g '*.md' \
    '\.github/workflows/[A-Za-z0-9_.-]+\.ya?ml' \
    "$skills_dir" 2>/dev/null || true
}

# bare_name_refs emits one "<file>:<line>:<workflow-relative-path>" per
# backtick-wrapped, lowercase-leading bare `<name>.yml` (or `.yaml`)
# citation, with the match rewritten onto the .github/workflows/ prefix and
# then dropped if it matches bare_yaml_name_is_allowlisted (defined below;
# bash resolves the call at run time, so the forward reference is fine) --
# e.g. `values.yaml` (a Helm chart file, not a workflow) would otherwise
# misreport as a dangling `.github/workflows/values.yaml` citation once the
# `.yaml` extension is in scope.
bare_name_refs() {
  local line rel_path file_line name
  # The sed rewrite uses -E (ERE, portable to both BSD sed on macOS and GNU
  # sed) rather than BRE `\?`/`\|`, which are GNU-only extensions and are
  # not available on this repo's macOS/BSD sed.
  rg -n -o -g '*.md' \
    '`[a-z][a-z0-9_-]*\.ya?ml`' \
    "$skills_dir" 2>/dev/null \
    | LC_ALL=C sed -E 's/`([a-z][a-z0-9_-]*\.ya?ml)`$/.github\/workflows\/\1/' \
    | while IFS= read -r line; do
        [ -z "$line" ] && continue
        rel_path="${line##*:}"
        file_line="${line%:*}"
        name="${rel_path#.github/workflows/}"
        if bare_yaml_name_is_allowlisted "$name"; then
          continue
        fi
        printf '%s\n' "$line"
      done \
    || true
}

# bare_yaml_name_is_allowlisted returns success (0) for a lowercase
# `<name>.yml` or `<name>.yaml` basename that is a KNOWN, real, non-workflow
# file allowed to appear bare -- either backtick-wrapped (shape 2) or in
# plain prose with no backticks and no path prefix (shape 3). Each entry
# names the actual file this repo has and states in one line why it is not a
# .github/workflows/*.yml(|.yaml) gate. Keep this list exhaustive rather
# than pattern-guessing a broader exemption -- an unnamed, undocumented skip
# is exactly the defect class #5855 fixed elsewhere in this gate (a
# blank-column `|| continue` in the reverse row check).
#
# This list is shared by both bare_name_refs (shape 2) and bare_prose_refs
# (shape 3): before the `.ya?ml` extension covered `.yaml`, shape 2 had no
# false-positive exposure because no real backtick-wrapped, lowercase, bare
# non-workflow `.yml` file existed in this repo's skill docs. Adding `.yaml`
# support surfaced one immediately -- `values.yaml` (see below) -- so shape
# 2 now needs the same allowlist shape 3 already had, not a separate one.
bare_yaml_name_is_allowlisted() {
  case "$1" in
    golangci.yml)
      # go/.golangci.yml is the golangci-lint config, not a GH Actions
      # workflow (see eshu-security-scan-gates/SKILL.md:102).
      return 0
      ;;
    taskfile.yml)
      # Taskfile.yml is the Task runner's manifest, not a GH Actions
      # workflow (see golang-engineering/references/verification-and-linting.md).
      # Always backtick-wrapped and uppercase-leading today, so this entry
      # is forward defense against a future lowercase, un-backticked mention.
      return 0
      ;;
    mkdocs.yml)
      # docs/mkdocs.yml is the MkDocs site config, not a GH Actions workflow
      # (see telemetry-coverage-discipline/SKILL.md).
      return 0
      ;;
    docker-compose*.yml)
      # docker-compose.yml / docker-compose.override.yml / etc. are Compose
      # stack files, not GH Actions workflows. No skill doc references one
      # bare today; listed pre-emptively per the #5855 P2 fix request.
      return 0
      ;;
    values.yaml)
      # A Helm chart's values.yaml, not a GH Actions workflow (see
      # eshu-release/SKILL.md:104). Backtick-wrapped bare mention;
      # surfaced by adding `.yaml` support to this gate.
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

# bare_prose_refs emits one "<file>:<line>:<workflow-relative-path>" per
# lowercase-leading bare `<name>.yml` citation that has NEITHER backticks
# NOR a leading path -- shape 1 (full-path) and shape 2
# (backtick-anchored) both miss, e.g. "editing security-scan.yml" in
# eshu-security-scan-gates/SKILL.md:6. Requires PCRE2 lookaround: `(?<!`)`
# and `(?<!/)` exclude tokens already caught by shape 2 or shape 1; `(?!`)`
# excludes a token immediately followed by a backtick (the closing-backtick
# half of a shape-2 match). Names in bare_yaml_name_is_allowlisted are
# dropped before being emitted, so they never reach the dangling-file check.
bare_prose_refs() {
  local line rel_path file_line
  rg -n -o -P -g '*.md' \
    '(?<![A-Za-z0-9_./`-])[a-z][a-z0-9_-]*\.ya?ml\b(?!`)' \
    "$skills_dir" 2>/dev/null \
    | while IFS= read -r line; do
        [ -z "$line" ] && continue
        rel_path="${line##*:}"
        file_line="${line%:*}"
        # NOTE: use an `if` guard, not `bare_yaml_name_is_allowlisted ... &&
        # continue` -- both forms behave identically here (a failing left
        # side of `&&` is a documented `set -e` exemption: `-e` only fires
        # on the LAST command of an AND/OR list, not the ones before it, so
        # the common non-allowlisted case would not trip `set -e` either
        # way; verified against both Homebrew bash 5.3.15 and macOS stock
        # bash 3.2.57). The `if` form is kept because it reads as an
        # explicit branch rather than a control-flow side effect of `&&`.
        if bare_yaml_name_is_allowlisted "$rel_path"; then
          continue
        fi
        printf '%s:.github/workflows/%s\n' "$file_line" "$rel_path"
      done \
    || true
}

# dangling_refs reads "<file>:<line>:<workflow-relative-path>" lines on
# stdin and prints only the ones whose workflow-relative-path does not exist
# under $workflows_dir.
dangling_refs() {
  local line file_line rel_path name
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    rel_path="${line##*:}"
    file_line="${line%:*}"
    name="${rel_path#.github/workflows/}"
    [ -f "${workflows_dir}/${name}" ] && continue
    printf '%s references missing workflow %s\n' "$file_line" "$rel_path"
  done
}

full_path_refs >"${tmp_dir}/full.txt"
bare_name_refs >"${tmp_dir}/bare.txt"
bare_prose_refs >"${tmp_dir}/prose.txt"

cat "${tmp_dir}/full.txt" "${tmp_dir}/bare.txt" "${tmp_dir}/prose.txt" \
  | dangling_refs >"${tmp_dir}/dangling.txt" || true

dangling_count="$(awk 'NF' "${tmp_dir}/dangling.txt" | wc -l | tr -d ' ')"
if [ "$dangling_count" -gt 0 ]; then
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    log "$line"
  done <"${tmp_dir}/dangling.txt"
  log "${dangling_count} skill doc reference(s) to a missing .github/workflows/*.yml file"
  exit 1
fi

checked_count="$(cat "${tmp_dir}/full.txt" "${tmp_dir}/bare.txt" "${tmp_dir}/prose.txt" | awk 'NF' | wc -l | tr -d ' ')"
log "OK: ${checked_count} workflow reference(s) checked under .agents/skills/**/*.md, 0 dangling"
