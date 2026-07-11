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

mkdir -p "$tmp/skill-links/.agents/skills/example" \
  "$tmp/skill-links/.agents/skills/eshu-performance-rigor/references" \
  "$tmp/skill-links/.claude/skills" \
  "$tmp/skill-links/.codex/skills"
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

printf '\nagent-hygiene test mirror: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
