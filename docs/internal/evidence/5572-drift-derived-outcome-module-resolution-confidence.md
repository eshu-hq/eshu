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

## Golden-corpus coverage per cause (review follow-up)

Issue #5572's two documented causes get DIFFERENT proof tiers, a deliberate,
stated decision rather than a silent gap:

- **`external_registry` has live golden-corpus coverage.**
  `tests/fixtures/ecosystems/terraform_comprehensive/terraform-aws-modules/vpc/aws/main.tf`
  is a real directory sitting at the EXACT path `modules.tf`'s pre-existing
  `module "vpc" { source = "terraform-aws-modules/vpc/aws" }` block resolves
  to as a local relative path -- the ADR's own documented false-positive
  shape, now made real instead of theoretical (that source was previously a
  dead reference; no directory existed there). It declares one genuine
  resource (`aws_security_group.vpc_endpoints`). The matching cassette side
  (`testdata/cassettes/terraformstate/supply-chain-demo.json`) carries the
  CORRECT module-prefixed address
  (`module.vpc.aws_security_group.vpc_endpoints`) as a separate
  `terraform_state_resource` fact under the same pre-existing S3-backed
  scope, so neither side's address matches the other -- the real, live
  spurious `added_in_config`/`added_in_state` pair the ADR describes, not a
  synthetic single-sided fixture. The new
  `POST /api/v0/terraform/config-state-drift/findings?variant=derived`
  entry in `testdata/golden/e2e-20repo-snapshot.json` asserts BOTH that
  `outcome="derived"` materializes for the wrongly-addressed finding AND
  (via `required_json_object_matches`, not two independent wildcard value
  checks) that the SAME finding's evidence array carries a
  `terraform_module_resolution_confidence` atom with
  `value="external_registry"` on one correlated object -- proving the
  specific cause reaches the read surface, which is the entire
  justification for keeping one `derived` outcome value instead of
  splitting per cause (see `tfconfigstate/doc.go`'s "Outcome model"
  section). This mirrors issue #5594's precedent in this same writer: a
  unit-tested behavior change to reducer-materialized, OpenAPI- and
  MCP-contracted truth gets cassette/golden replay proof, not only fakes.
- **`depth_exceeded` deliberately stays unit/integration-only.** Reaching
  it requires an 11-level-deep local module chain
  (`maxModulePrefixDepth` = 10) -- a fixture heavy enough for a rare
  production shape (a real repo would need ten nested nameless wrapper
  modules purely to trigger this path) that
  `TestBuildModulePrefixMapRecordsDepthExceededAsLowConfidence` and
  `TestLoadDriftEvidenceMarksLowConfidenceForDepthExceededModuleChain`
  (`go/internal/storage/postgres/tfstate_drift_evidence_module_confidence_test.go`)
  already prove precisely, including the depth-comparison fix itself (the
  masking case where the resource is silently misattributed to the
  ancestor's real-but-wrong prefix rather than falling back to root). This
  is a scoping decision, not an oversight: if a future real-world report
  shows `depth_exceeded` firing in practice, a golden fixture is the
  concrete follow-up, mirroring the `external_registry` fixture added here.
