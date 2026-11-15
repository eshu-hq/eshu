#!/usr/bin/env bash
# test-agent-skill-census-cases.sh — the skill-census cases for the
# agent-canon test mirror. Sourced, not run on its own.
#
# The shared skills live in the eshu-agent-kit. The installer puts every
# one of them in the harness trees under its harness-visible name, while
# .agents/skills carries only the ones the alias manifest does not rename.
# So the canon gate's census is the harness tree, and these cases build
# kit-shaped fixtures: a skill under .agents/skills, its alias wrappers in
# .claude/skills and .codex/skills, and the .kit-alias marker the gate
# reads to prove the wrapper resolves to that skill.
#
# A trigger path for the agent-canon gate, so editing it alone still
# selects the gate that runs it.

# shellcheck source=scripts/lib/test-agent-alias-cases.sh
source "$here/lib/test-agent-alias-cases.sh"

mkdir -p "$tmp/skill-links/.agents/skills/example" \
  "$tmp/skill-links/.agents/skills/eshu-performance-rigor/references" \
  "$tmp/skill-links/.claude/skills" \
  "$tmp/skill-links/.codex/skills"
write_nudge_fixture "$tmp/skill-links"
printf 'shared canon\n' >"$tmp/skill-links/AGENTS.md"
printf '%s\n' '---' 'name: example' 'description: example' '---' \
  >"$tmp/skill-links/.agents/skills/example/SKILL.md"
make_alias_fixture "$tmp/skill-links" .claude example
make_alias_fixture "$tmp/skill-links" .codex example
# The census is the harness tree, so the performance skill's contract must
# live in the harness copies the gate reads. The .agents/ copy is the
# canonical source the installer links from.
cat >"$tmp/skill-links/.agents/skills/eshu-performance-rigor/SKILL.md" <<'LINK_PERF_SKILL'
---
name: eshu-performance-rigor
description: perf fixture
---
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
for harness in .claude .codex; do
  mkdir -p "$tmp/skill-links/$harness/skills/eshu-performance-rigor/references"
  cp "$tmp/skill-links/.agents/skills/eshu-performance-rigor/SKILL.md" \
    "$tmp/skill-links/$harness/skills/eshu-performance-rigor/SKILL.md"
  cp "$tmp/skill-links/.agents/skills/eshu-performance-rigor/references/run-manifest.md" \
    "$tmp/skill-links/$harness/skills/eshu-performance-rigor/references/run-manifest.md"
  make_alias_marker_fixture "$tmp/skill-links" "$harness" eshu-performance-rigor
done
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/skill-links" "$canon" >/dev/null 2>&1; then
  ok "agent-canon passes when shared skill harness aliases are complete"
else
  no "agent-canon should pass when shared skill harness aliases are complete"
fi

run_agent_alias_marker_cases
# The kit-fidelity cases prove the wrapper BODIES against the kit source,
# not just their names. They restore the fixture tree they use, so they run
# after the marker cases and before the drift case below.
run_agent_kit_fidelity_cases

rm -r "$tmp/skill-links/.codex/skills/example"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/skill-links" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail when one harness cannot discover a shared skill"
else
  ok "agent-canon fails when a shared skill harness alias is missing"
fi

mkdir -p "$tmp/perf-contract/.agents/skills/eshu-performance-rigor/references" \
  "$tmp/perf-contract/.claude/skills" \
  "$tmp/perf-contract/.codex/skills"
write_nudge_fixture "$tmp/perf-contract"
printf 'shared canon\n' >"$tmp/perf-contract/AGENTS.md"
printf '%s\n' '---' 'name: eshu-performance-rigor' 'description: incomplete' '---' \
  >"$tmp/perf-contract/.agents/skills/eshu-performance-rigor/SKILL.md"
printf '# Performance Run Manifest\n' \
  >"$tmp/perf-contract/.agents/skills/eshu-performance-rigor/references/run-manifest.md"
# The gate reads the harness tree, so the contract the fixture grows must
# live there. The .agents/ copy stays the canonical source.
for harness in .claude .codex; do
  mkdir -p "$tmp/perf-contract/$harness/skills/eshu-performance-rigor/references"
  cp "$tmp/perf-contract/.agents/skills/eshu-performance-rigor/SKILL.md" \
    "$tmp/perf-contract/$harness/skills/eshu-performance-rigor/SKILL.md"
  cp "$tmp/perf-contract/.agents/skills/eshu-performance-rigor/references/run-manifest.md" \
    "$tmp/perf-contract/$harness/skills/eshu-performance-rigor/references/run-manifest.md"
  make_alias_marker_fixture "$tmp/perf-contract" "$harness" eshu-performance-rigor
done
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/perf-contract" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail when the performance workflow contract is incomplete"
else
  ok "agent-canon fails when the performance workflow contract is incomplete"
fi

# The contract grows in the harness copies the gate reads. Each growth step
# names the skill file and the manifest file separately: a single heredoc
# cannot feed two `cat` calls, because the first would consume the whole
# stream and the second would get nothing.
grow_perf_fixture() {
  local harness skill_text="$1" manifest_text="$2"
  for harness in .claude .codex; do
    printf '%s' "$skill_text" \
      >>"$tmp/perf-contract/$harness/skills/eshu-performance-rigor/SKILL.md"
    printf '%s' "$manifest_text" \
      >>"$tmp/perf-contract/$harness/skills/eshu-performance-rigor/references/run-manifest.md"
  done
}

grow_perf_fixture '## Target Contribution Budget
required_saving_seconds maximum_recoverable_seconds expected_saving_seconds
## Baseline Promotion
## Retention Modes
stop-and-preserve
git merge-base --is-ancestor
' 'target_contribution
phase_durations_seconds
retention
accepted_commit
hardware_class
'
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/perf-contract" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail when the performance resource envelope is missing"
else
  ok "agent-canon fails when the performance resource envelope is missing"
fi

grow_perf_fixture '## Resource-Qualified Claims
absolute_target_applicable
same-machine relative
' 'reference_profile
machine_profile
resource_envelope
memory_bytes
container_memory_limit_bytes
absolute_target_applicable
compose_service_limits
service_usage_summary
'
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/perf-contract" "$canon" >/dev/null 2>&1; then
  ok "agent-canon passes when the performance workflow contract is complete"
else
  no "agent-canon should pass when the performance workflow contract is complete"
fi

# The mandatory performance skill is missing from the census the gate reads.
for harness in .claude .codex; do
  mv "$tmp/perf-contract/$harness/skills/eshu-performance-rigor/SKILL.md" \
    "$tmp/perf-contract/$harness/performance-skill.saved"
done
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/perf-contract" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail when the mandatory performance skill is missing"
else
  ok "agent-canon fails when the mandatory performance skill is missing"
fi
for harness in .claude .codex; do
  mv "$tmp/perf-contract/$harness/performance-skill.saved" \
    "$tmp/perf-contract/$harness/skills/eshu-performance-rigor/SKILL.md"
done
