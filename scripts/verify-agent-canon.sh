#!/usr/bin/env bash
#
# verify-agent-canon.sh — fail if shared agent guidance drifts or conflicts.
#
# AGENTS.md and CLAUDE.md MUST stay byte-identical: AGENTS.md is read by Codex
# and opencode, CLAUDE.md by Claude Code, and the repo rule requires the two to
# be in lockstep so every harness sees the same rules. This parity was enforced
# only by a local pre-commit hook (bypassable with --no-verify); this script is
# the CI gate so a drifted commit cannot merge.
#
# Exit 0 when identical; non-zero with a unified diff on drift.
set -euo pipefail

repo_root="${ESHU_AGENT_CANON_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  # Derive the repo root from the script's own location (hook- and worktree-safe);
  # git hooks export GIT_DIR, which breaks `git rev-parse --show-toplevel` from a
  # subdirectory. The script always lives at <repo>/scripts/.
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi

agents="$repo_root/AGENTS.md"
claude="$repo_root/CLAUDE.md"

missing=0
for f in "$agents" "$claude"; do
  if [ ! -f "$f" ]; then
    printf 'verify-agent-canon: missing required file %s\n' "$f" >&2
    missing=1
  fi
done
[ "$missing" -eq 0 ] || exit 1

diff_out="$(diff -u "$agents" "$claude" 2>&1 || true)"
if [ -n "$diff_out" ]; then
  printf 'verify-agent-canon: AGENTS.md and CLAUDE.md have drifted.\n' >&2
  printf 'They MUST be byte-identical (the root agent canon is shared across harnesses).\n\n' >&2
  printf '%s\n\n' "$diff_out" >&2
  printf 'Fix: make both files identical, then re-run.\n' >&2
  exit 1
fi

printf 'verify-agent-canon: AGENTS.md and CLAUDE.md are byte-identical.\n'

skills_root="$repo_root/.agents/skills"
if [ -d "$skills_root" ]; then
  for skill_file in "$skills_root"/*/SKILL.md; do
    [ -f "$skill_file" ] || continue
    skill_name="$(basename "$(dirname "$skill_file")")"
    for harness in .claude .codex; do
      link="$repo_root/$harness/skills/$skill_name"
      if [ ! -L "$link" ]; then
        printf 'verify-agent-canon: %s cannot discover shared skill %s; missing symlink %s\n' \
          "$harness" "$skill_name" "$link" >&2
        exit 1
      fi
      if [ ! -f "$link/SKILL.md" ] || ! cmp -s "$skill_file" "$link/SKILL.md"; then
        printf 'verify-agent-canon: %s skill link %s does not resolve to %s\n' \
          "$harness" "$link" "$skill_file" >&2
        exit 1
      fi
    done
  done
  printf 'verify-agent-canon: shared skill discovery links are complete.\n'

  # Every project skill must be reachable from the editor-side nudge hook, or be
  # explicitly exempt. The nudge table is an Eshu path->skill map, so it rots
  # silently every time a directory moves or a skill is added: the hook keeps
  # exiting 0 and nobody learns the arm stopped matching. Session-triggered
  # skills (review, release, humanizer) have no characteristic file path and
  # belong in NUDGE_EXEMPT rather than in an arm.
  nudge_hook="$repo_root/.claude/hooks/skill-nudge.sh"
  if [ ! -f "$nudge_hook" ]; then
    printf 'verify-agent-canon: missing skill nudge hook: %s\n' "$nudge_hook" >&2
    exit 1
  fi
  # Scope the match to the two places that actually route: the IDS= values a
  # case arm assigns, and the explicit exempt block. Matching the whole file
  # would let any passing mention in a comment satisfy the gate, which is the
  # tautological-guard failure this repo keeps relearning. IDS holds the
  # enforced ids and NOTE the human half, deliberately separate, so a word in
  # prose cannot mint a skill id.
  # Only assignments INSIDE the case block count. An `IDS="x"` appended after
  # `esac` is unreachable -- no path can ever select it -- but a whole-file
  # match accepts it and reports the skill routed. Scope to the block first.
  nudge_case="$(sed -n '/^case /,/^esac/p' "$nudge_hook")"
  nudge_arms="$(printf '%s' "$nudge_case" | rg -o 'IDS="[^"]*"' || true)"
  nudge_exempt="$(sed -n '/^# NUDGE_EXEMPT_BEGIN/,/^# NUDGE_EXEMPT_END/p' "$nudge_hook")"
  if [ -z "$nudge_exempt" ]; then
    printf 'verify-agent-canon: %s has no NUDGE_EXEMPT_BEGIN/END block\n' \
      "$nudge_hook" >&2
    exit 1
  fi
  missing_arms=()
  for skill_file in "$skills_root"/*/SKILL.md; do
    [ -f "$skill_file" ] || continue
    skill_name="$(basename "$(dirname "$skill_file")")"
    # Whole-name match, not substring: a future `golang-engineering-v2` must not
    # count as covered because the `golang-engineering` arm mentions a prefix of
    # it. Hyphens are word characters here, so \b is not enough.
    skill_pattern="(^|[^A-Za-z0-9-])${skill_name}([^A-Za-z0-9-]|$)"
    if printf '%s' "$nudge_arms" | rg -q -- "$skill_pattern"; then
      continue
    fi
    if printf '%s' "$nudge_exempt" | rg -q -- "$skill_pattern"; then
      continue
    fi
    missing_arms+=("$skill_name")
  done
  if [ "${#missing_arms[@]}" -gt 0 ]; then
    printf 'verify-agent-canon: skill(s) unreachable from %s:\n' "$nudge_hook" >&2
    for skill_name in "${missing_arms[@]}"; do
      printf '  %s\n' "$skill_name" >&2
    done
    printf 'Fix: add a case arm mapping its surface paths, or add it to NUDGE_EXEMPT\n' >&2
    printf 'with a one-line reason if it has no characteristic file path.\n' >&2
    exit 1
  fi
  printf 'verify-agent-canon: every project skill is reachable from the nudge hook.\n'

  performance_skill="$skills_root/eshu-performance-rigor/SKILL.md"
  performance_manifest="$skills_root/eshu-performance-rigor/references/run-manifest.md"
  if [ ! -f "$performance_skill" ]; then
    printf 'verify-agent-canon: missing mandatory performance skill: %s\n' \
      "$performance_skill" >&2
    exit 1
  fi

    performance_skill_tokens=(
      '## Target Contribution Budget'
      'required_saving_seconds'
      'maximum_recoverable_seconds'
      'expected_saving_seconds'
      '## Resource-Qualified Claims'
      'absolute_target_applicable'
      'same-machine relative'
      '## Baseline Promotion'
      '## Retention Modes'
      'stop-and-preserve'
      'git merge-base --is-ancestor'
    )
    for token in "${performance_skill_tokens[@]}"; do
      if ! rg -Fq "$token" "$performance_skill"; then
        printf 'verify-agent-canon: performance skill missing workflow contract token: %s\n' \
          "$token" >&2
        exit 1
      fi
    done

    if [ ! -f "$performance_manifest" ]; then
      printf 'verify-agent-canon: performance skill missing run manifest reference: %s\n' \
        "$performance_manifest" >&2
      exit 1
    fi
    performance_manifest_tokens=(
      'target_contribution'
      'phase_durations_seconds'
      'retention'
      'accepted_commit'
      'hardware_class'
      'machine_profile'
      'reference_profile'
      'resource_envelope'
      'memory_bytes'
      'container_memory_limit_bytes'
      'absolute_target_applicable'
      'compose_service_limits'
      'service_usage_summary'
    )
    for token in "${performance_manifest_tokens[@]}"; do
      if ! rg -Fq "$token" "$performance_manifest"; then
        printf 'verify-agent-canon: run manifest missing workflow contract token: %s\n' \
          "$token" >&2
        exit 1
      fi
    done
    printf 'verify-agent-canon: performance workflow contract is complete.\n'
