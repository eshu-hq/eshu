#!/usr/bin/env bash
#
# test-verify-agent-hygiene.sh — test mirror for verify-agent-canon.sh and
# verify-no-ai-attribution.sh. Exercises pass and fail cases against throwaway
# fixtures so the gates' behavior is pinned, mirroring the other verify gates.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo_root="$(cd "$here/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

pass=0
fail=0
ok() { printf 'ok - %s\n' "$1"; pass=$((pass + 1)); }
no() { printf 'NOT OK - %s\n' "$1"; fail=$((fail + 1)); }

canon="$repo_root/scripts/verify-agent-canon.sh"
attr="$repo_root/scripts/verify-no-ai-attribution.sh"

# --- verify-agent-canon ---
mkdir -p "$tmp/good"
printf 'shared canon\n' >"$tmp/good/AGENTS.md"
printf 'shared canon\n' >"$tmp/good/CLAUDE.md"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/good" "$canon" >/dev/null 2>&1; then
  ok "agent-canon passes when AGENTS.md == CLAUDE.md"
else
  no "agent-canon should pass when identical"
fi

mkdir -p "$tmp/bad"
printf 'one\n' >"$tmp/bad/AGENTS.md"
printf 'two\n' >"$tmp/bad/CLAUDE.md"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/bad" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail on drift"
else
  ok "agent-canon fails when the two files drift"
fi

# Retired review-bar phrasing. Both live escapes were LINE-WRAPPED, at different
# points, and a per-line sweep reported them clean twice, so the wrapped shapes
# are the cases that matter -- a mirror that only checks the single-line form
# would pass while the bug it targets sits in the tree.
mkdir -p "$tmp/bar-ok"
printf 'canon\n' >"$tmp/bar-ok/AGENTS.md"
printf 'canon\n' >"$tmp/bar-ok/CLAUDE.md"
printf 'Ready means P0=0, P1=0, P2-blocking=0 with every deferred P2 tracked\nin a linked issue with the owner agreement quoted.\n' \
  >"$tmp/bar-ok/local-testing-stub.md"
mkdir -p "$tmp/bar-ok/docs/public/reference"
mv "$tmp/bar-ok/local-testing-stub.md" "$tmp/bar-ok/docs/public/reference/local-testing.md"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/bar-ok" "$canon" >/dev/null 2>&1; then
  ok "agent-canon passes on the P2-blocking bar wording"
else
  no "agent-canon should pass on the P2-blocking bar wording"
fi

mkdir -p "$tmp/bar-wrap-a/docs/public/reference"
printf 'canon\n' >"$tmp/bar-wrap-a/AGENTS.md"
printf 'canon\n' >"$tmp/bar-wrap-a/CLAUDE.md"
printf 'Ready means every deferred P2 tracked\nand named, the gate is complete.\n' \
  >"$tmp/bar-wrap-a/docs/public/reference/local-testing.md"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/bar-wrap-a" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail on \"tracked\\nand named\" (wrap before the conjunction)"
else
  ok "agent-canon fails on \"tracked\\nand named\" (wrap before the conjunction)"
fi

mkdir -p "$tmp/bar-wrap-b/docs/public/reference"
printf 'canon\n' >"$tmp/bar-wrap-b/AGENTS.md"
printf 'canon\n' >"$tmp/bar-wrap-b/CLAUDE.md"
printf 'Ready means every deferred P2 tracked and\nnamed, and the owner able to see why.\n' \
  >"$tmp/bar-wrap-b/docs/public/reference/local-testing.md"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/bar-wrap-b" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail on \"tracked and\\nnamed\" (wrap after the conjunction)"
else
  ok "agent-canon fails on \"tracked and\\nnamed\" (wrap after the conjunction)"
fi

mkdir -p "$tmp/bar-canon-p1/docs/public/reference"
printf 'canon\n' >"$tmp/bar-canon-p1/AGENTS.md"
printf 'canon\n' >"$tmp/bar-canon-p1/CLAUDE.md"
printf 'a preliminary full review with zero\nP0/P1/P2 findings, run make pre-pr once.\n' \
  >"$tmp/bar-canon-p1/docs/public/reference/local-testing.md"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/bar-canon-p1" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail on the wrapped \"zero\\nP0/P1/P2 findings\" clause"
else
  ok "agent-canon fails on the wrapped \"zero\\nP0/P1/P2 findings\" clause"
fi

