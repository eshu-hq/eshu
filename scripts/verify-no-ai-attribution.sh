#!/usr/bin/env bash
#
# verify-no-ai-attribution.sh — fail if AI-attribution markers appear in commit
# messages or added diff content.
#
# Repo rule: no AI attribution in commits, PRs, or docs — no Co-authored-by
# trailer, no "Generated with/by <AI tool>", no tool fingerprints. Nothing
# enforced this before; cheaper models routinely append such footers. CI runs
# this in range mode; pre-commit runs --staged (content) and --message (the
# commit-msg hook).
#
# Modes:
#   (default)         range mode — scan commit messages and added diff lines in
#                     <base>..HEAD. base = ESHU_AI_ATTRIBUTION_BASE, else
#                     origin/$GITHUB_BASE_REF, else merge-base origin/main HEAD.
#   --staged          scan staged (git diff --cached) added lines.
#   --message <file>  scan a commit-message file.
#
# Exit 0 when clean; non-zero listing each offending location.
set -euo pipefail

repo_root="${ESHU_AI_ATTRIBUTION_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi

# Case-insensitive ERE of real AI-attribution markers. Deliberately specific so
# it matches AI attribution, not prose naming the rule and NOT a normal human
# Co-authored-by trailer: a Co-authored-by line is flagged only when it names an
# AI tool (or the Anthropic address). Plus "generated with/by <AI tool>", the
# Claude Code robot-emoji footer, and the Anthropic noreply address anywhere.
pattern='co-authored-by:.*(claude|copilot|chatgpt|gpt-|cursor|gemini|codex|anthropic).*<|generated (with|by) (\[?claude|copilot|chatgpt|gpt-|cursor|gemini|codex)|🤖 generated with|noreply@anthropic\.com'

# The gate's own implementation and docs necessarily contain these patterns.
# Exclude them from content scans so the gate never flags itself.
self_excludes=(
  ':(exclude)scripts/verify-no-ai-attribution.sh'
  ':(exclude)scripts/test-verify-agent-hygiene.sh'
  ':(exclude)docs/public/reference/local-testing/pre-commit-hooks.md'
)

fail=0

report() {
  printf 'verify-no-ai-attribution: %s\n' "$1" >&2
}

scan_added_lines() { # $1..: git diff revision/args
  git -C "$repo_root" diff "$@" -- . "${self_excludes[@]}" 2>/dev/null \
    | rg '^\+' | rg -v '^\+\+\+ ' | rg -i -n -e "$pattern" || true
}

mode="${1:-range}"
case "$mode" in
  --staged)
    hits="$(scan_added_lines --cached)"
    if [ -n "$hits" ]; then
      report "AI-attribution marker in staged content:"
      printf '%s\n' "$hits" >&2
      fail=1
    fi
    ;;
  --message)
    msg_file="${2:-}"
    if [ -n "$msg_file" ] && [ -f "$msg_file" ]; then
      hits="$(rg -i -n -e "$pattern" "$msg_file" || true)"
      if [ -n "$hits" ]; then
        report "AI-attribution marker in commit message:"
        printf '%s\n' "$hits" >&2
        fail=1
      fi
    fi
    ;;
  *)
    base="${ESHU_AI_ATTRIBUTION_BASE:-}"
    if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
      git -C "$repo_root" fetch --no-tags --depth=1 origin "$GITHUB_BASE_REF" >/dev/null 2>&1 || true
      if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
        base="origin/$GITHUB_BASE_REF"
      fi
    fi
    if [ -z "$base" ]; then
      if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
        base="$(git -C "$repo_root" merge-base origin/main HEAD 2>/dev/null || echo origin/main)"
      else
        printf 'verify-no-ai-attribution: no base commit available, skipping\n'
        exit 0
      fi
    fi

    msg_hits="$(git -C "$repo_root" log --format='%H %s%n%b' "$base..HEAD" 2>/dev/null | rg -i -n -e "$pattern" || true)"
    if [ -n "$msg_hits" ]; then
      report "AI-attribution marker in commit message(s) in $base..HEAD:"
      printf '%s\n' "$msg_hits" >&2
      fail=1
    fi

    diff_hits="$(scan_added_lines "$base...HEAD")"
    if [ -n "$diff_hits" ]; then
      report "AI-attribution marker in added content in $base...HEAD:"
      printf '%s\n' "$diff_hits" >&2
      fail=1
    fi
    ;;
esac

if [ "$fail" -ne 0 ]; then
  printf '\nFix: remove the AI-attribution line(s). No Co-authored-by, no\n' >&2
  printf '"Generated with/by <tool>", no tool fingerprints — in commits, PRs, or docs.\n' >&2
  exit 1
fi

printf 'verify-no-ai-attribution: no AI-attribution markers found.\n'