fi

# Retired-phrasing guard for the review bar (#6175). These phrasings were all
# removed when the bar became P2-blocking, and each one re-introduced would put
# the canon back into the self-contradiction that survived seven review rounds:
# a document demanding every P2 fixed while its neighbour allows a tracked
# deferral, with an agent free to follow whichever it read last.
#
# -U is load-bearing, not stylistic. Rule text is line-wrapped, and rg matches
# per line, so a single-line pattern cannot see a clause broken across a wrap.
# Both live escapes this guard was built for were wrapped, at DIFFERENT points
# ("tracked\nand named" and "tracked and\nnamed"), and a per-line sweep -- run
# by hand, twice, by two different readers -- reported them clean.
#
# Scope honestly: this catches RE-INTRODUCTION of phrasings already known to be
# wrong. It cannot catch a phrasing nobody has imagined; six such sites were
# found only by reading a deliberately over-broad net, which is still required
# and which must itself be run with -U.
canon_rule_files=(
  "$repo_root/AGENTS.md"
  "$repo_root/CLAUDE.md"
  "$repo_root/docs/public/reference/local-testing.md"
  "$repo_root/docs/public/guides/run-the-proof-suite.md"
  "$repo_root/docs/internal/agent-orchestration.md"
)
retired_bar_pattern='P2=0|P0=0,[[:space:]]*P1=0,[[:space:]]*P2=0|zero[[:space:]]+P0/P1/P2[[:space:]]+findings|tracked[[:space:]]+and[[:space:]]+named|P0=<n>,[[:space:]]*P1=<n>,[[:space:]]*P2=<n>'
canon_rule_targets=()
for f in "${canon_rule_files[@]}"; do
  [ -f "$f" ] && canon_rule_targets+=("$f")
done
[ -d "$repo_root/.agents/skills" ] && canon_rule_targets+=("$repo_root/.agents/skills")
if [ "${#canon_rule_targets[@]}" -gt 0 ]; then
  if rg -nU "$retired_bar_pattern" "${canon_rule_targets[@]}" >&2; then
    printf 'verify-agent-canon: retired review-bar phrasing re-introduced above.\n' >&2
    printf '  The bar is P0=0, P1=0, P2-blocking=0, with every deferred P2 tracked in a\n' >&2
    printf '  linked issue, the owner agreement quoted in the PR, and its severity-table\n' >&2
    printf '  category named. See .agents/skills/eshu-code-review/references/merge-bar.md\n' >&2
    exit 1
  fi
  printf 'verify-agent-canon: no retired review-bar phrasing in the canon or skills.\n'
fi

opencode_agents="$repo_root/.opencode/agent"
if [ -d "$opencode_agents" ]; then
  conflict_pattern='Push over HTTPS|Always .*--no-verify|https://github[.]com/eshu-hq/eshu[.]git'
  if rg -n "$conflict_pattern" "$opencode_agents" >&2; then
    printf 'verify-agent-canon: OpenCode role shim contradicts root Git policy.\n' >&2
    exit 1
  fi
  printf 'verify-agent-canon: OpenCode role shims do not override root Git policy.\n'
fi