# write_nudge_fixture <repo_root> — minimal skill-nudge hook covering the two
# skills every fixture below declares. The canon gate requires each skill to be
# assigned by a SKILL= arm or named in the exempt block, so a fixture repo
# without this file fails for the wrong reason.
write_nudge_fixture() {
  mkdir -p "$1/.claude/hooks"
  {
    printf '#!/bin/bash\n'
    printf '# NUDGE_EXEMPT_BEGIN\n'
    printf '#   example                 fixture skill, no real surface\n'
    printf '#   eshu-performance-rigor  fixture skill, no real surface\n'
    printf '# NUDGE_EXEMPT_END\n'
    printf 'SKILL=""\n'
  } >"$1/.claude/hooks/skill-nudge.sh"
}

mkdir -p "$tmp/skill-links/.agents/skills/example" \
  "$tmp/skill-links/.agents/skills/eshu-performance-rigor/references" \
  "$tmp/skill-links/.claude/skills" \
  "$tmp/skill-links/.codex/skills"
write_nudge_fixture "$tmp/skill-links"
printf 'shared canon\n' >"$tmp/skill-links/AGENTS.md"
printf 'shared canon\n' >"$tmp/skill-links/CLAUDE.md"
printf '%s\n' '---' 'name: example' 'description: example' '---' \
  >"$tmp/skill-links/.agents/skills/example/SKILL.md"
ln -s ../../.agents/skills/example "$tmp/skill-links/.claude/skills/example"
ln -s ../../.agents/skills/example "$tmp/skill-links/.codex/skills/example"
cat >"$tmp/skill-links/.agents/skills/eshu-performance-rigor/SKILL.md" <<'LINK_PERF_SKILL'
## Target Contribution Budget
required_saving_seconds maximum_recoverable_seconds expected_saving_seconds
## Resource-Qualified Claims
absolute_target_applicable same-machine relative
## Baseline Promotion
## Retention Modes
stop-and-preserve git merge-base --is-ancestor
LINK_PERF_SKILL
cat >"$tmp/skill-links/.agents/skills/eshu-performance-rigor/references/run-manifest.md" <<'LINK_PERF_MANIFEST'
target_contribution phase_durations_seconds retention accepted_commit
hardware_class machine_profile reference_profile resource_envelope memory_bytes
container_memory_limit_bytes absolute_target_applicable compose_service_limits
service_usage_summary
LINK_PERF_MANIFEST
ln -s ../../.agents/skills/eshu-performance-rigor \
  "$tmp/skill-links/.claude/skills/eshu-performance-rigor"
ln -s ../../.agents/skills/eshu-performance-rigor \
  "$tmp/skill-links/.codex/skills/eshu-performance-rigor"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/skill-links" "$canon" >/dev/null 2>&1; then
  ok "agent-canon passes when shared skill discovery links are complete"
else
  no "agent-canon should pass when shared skill discovery links are complete"
fi

rm "$tmp/skill-links/.codex/skills/example"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/skill-links" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail when one harness cannot discover a shared skill"
else
  ok "agent-canon fails when a shared skill discovery link is missing"
fi

mkdir -p "$tmp/perf-contract/.agents/skills/eshu-performance-rigor/references" \
  "$tmp/perf-contract/.claude/skills" \
  "$tmp/perf-contract/.codex/skills"
write_nudge_fixture "$tmp/perf-contract"
printf 'shared canon\n' >"$tmp/perf-contract/AGENTS.md"
printf 'shared canon\n' >"$tmp/perf-contract/CLAUDE.md"
printf '%s\n' '---' 'name: eshu-performance-rigor' 'description: incomplete' '---' \
  >"$tmp/perf-contract/.agents/skills/eshu-performance-rigor/SKILL.md"
printf '# Performance Run Manifest\n' \
  >"$tmp/perf-contract/.agents/skills/eshu-performance-rigor/references/run-manifest.md"
ln -s ../../.agents/skills/eshu-performance-rigor \
  "$tmp/perf-contract/.claude/skills/eshu-performance-rigor"
ln -s ../../.agents/skills/eshu-performance-rigor \
  "$tmp/perf-contract/.codex/skills/eshu-performance-rigor"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/perf-contract" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail when the performance workflow contract is incomplete"
else
  ok "agent-canon fails when the performance workflow contract is incomplete"
fi

