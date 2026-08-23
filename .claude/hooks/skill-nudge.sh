#!/bin/bash
# skill-nudge.sh — PreToolUse hook for Edit|Write. Non-blocking: injects a
# one-time-per-session reminder to load the project skill matching the file
# being edited. This is the push-based fix for "forgot to load the skill
# after compaction". Never blocks; fires once per (session, skill).
#
# This table lives in the Eshu repo on purpose. It maps Eshu source paths to
# Eshu project skills, so it goes stale the moment a directory moves or a skill
# is renamed. `scripts/verify-agent-canon.sh` asserts that every skill under
# .agents/skills/ either has an arm below or is named in NUDGE_EXEMPT, which
# turns that staleness into a red gate instead of a silent no-op.
set -u

INPUT=$(cat)
# No eval: file_path can carry shell metacharacters (injection risk).
SID=$(printf '%s' "$INPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("session_id","na")[:12])' 2>/dev/null)
FP=$(printf '%s' "$INPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("tool_input",{}).get("file_path") or "")' 2>/dev/null)
[ -z "${FP:-}" ] && exit 0

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

SKILL=""
# bash `case` takes the FIRST match, so the narrow gate surfaces below must stay
# above the broad `.github/workflows/*` and `scripts/verify-*` arms. Editing
# security-scan.yml used to nudge generator-script-discipline, which is not the
# skill that owns it.
case "$FP" in
  *.github/workflows/security-scan.yml)          SKILL="eshu-security-scan-gates";;
  */go/go.mod|*/go/go.sum)                       SKILL="eshu-security-scan-gates + golang-engineering (toolchain or dependency bump moves govulncheck/nancy)";;
  */internal/telemetry/*|*/scripts/verify-telemetry-coverage.sh|*eshu-operator-overview.json|*/observability/*) SKILL="telemetry-coverage-discipline";;
  */internal/queue/*)                            SKILL="concurrency-deadlock-rigor + eshu-postgres-rigor";;
  */docs/internal/remote-validation/*|*/docs/internal/evidence/*|*/scripts/run-remote-e2e-*|*_bench_test.go) SKILL="eshu-performance-rigor";;
  */storage/cypher/*|*cypher*|*/storage/neo4j/*) SKILL="cypher-query-rigor";;
  */internal/reducer/*|*/internal/projector/*)   SKILL="eshu-correlation-truth + eshu-diagnostic-rigor";;
  */storage/postgres/*|*.sql)                    SKILL="eshu-postgres-rigor";;
  */factschema/*|*fact-kind-registry*|*fixturepack*) SKILL="eshu-contract-rigor";;
  */testdata/cassettes/*|*/testdata/golden/*|*golden-corpus*) SKILL="eshu-golden-corpus-rigor";;
  */internal/mcp/*|*/internal/query/*)           SKILL="eshu-mcp-call-rigor (for tool contracts)";;
  *.github/workflows/*|*/scripts/verify-*)       SKILL="generator-script-discipline / gate BITES-proof discipline";;
  */tools/golangci-lint-dirgate/*|*dirgate-grandfather*) SKILL="golang-engineering (dirgate: RECOMPUTE the pin, never side-pick; a count match is not a digest match)";;
  */cmd/eshu/*|*/internal/cli/*)                 SKILL="golang-engineering + eshu-folder-doc-keeper (cobra wrapper vs package split; doc.go/README/AGENTS lockstep)";;
  */doc.go|*/AGENTS.md|*/README.md)              SKILL="eshu-folder-doc-keeper";;
  # Fallback, and it MUST stay last. Every arm above names a specialist skill
  # for a specific surface; this one catches the rest of the Go tree, which the
  # specialist arms miss entirely -- nothing covered go/cmd/ci-gates,
  # go/cmd/api, go/cmd/reducer, go/cmd/ingester, or go/cmd/bootstrap-index.
  # Placed any earlier it swallows */doc.go and the surface arms above it,
  # the same first-match trap that mis-routed security-scan.yml.
  # Noise cost is one nudge per session, not per edit: the stamp below is keyed
  # on (session, skill).
  *.go)                                          SKILL="golang-engineering";;
esac
[ -z "$SKILL" ] && exit 0

STAMP="/tmp/claude-nudge-${SID}-$(printf '%s' "$SKILL" | tr -cd 'a-z-' | cut -c1-30)"
[ -f "$STAMP" ] && exit 0
touch "$STAMP" 2>/dev/null

python3 -c '
import json,sys
skill=sys.argv[1]
print(json.dumps({"hookSpecificOutput":{"hookEventName":"PreToolUse",
 "additionalContext":"REMINDER (auto-hook): you are editing a surface governed by the project skill(s): "+skill+". If not already loaded THIS context window (compaction clears them), invoke via the Skill tool before continuing. TDD applies: failing test first."}}))
' "$SKILL"
exit 0
