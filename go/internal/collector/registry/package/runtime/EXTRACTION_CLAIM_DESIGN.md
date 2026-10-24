# Claim / Scope / Generation design note (extraction PREP, #8862)

How the package-registry producer turns a workflow claim into a committed
fact generation today, and what the extracted destination must preserve.

## Claim intake (`runtime/source.go:NextClaimed`)

1. `workflow.WorkItem` arrives from `collector.ClaimedService`. Required:
   matching `CollectorInstanceID`, empty-or-`package_registry`
   `CollectorKind`, non-blank `GenerationID`.
2. `item.ScopeID` selects one explicit `TargetConfig` from the source map
   (duplicate scope IDs rejected at `NewClaimedSource`). Unknown scopes fall
   back to derived targets (`derived_targets.go`); otherwise the claim fails.
3. `prefetchWarningGeneration` short-circuits before any fetch:
   private-visibility target without credentials yields
   `credentials_missing`; ecosystems with no native metadata source and no
   explicit URL/format yield `unsupported_metadata_source`.
4. `fetchMetadata` runs the bounded HTTP fetch (20MiB cap, ecosystem Accept
   header, resolved bearer/basic credentials). `metadata_too_large` and
   derived-target `registry_not_found` complete as warning generations;
   every other fetch error fails the claim for retry.
5. `collectDocument` routes through the format adapter (native or
   Artifactory wrapper) into `MetadataParserRegistry.Parse`, then
   `boundedParsedMetadata` enforces `package_limit` (hard error) and
   `version_limit` (keep-first-N plus a `version_limit_truncated` warning),
   and each observation family becomes envelopes via `New*Envelope`.

## Scope and generation (`FactsFromSlice`)

- Scope: `scope.IngestionScope{ScopeID: target.Base.ScopeID,
  SourceSystem: package_registry, ScopeKind: package_registry,
  PartitionKey: provider:ecosystem}` plus provider/ecosystem metadata.
- Generation: `scope.ScopeGeneration{GenerationID: item.GenerationID,
  Status: pending, TriggerKind: snapshot}`, observed/ingested at collect time.
- Envelopes carry `ScopeID`, `GenerationID`, `CollectorKind`,
  `FencingToken: item.CurrentFencingToken`, `SourceConfidence: reported`,
  `FactID` derived from (kind, stable key, scope, generation) and
  `StableFactKey` derived from identity only — stable across generations.
- Commit path is `collector.ClaimedService`: control store for claim/lease,
  ingestion committer for facts. Oversized metadata completes the claim
  without `FailClaimRetryable` (terminal, warning evidence, no retry burn).

## Fencing, lease, and retry posture

`ClaimLeaseTTL` with a shorter heartbeat, `MaxAttempts` from
`workflow.DefaultClaimMaxAttempts`, poll interval from env. Rate limits are
telemetry-visible (`ErrRateLimited`, per-ecosystem counter) without feed URLs
in labels. Cancellation, revoked authority, stale claims, and duplicate
delivery (AC5) are exercised only in-tree so far; the extracted producer must
re-prove them against the real control plane (NOT_CHECKED).

## Extraction boundary

The destination owns everything in the census move set plus this flow. Core
retains `workflow`, `scope`, `collector`, `facts/chain`, `packageidentity`,
and storage. The grant the destination needs is explicit: claim intake types,
committer/control-store interfaces, scope/generation constructors, fact
envelope and schema-version contracts, and the factschema payload encoders —
versioned, never vendored by copy.