cat >>"$tmp/perf-contract/.agents/skills/eshu-performance-rigor/SKILL.md" <<'PERF_SKILL'
## Target Contribution Budget
required_saving_seconds maximum_recoverable_seconds expected_saving_seconds
## Baseline Promotion
## Retention Modes
stop-and-preserve
git merge-base --is-ancestor
PERF_SKILL
cat >>"$tmp/perf-contract/.agents/skills/eshu-performance-rigor/references/run-manifest.md" <<'PERF_MANIFEST'
target_contribution
phase_durations_seconds
retention
accepted_commit
hardware_class
PERF_MANIFEST
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/perf-contract" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail when the performance resource envelope is missing"
else
  ok "agent-canon fails when the performance resource envelope is missing"
fi

cat >>"$tmp/perf-contract/.agents/skills/eshu-performance-rigor/SKILL.md" <<'PERF_RESOURCES'
## Resource-Qualified Claims
absolute_target_applicable
same-machine relative
PERF_RESOURCES
cat >>"$tmp/perf-contract/.agents/skills/eshu-performance-rigor/references/run-manifest.md" <<'PERF_RESOURCE_MANIFEST'
reference_profile
machine_profile
resource_envelope
memory_bytes
container_memory_limit_bytes
absolute_target_applicable
compose_service_limits
service_usage_summary
PERF_RESOURCE_MANIFEST
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/perf-contract" "$canon" >/dev/null 2>&1; then
  ok "agent-canon passes when the performance workflow contract is complete"
else
  no "agent-canon should pass when the performance workflow contract is complete"
fi

mv "$tmp/perf-contract/.agents/skills/eshu-performance-rigor/SKILL.md" \
  "$tmp/perf-contract/performance-skill.saved"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/perf-contract" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail when the mandatory performance skill is missing"
else
  ok "agent-canon fails when the mandatory performance skill is missing"
fi
mv "$tmp/perf-contract/performance-skill.saved" \
  "$tmp/perf-contract/.agents/skills/eshu-performance-rigor/SKILL.md"

mkdir -p "$tmp/opencode-conflict/.opencode/agent"
printf 'shared canon\n' >"$tmp/opencode-conflict/AGENTS.md"
printf 'shared canon\n' >"$tmp/opencode-conflict/CLAUDE.md"
printf '%s\n' 'Push over HTTPS and always use --no-verify.' \
  >"$tmp/opencode-conflict/.opencode/agent/develop-eshu.md"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/opencode-conflict" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail on OpenCode instructions that contradict root Git policy"
else
  ok "agent-canon fails on contradictory OpenCode Git instructions"
fi

if rg -Fq '\.agents/' "$repo_root/.pre-commit-config.yaml" \
  && rg -Fq 'scripts/verify-agent-canon\.sh' "$repo_root/.pre-commit-config.yaml" \
  && rg -Fq 'scripts/test-verify-agent-hygiene\.sh' "$repo_root/.pre-commit-config.yaml"; then
  ok "agent-canon pre-commit hook watches its skill and verifier inputs"
else
  no "agent-canon pre-commit hook must watch its skill and verifier inputs"
fi

if rg -Fq '.opencode/agent/**' "$repo_root/specs/ci-gates.v1.yaml" \
  && rg -Fq 'scripts/verify-agent-canon.sh' "$repo_root/specs/ci-gates.v1.yaml" \
  && rg -Fq 'scripts/test-verify-agent-hygiene.sh' "$repo_root/specs/ci-gates.v1.yaml"; then
  ok "agent-canon registry watches all policy and verifier inputs"
else
  no "agent-canon registry must watch all policy and verifier inputs"
fi

# --- verify-no-ai-attribution --message ---
printf 'feat: a clean message\n' >"$tmp/msg-clean"
if "$attr" --message "$tmp/msg-clean" >/dev/null 2>&1; then
  ok "attribution passes on a clean commit message"
else
  no "attribution should pass on a clean message"
fi

printf 'feat: x\n\nCo-authored-by: Claude <noreply@anthropic.com>\n' >"$tmp/msg-coauth"
if "$attr" --message "$tmp/msg-coauth" >/dev/null 2>&1; then
  no "attribution should fail on a Co-authored-by trailer"
else
  ok "attribution fails on a Co-authored-by trailer"
fi

printf 'feat: x\n\n🤖 Generated with Claude Code\n' >"$tmp/msg-robot"
if "$attr" --message "$tmp/msg-robot" >/dev/null 2>&1; then
  no "attribution should fail on the robot-emoji footer"
else
  ok "attribution fails on the robot-emoji footer"
fi

# Regression for the Codex P2 on #3988: a normal HUMAN Co-authored-by trailer
# (no AI tool / no anthropic address) must NOT be flagged — the rule is about
# AI attribution, and the repo already has human co-author trailers.
printf 'feat: x\n\nCo-authored-by: Jane Doe <jane@example.com>\n' >"$tmp/msg-human"
if "$attr" --message "$tmp/msg-human" >/dev/null 2>&1; then
  ok "attribution passes on a human Co-authored-by trailer"
