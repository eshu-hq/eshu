# Evidence: #5572 per-address module-resolution-confidence signal ("derived" outcome)

This note carries the performance/no-regression and observability evidence
markers `scripts/verify-performance-evidence.sh` requires for the hot-path
files this change touches.

Touched hot-path files this covers:

- `go/internal/storage/postgres/tfstate_drift_evidence.go`
- `go/internal/storage/postgres/tfstate_drift_evidence_config_row.go`
- `go/internal/storage/postgres/tfstate_drift_evidence_module_prefix.go`
- `go/internal/storage/postgres/tfstate_drift_evidence_module_confidence.go`
- `go/internal/storage/postgres/tfstate_drift_evidence_prior_config.go`
- `go/internal/storage/postgres/aws_cloud_runtime_drift_evidence.go`
- `go/internal/storage/postgres/terraform_config_state_drift_findings.go`
- `go/internal/reducer/terraform_config_state_drift_writer.go`

## What changed and why it does not add a new query, join, or scan pattern

`buildModulePrefixMap` already read every `terraform_modules` fact for the
generation and already walked the module-call graph once per generation. This
change adds zero new Postgres queries: it populates a second, in-memory
`moduleResolutionConfidenceMap` (a plain `map[string]string`) alongside the
existing `modulePrefixMap` from data the SAME query result set already
produced, and records into it either (a) once per `external_registry`-
classified call (a single extra `resolveLocalCallee` string join+clean, the
same O(1) operation `classifyModuleSource` already performs for every call)
or (b) once per `depth_exceeded` event (already a rare, bounded-depth event —
`maxModulePrefixDepth` caps the recursion at 10).

Per `terraform_resources` entry, `emitConfigRowsForEntry` now does ONE
additional directory-walk-up lookup
(`moduleResolutionConfidenceMap.reasonForPath`), which is the exact same
walk-up-the-directory-chain algorithm `modulePrefixMap.modulePrefixForPath`
already performs for every entry — same O(directory depth) shape, doubling
the constant factor of an already-bounded (`maxModulePrefixDepth` = 10)
per-entry walk. No loop was changed from O(1) to O(n) or worse; no new
per-entry Postgres round trip was added; the four SQL queries
`LoadDriftEvidence` issues are unchanged in count, shape, and predicates.

- No-Regression Evidence (#5572): `cd go && go test
  ./internal/correlation/drift/tfconfigstate/...
  ./internal/storage/postgres/... ./internal/reducer/... ./internal/query/...
  ./internal/mcp/... -count=1` is green, including the full pre-existing
  module-aware-joining suite (`TestLoadConfigByAddressAppliesModulePrefixForCalleeFiles`,
  `TestLoadConfigByAddressExpandsSameCalleeForMultipleCallers`,
  `TestLoadConfigByAddressNestedChainProducesMultiLevelAddress`,
  `TestLoadConfigByAddressRootModuleResourcesKeepIdenticalAddress`,
  `TestLoadPriorConfigAddressesAppliesModulePrefix`,
  `TestLoadPriorConfigAddressesUsesPriorGenerationModulePrefixOnRename`) with
  byte-identical addresses and prefix counts to before this change --
  proving the new confidence bookkeeping is purely additive and does not
  alter any existing address, join, or drift classification. The complexity
  argument above (same query count, same per-entry walk shape, bounded event
  counts) is the proof; there is no separate query-plan or throughput claim
  to benchmark because no query, index, or scan changed.
- Observability Evidence (#5572): no new metric or span. The change reuses
  the existing `eshu_dp_drift_unresolved_module_calls_total{reason}` counter
  path unchanged (issue #169) and adds a per-finding
  `terraform_module_resolution_confidence` evidence atom that flows through
  the SAME `Evidence []map[string]any` field the drift finding's payload
  already serializes and the SAME
  `POST /api/v0/terraform/config-state-drift/findings` response the finding
  row already returns -- no new response field, no new log line, no new
  telemetry instrument. `TestPostgresTerraformConfigStateDriftWriterDowngradesOutcomeToDerivedWhenModuleResolutionReasonPresent`
  proves the evidence atom and the downgraded `outcome="derived"` both reach
  the durable row.
