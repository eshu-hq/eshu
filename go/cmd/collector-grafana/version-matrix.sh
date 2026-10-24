#!/bin/bash
# Version-pair matrix DRAFT — collector-grafana (#8857).
#
# Prints the in-tree contract versions the extracted collector must stay
# compatible with, plus the core/collector version-pair table to fill once
# the standalone release exists. External pairs are NOT_CHECKED until the
# extracted repository publishes its first supported release.
#
# Credential/provenance notes (in-tree path, locked by this PREP):
# - Grafana tokens resolve only from the environment at startup
#   (config.go: requiredEnvValue of target.TokenEnv via getenv). They live
#   in the runtime-only TargetConfig.Token, travel in the Grafana
#   "Authorization" HTTP header, and must never reach desired collector
#   config output, facts, logs, metric labels, or docs.
# - Every emitted envelope carries provenance: SourceRef.SourceSystem
#   (grafana), ScopeID, GenerationID, FactKey (stable key, generation
#   bound), fencing token, observed_at, and SourceRecordID. Dashboard
#   URLs, query models, contact points, and notification URLs are
#   fingerprint-or-flag redacted before emission (redaction.go).
# - Cassette replay is the credential-free provenance path: -mode=cassette
#   serves pre-recorded envelopes via buildCassetteService; no live
#   credentials required.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# ROOT is the go/ module directory (script lives at go/cmd/<name>/).
FAIL=0

check() {
  local desc="$1" got="$2" want="$3"
  if [[ "$got" == "$want" ]]; then
    printf 'PASS  %-42s %s\n' "$desc" "$got"
  else
    printf 'FAIL  %-42s got %s want %s\n' "$desc" "$got" "$want"
    FAIL=1
  fi
}

echo "== in-tree contract versions (locked) =="
check "go toolchain (go.mod)" "$(rg -o '^go [0-9.]+' "$ROOT/go.mod" | head -n 1)" "go 1.26.10"
check "fact schema version (V1 const)" \
  "$(rg -o 'ObservabilitySchemaVersionV1 = "[0-9.]+"' "$ROOT/internal/facts/observability.go" | head -n 1)" 'ObservabilitySchemaVersionV1 = "1.0.0"'
check "emitted grafana kind count (PREP)" \
  "$(rg -o 'Observability(SourceInstance|ObservedDashboard|ObservedRule|CoverageWarning)FactKind = ' "$ROOT/internal/facts/observability.go" | sort -u | wc -l | tr -d ' ')" "4"
check "cassette schema version" \
  "$(rg -o 'SchemaVersionV1 = "[0-9]+"' "$ROOT/internal/replay/cassette/format.go" | head -n 1)" 'SchemaVersionV1 = "1"'
check "grafana PREP census present" \
  "$([ -f "$ROOT/cmd/collector-grafana/EXTRACTION_PREP.md" ] && echo yes || echo no)" "yes"

echo ""
echo "== core/collector version-pair matrix (DRAFT) =="
printf '%-16s %-16s %-14s %s\n' "core" "collector" "status" "note"
printf '%-16s %-16s %-14s %s\n' "in-tree" "in-tree" "LOCKED" "this worktree; mirror tests prove AC3 here"
printf '%-16s %-16s %-14s %s\n' "TBD-oldest" "0.1.0" "NOT_CHECKED" "fill at first extracted release (#6709 grant + #4047 gate)"
printf '%-16s %-16s %-14s %s\n' "TBD-newest" "TBD" "NOT_CHECKED" "fill at cutover rehearsal; replay committed generation first"

echo ""
echo "== credential/provenance checklist =="
echo "- token_env resolved via getenv only (config.go); header transport, never query parameter"
echo "- URLs fingerprinted + url_redacted; query models, contacts, notification URLs flag-redacted"
echo "- envelopes carry collector_instance_id, fencing token, generation, observed_at"

exit "$FAIL"
