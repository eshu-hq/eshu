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
