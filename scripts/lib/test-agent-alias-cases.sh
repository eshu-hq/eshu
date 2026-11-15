#!/usr/bin/env bash
# test-agent-alias-cases.sh — installer alias-wrapper fixtures and the
# marker-missing seeded pair for the agent-canon test mirror. Sourced, not
# executed: defines make_alias_fixture for fixture setup and
# run_agent_alias_marker_cases for the seeded RED/GREEN pair, both run by
# scripts/test-verify-agent-hygiene.sh against its own $tmp and $canon.
# shellcheck disable=SC2148

# make_alias_fixture <root> <harness> <skill> — installer-shaped harness
# alias wrapper: a directory carrying the .kit-alias marker and a SKILL.md
# under the same name, which is what `install.sh --repo --aliases` generates.
make_alias_fixture() {
  local root="$1" harness="$2" skill="$3"
  mkdir -p "$root/$harness/skills/$skill"
  printf -- '---\nname: %s\ndescription: %s fixture\n---\n\n# %s\n' "$skill" "$skill" "$skill" >"$root/$harness/skills/$skill/SKILL.md"
  printf 'alias=%s source=%s\n' "$skill" "$skill" >"$root/$harness/skills/$skill/.kit-alias"
}

# make_alias_marker_fixture <root> <harness> <skill> — installer-shaped
# harness alias wrapper that keeps whatever SKILL.md the caller already
# wrote, and only adds the .kit-alias marker. The frontmatter cases need
# this: make_alias_fixture overwrites SKILL.md with a generic description,
# so a fixture that means to exercise a specific description would silently
# test the generic one instead.
make_alias_marker_fixture() {
  local root="$1" harness="$2" skill="$3"
  mkdir -p "$root/$harness/skills/$skill"
  printf 'alias=%s source=%s\n' "$skill" "$skill" >"$root/$harness/skills/$skill/.kit-alias"
}

# run_agent_alias_marker_cases — seeded pair: the canon gate must fail when
# a harness alias wrapper loses its .kit-alias marker and pass again once
# the marker is restored, proving the marker (not the directory) is the
# resolution signal. Runs against the skill-links fixture root.
run_agent_alias_marker_cases() {
  rm "$tmp/skill-links/.codex/skills/example/.kit-alias"
  if ESHU_AGENT_CANON_REPO_ROOT="$tmp/skill-links" "$canon" >/dev/null 2>&1; then
    no "agent-canon should fail when a harness alias wrapper loses its marker"
  else
    ok "agent-canon fails when a harness alias wrapper loses its marker"
  fi
  printf 'alias=example source=example\n' >"$tmp/skill-links/.codex/skills/example/.kit-alias"
  if ESHU_AGENT_CANON_REPO_ROOT="$tmp/skill-links" "$canon" >/dev/null 2>&1; then
    ok "agent-canon passes again once the alias marker is restored"
  else
    no "agent-canon should pass again once the alias marker is restored"
  fi
}

# run_agent_kit_fidelity_cases — body proof against the kit source, the
# five cases the name-only check cannot make: with the kit checkout beside
# the repo the gate accepts installer-shaped wrappers (GREEN), fails on a
# hostile same-name body (RED), fails on matching same-name bodies that
# drift from the kit source (the isolated kit-proof RED: cross-tree passes,
# so only the body comparison can fire), still passes without the kit
# checkout (the skip path is the name/marker floor, not an accident), and
# fails when the two harness trees drift apart with the kit absent (the
# cross-tree cmp alone). Together they prove every fail branch of the new
# checks. Leaves both trees fixture-shaped and no kit dir for the sibling
# marker-missing cases that follow.
run_agent_kit_fidelity_cases() {
  local kit="$tmp/skill-links/eshu-agent-kit"
  mkdir -p "$kit/skills/eshu/example" "$kit/skills/eshu/eshu-performance-rigor" \
    "$kit/templates/repo-overlay"
  # The kit sources are copies of the wrappers the census already built, so
  # they match byte for byte by construction. A hand-written stub here would
  # drift from the wrapper and the body proof would (correctly) fail on the
  # GREEN case.
  cp "$tmp/skill-links/.claude/skills/example/SKILL.md" \
    "$kit/skills/eshu/example/SKILL.md"
  cp "$tmp/skill-links/.claude/skills/eshu-performance-rigor/SKILL.md" \
    "$kit/skills/eshu/eshu-performance-rigor/SKILL.md"
  mkdir -p "$kit/skills/eshu/eshu-performance-rigor/references"
  cp "$tmp/skill-links/.claude/skills/eshu-performance-rigor/references/run-manifest.md" \
    "$kit/skills/eshu/eshu-performance-rigor/references/run-manifest.md"
  printf 'example\texample\neshu-performance-rigor\teshu-performance-rigor\n' \
    >"$kit/templates/repo-overlay/eshu-aliases.tsv"
  if ESHU_AGENT_CANON_REPO_ROOT="$tmp/skill-links" "$canon" >/dev/null 2>&1; then
    ok "agent-canon proves wrapper bodies against the kit source (green)"
  else
    no "agent-canon should pass on installer-shaped wrappers with a kit source"
  fi
  printf -- '---\nname: example\ndescription: example fixture\n---\n\n# HOSTILE BODY\n' \
    >"$tmp/skill-links/.claude/skills/example/SKILL.md"
  if ESHU_AGENT_CANON_REPO_ROOT="$tmp/skill-links" "$canon" >/dev/null 2>&1; then
    no "agent-canon should fail on a same-name wrapper body that drifts from the kit"
  else
    ok "agent-canon fails on a same-name wrapper body that drifts from the kit"
  fi
  # Isolated kit-proof RED: identical bodies in both trees, so the cross-tree
  # cmp passes and only the kit-source comparison can fail.
  printf -- '---\nname: example\ndescription: example fixture\n---\n\n# HOSTILE BODY\n' \
    >"$tmp/skill-links/.codex/skills/example/SKILL.md"
  if ESHU_AGENT_CANON_REPO_ROOT="$tmp/skill-links" "$canon" >/dev/null 2>&1; then
    no "agent-canon should fail on matching wrapper bodies that drift from the kit"
  else
    ok "agent-canon fails on matching wrapper bodies that drift from the kit source"
  fi
  make_alias_fixture "$tmp/skill-links" .claude example
  make_alias_fixture "$tmp/skill-links" .codex example
  rm -r "$kit"
  if ESHU_AGENT_CANON_REPO_ROOT="$tmp/skill-links" "$canon" >/dev/null 2>&1; then
    ok "agent-canon keeps its name/marker floor without a kit checkout"
  else
    no "agent-canon should pass without a kit checkout (name/marker floor)"
  fi
  printf -- '---\nname: example\ndescription: codex-only body\n---\n\n# example\n' \
    >"$tmp/skill-links/.codex/skills/example/SKILL.md"
  if ESHU_AGENT_CANON_REPO_ROOT="$tmp/skill-links" "$canon" >/dev/null 2>&1; then
    no "agent-canon should fail when the two harness trees carry different bodies"
  else
    ok "agent-canon fails when the two harness trees carry different bodies"
  fi
  make_alias_fixture "$tmp/skill-links" .codex example
}