else
  no "attribution should NOT flag a human Co-authored-by trailer"
fi

# Nudge-reachability check. A skill nobody can reach from the editor hook is
# the silent half of skill routing: the hook keeps exiting 0, so only a gate
# notices. Pin all three ways it can be wrong.
mkdir -p "$tmp/nudge/.agents/skills/eshu-performance-rigor/references" \
  "$tmp/nudge/.claude/skills" "$tmp/nudge/.codex/skills"
printf 'shared canon\n' >"$tmp/nudge/AGENTS.md"
printf 'shared canon\n' >"$tmp/nudge/CLAUDE.md"
cat >"$tmp/nudge/.agents/skills/eshu-performance-rigor/SKILL.md" <<'NUDGE_PERF_SKILL'
## Target Contribution Budget
required_saving_seconds maximum_recoverable_seconds expected_saving_seconds
## Resource-Qualified Claims
absolute_target_applicable same-machine relative
## Baseline Promotion
## Retention Modes
stop-and-preserve git merge-base --is-ancestor
NUDGE_PERF_SKILL
cat >"$tmp/nudge/.agents/skills/eshu-performance-rigor/references/run-manifest.md" <<'NUDGE_PERF_MANIFEST'
target_contribution phase_durations_seconds retention accepted_commit
hardware_class machine_profile reference_profile resource_envelope memory_bytes
container_memory_limit_bytes absolute_target_applicable compose_service_limits
service_usage_summary
NUDGE_PERF_MANIFEST
ln -s ../../.agents/skills/eshu-performance-rigor \
  "$tmp/nudge/.claude/skills/eshu-performance-rigor"
ln -s ../../.agents/skills/eshu-performance-rigor \
  "$tmp/nudge/.codex/skills/eshu-performance-rigor"
write_nudge_fixture "$tmp/nudge"

# Control: the fixture must PASS before the orphan is introduced. Without this,
# every "fails" assertion below could be failing for an unrelated reason and
# still read as proof.
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/nudge" "$canon" >/dev/null 2>&1; then
  ok "nudge fixture is clean before the orphan skill is added"
else
  no "nudge fixture should be clean before the orphan skill is added"
fi

mkdir -p "$tmp/nudge/.agents/skills/orphan"
printf '%s\n' '---' 'name: orphan' 'description: orphan' '---' \
  >"$tmp/nudge/.agents/skills/orphan/SKILL.md"
ln -s ../../.agents/skills/orphan "$tmp/nudge/.claude/skills/orphan"
ln -s ../../.agents/skills/orphan "$tmp/nudge/.codex/skills/orphan"

rm "$tmp/nudge/.claude/hooks/skill-nudge.sh"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/nudge" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail when the nudge hook is missing entirely"
else
  ok "agent-canon fails when the nudge hook is missing entirely"
fi

write_nudge_fixture "$tmp/nudge"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/nudge" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail when a skill has neither an arm nor an exemption"
else
  ok "agent-canon fails when a skill has neither an arm nor an exemption"
fi

# A bare mention in a comment must NOT satisfy the check. This is the
# tautological-guard case: scoping the match to the whole file would pass here.
printf '# orphan is mentioned only in prose\n' \
  >>"$tmp/nudge/.claude/hooks/skill-nudge.sh"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/nudge" "$canon" >/dev/null 2>&1; then
  no "agent-canon should not accept a skill named only in an unrelated comment"
else
  ok "agent-canon rejects a skill named only in an unrelated comment"
fi

# A longer skill name that merely CONTAINS an existing one must not count as
# covered. Substring matching would pass this and leave the new skill unrouted.
printf 'SKILL="orphan-extended"\n' >>"$tmp/nudge/.claude/hooks/skill-nudge.sh"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/nudge" "$canon" >/dev/null 2>&1; then
  no "agent-canon should not accept orphan covered by an orphan-extended arm"
else
  ok "agent-canon rejects a prefix match against a longer skill name"
fi

printf 'SKILL="orphan"\n' >>"$tmp/nudge/.claude/hooks/skill-nudge.sh"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/nudge" "$canon" >/dev/null 2>&1; then
  ok "agent-canon passes once the skill has a real SKILL= arm"
else
  no "agent-canon should pass once the skill has a real SKILL= arm"
fi

printf '\nagent-hygiene test mirror: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
