#!/bin/bash
# skill-nudge.sh — PreToolUse hook for Edit|MultiEdit|Write. Refuses an edit to
# a surface governed by a project skill until that skill is loaded in the
# current context window.
#
# This was advisory once: exit 0 with additionalContext, a sentence in front of
# the agent and nothing more. Measured in the session that first observed it
# firing, it fired three times and zero of the named skills were loaded. An
# advisory hook is prose delivered by a hook, which is the tier the repo's
# prose -> skill -> hook ladder already found insufficient. It now exits 2,
# which fails the tool call.
#
# The requirement is satisfied by `.claude/hooks/skill-loaded.sh`, a PostToolUse
# hook on the Skill tool that records what was actually loaded. So this checks
# loading, not acknowledgement: retrying without loading is refused again.
#
# This table maps Eshu source paths to Eshu project skills and lives in the Eshu
# repo for that reason. `scripts/verify-agent-canon.sh` asserts every skill
# under .agents/skills/ is either assigned by an IDS= arm or named in
# NUDGE_EXEMPT, so a directory move or a new skill cannot silently unroute it.
set -u

INPUT=$(cat)

# Only act inside an Eshu checkout. This hook can be installed at user level in
# ~/.claude/settings.json, which puts it in front of every repository on the
# machine -- and blocking an edit in someone else's Go project over an Eshu
# skill would be indefensible. Walk up from the edited file looking for a marker
# only Eshu has; works from the main checkout and from any worktree.
eshu_root() {
  local d="${1:-}"
  [ -n "$d" ] || return 1
  [ -d "$d" ] || d="$(dirname "$d")"
  while [ -n "$d" ] && [ "$d" != "/" ] && [ "$d" != "." ]; do
    [ -e "$d/.agents/skills/eshu-code-review" ] && return 0
    d="$(dirname "$d")"
  done
  return 1
}

command -v python3 >/dev/null 2>&1 || exit 0

# No eval: file_path can carry shell metacharacters (injection risk).
SID=$(printf '%s' "$INPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("session_id","na")[:12])' 2>/dev/null)
FP=$(printf '%s' "$INPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("tool_input",{}).get("file_path") or "")' 2>/dev/null)
[ -z "${FP:-}" ] && exit 0
eshu_root "$FP" || exit 0

# NUDGE_EXEMPT_BEGIN
# Skills with no characteristic file path. They are triggered by an event or an
# intent, so a PreToolUse Edit|Write arm could never fire for them. Listed so
# scripts/verify-agent-canon.sh can tell "deliberately exempt" from "forgotten".
#   eshu-code-review        runs against a final diff, not a path
#   eshu-humanizer          runs over prose headed for a human reader
#   eshu-issue-driver       invoked with an issue or epic number
#   eshu-release            invoked by release intent
#   resolve-review-threads  invoked with a PR number
#   eshu-session-lifecycle  session events; nudged by .claude/hooks/on-compact.sh
# NUDGE_EXEMPT_END

# IDS is the canonical, space-separated list this hook enforces. NOTE is the
# human half -- guidance that belongs in the message but is not a skill id.
# Keep them separate: matching on the prose would make "dirgate" a skill.
IDS=""
NOTE=""
# bash `case` takes the FIRST match, so the narrow gate surfaces below must stay
# above the broad `.github/workflows/*` and `scripts/verify-*` arms, and the
# `*.go` fallback must stay last of all.
case "$FP" in
  *.github/workflows/security-scan.yml)          IDS="eshu-security-scan-gates";;
  */go/go.mod|*/go/go.sum)                       IDS="eshu-security-scan-gates golang-engineering"
                                                 NOTE="a toolchain or dependency bump moves govulncheck and nancy";;
  */internal/telemetry/*|*/scripts/verify-telemetry-coverage.sh|*eshu-operator-overview.json|*/observability/*) IDS="telemetry-coverage-discipline";;
  */internal/queue/*)                            IDS="concurrency-deadlock-rigor eshu-postgres-rigor";;
  */docs/internal/remote-validation/*|*/docs/internal/evidence/*|*/scripts/run-remote-e2e-*|*_bench_test.go) IDS="eshu-performance-rigor";;
  */storage/cypher/*|*cypher*|*/storage/neo4j/*) IDS="cypher-query-rigor";;
  */internal/reducer/*|*/internal/projector/*)   IDS="eshu-correlation-truth eshu-diagnostic-rigor";;
  */storage/postgres/*|*.sql)                    IDS="eshu-postgres-rigor";;
  */factschema/*|*fact-kind-registry*|*fixturepack*) IDS="eshu-contract-rigor";;
  */testdata/cassettes/*|*/testdata/golden/*|*golden-corpus*) IDS="eshu-golden-corpus-rigor";;
  */internal/mcp/*|*/internal/query/*)           IDS="eshu-mcp-call-rigor"
                                                 NOTE="for tool contracts";;
  *.github/workflows/*|*/scripts/verify-*)       IDS="generator-script-discipline";;
  */tools/golangci-lint-dirgate/*|*dirgate-grandfather*) IDS="golang-engineering"
                                                 NOTE="dirgate: RECOMPUTE the pin, never side-pick; a count match is not a digest match";;
  */cmd/eshu/*|*/internal/cli/*)                 IDS="golang-engineering eshu-folder-doc-keeper"
                                                 NOTE="cobra wrapper vs package split; doc.go/README/AGENTS lockstep";;
  */doc.go|*/AGENTS.md|*/README.md)              IDS="eshu-folder-doc-keeper";;
  *.go)                                          IDS="golang-engineering";;
esac
[ -z "$IDS" ] && exit 0

# Which required skills are not loaded in THIS context window?
MISSING=""
for id in $IDS; do
  [ -f "/tmp/claude-skill-loaded-${SID}-${id}" ] && continue
  MISSING="${MISSING}${MISSING:+ }${id}"
done
[ -z "$MISSING" ] && exit 0

# Explicit, deliberate escape hatch. A skill can be genuinely unloadable -- most
# often when it lives on a branch the session's project directory cannot see --
# and a permanent block on editing a whole surface is worse than the rule it
# enforces. This is not an automatic degrade: nothing here relaxes on its own,
# because a silent fallback would hide exactly the case worth knowing about.
OVERRIDE="/tmp/claude-skill-override-${SID}"
[ -f "$OVERRIDE" ] && exit 0

printf 'BLOCKED (auto-hook): %s\n' "$FP" >&2
printf 'That surface is governed by project skill(s) not loaded in this context: %s\n' "$MISSING" >&2
[ -n "$NOTE" ] && printf 'Note: %s\n' "$NOTE" >&2
printf 'Invoke each via the Skill tool, then retry. Compaction clears loaded skills, so having loaded it earlier in the session does not count.\n' >&2
printf 'TDD applies: failing test first.\n' >&2
printf 'If a skill genuinely cannot be loaded here, say so and run: touch %s\n' "$OVERRIDE" >&2
exit 2
