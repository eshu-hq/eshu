# AGENTS — internal/reducer

This file guides LLM assistants working in `go/internal/reducer`. Read it
before touching any file in this directory.

## Read first

- This package's tests parse shell files under
  `scripts/lib/ifa_family_registry/`, and its Ifá CI gate runs the whole
  package with no `-run` filter. Both are recorded under "Gotchas /
  invariants" in `README.md`; read them before renaming that shell directory or
  adding a slow test here.

1. Root `AGENTS.md` "Golden Rules", `docs/internal/agent-guide.md` "Bootstrap
   And Correlation Truth", `eshu-correlation-truth`, `concurrency-deadlock-rigor`.
2. `docs/public/architecture.md` — service boundaries and data flow.
3. `docs/public/deployment/service-runtimes.md` — Resolution Engine section.
4. `docs/public/reference/telemetry/index.md` — observability contract.
5. `go/internal/projector/README.md` — projector→reducer handoff and phase
   publication model.
6. `go/cmd/reducer/README.md` — runtime wiring context.

## Invariants (cite file:line)

- **Every domain must be cross-source, cross-scope, and truth-emitting** —
  `registry.go:53` `OwnershipShape.Validate`; registration fails unless the
  domain declares either durable canonical writes or bounded counter emission.
- **Intent lifecycle is fixed: pending → claimed → running → succeeded/failed** —
  `intent.go:65–74`; do not invent additional states.
- **Generation supersession short-circuits execution** — `runtime.go` checks `GenerationCheck` before dispatching to `Handler.Handle`; return `ResultStatusSuperseded` rather than projecting stale truth.
- **One rebind exception (#10106)** — the scope-keyed Crossplane SATISFIED_BY domain rebinds to the active generation in `generationCurrent` when `ActiveGeneration` is wired; never widen it to a domain whose intent or handler reads generation-scoped facts.
- **Heartbeat stops before Ack** — `service.go:337` calls `stopHeartbeat()`
  before `WorkSink.Ack`; do not reorder this or you risk lease extension
  after the transaction has committed.
- **`deployment_mapping` blocks on `resolved_relationships`** —
  `DomainDeploymentMapping` handler consumes relationships that do not exist
  until Phase 3 of the bootstrap pipeline. Any domain added as a consumer of
  `resolved_relationships` must have a post-Phase-3 reopen mechanism. See
  `bootstrap-index/main.go:273`. Its consumers' own gate (`crossrepo.EvaluateOwnGate`, #7597) fails a dead-lettered producer in the counting `*_producer_dead_lettered` classes and a missing one in `*_producer_missing` (never enroll them as non-counting); the producer's relationship-generation activation reopens those consumers in the same transaction.
- **Phase publications and graph writes are not atomic** — if a write
  commits but the publication fails, `gpphase.PhaseRepairQueue`
  captures the retry. Do not skip enqueueing to the repair queue when a
  publish fails.
- **Shared projection intent IDs are stable SHA256 hashes** —
  `sharedintent.Build` (`sharedintent/intent.go`, root spelling `BuildSharedProjectionIntent`); changing the default identity fields breaks
  in-flight idempotency. `IdentityKey` is a narrow override for domains that
  must store several rows under one durable `PartitionKey` without collapsing
  their `intent_id`s; audit every caller before using it.
- **Edge domains gate on readiness phases** — `worker.ReadinessPhase` (`intents/shared/worker/domains.go`);
  `code_calls`, `inheritance_edges`, `sql_relationships`, and `rationale_edges`
  gate on `canonical_nodes_committed` because their targets are canonical or
  created inline. `semantic_nodes_committed` can stall them forever (#2867-#2869).
- **Promoted edge domains keep their handler's evidence source** — a domain moved
  onto the shared-projection runner must keep its original `evidence_source`, not
  the runner's global source: inheritance, SQL, and rationale keep
  `reducer/inheritance`, `reducer/sql-relationship`, and `reducer/rationale`.
- **SQL trigger functions materialize as `EXECUTES` edges** —
  `ExtractSQLRelationshipRows` reads `function_name` from `SqlTrigger`
  metadata and writes trigger-to-`SqlFunction` `EXECUTES` rows
  (`sqlrelationship/sql_relationship_materialization.go:347`). Code dead-code
  uses those rows as incoming reachability for stored routines.
- **SQL migration files materialize as `MIGRATES` edges** — the `SqlMigration`
  case in `ExtractSQLRelationshipRows` reads `migration_targets` (`{kind,name,
  operation}`) from `SqlMigration` metadata and resolves each via
  `resolveSQLMigrationTarget`, which constrains by **both kind and name** and
  prefers a same-file match. **Ambiguity trap** (#5346): a repo that keeps both
  `schema.sql` and a migrations dir has two same-kind same-name objects (e.g.
  two `SqlTable "users"`); a target that resolves to more than one candidate
  across different files is **skipped and tallied** (`AmbiguousMigrationTargets`),
  never guessed — same never-fabricate discipline as READS_FROM. `select`-only
  mentions are excluded from migration targets (a backfill's read is not a
  migrate). `DROP TABLE` targets are parsed as `operation: "drop"` metadata;
  `MIGRATES` remains one adjacency/provenance edge per migration-target pair and
  never implies the target is absent from head state.
- **All canonical graph writes go through `internal/storage/cypher`** — no
  handler may call a Neo4j or NornicDB driver directly.
- **`JavaScript` dynamic-call alias parsing is indexed once per function** —
  `BuildEntityIndex` caches static alias metadata
  (`code/call/shared/javascript_aliases.go`) and
  `javascript.ResolveDynamicCallee` reuses that cache
  (`code/call/javascript/dynamic.go`). Do not move that
  work back into the per-call loop; generated JS bundles make that
  multiplicative. Cache negative scans too; a source with no static aliases
  must not be sent through the regex pass once per call.
- **Receiver-constrained call-resolution precision corpus** —
  `code_call_resolution_goldens_*_test.go` (issue #3156) locks in per-language
  cross-file resolution truth (`resolution_method` + derived confidence) and
  guards against false positives (ambiguous/dynamic/missing-dependency calls
  must not silently bind to a wrong target). When you improve a resolver,
  flip the matching documented gap (`idealMethod`/`gapIssue` or
  `falsePositiveGap`, tracked by #3198) into a strict assertion in the same
  change — the corpus FAILS if a tracked gap is fixed but its marker is left
  behind, so markers cannot go stale.
- **Security-alert reconciliation identity is provider-alert stable** —
  `securityalert/reconciliation_writer.go` must key reducer facts by provider,
  provider alert id or number, provider evidence scope, package id, and advisory
  ids. Do not include mutable canonical `repository_id` or source fact id in
  the replacement identity, or provider-only placeholders can remain active
  beside later matched or stale rows for the same provider alert.

## Common changes

### Add a new reducer domain

1. Add a `Domain` constant to `domain.go` and `knownDomains` map.
2. Write the handler struct satisfying the `Handler` interface.
3. Add the handler to `implementedDefaultDomainDefinitions` in `defaults.go`.
4. Add a `DomainDefinition` (with `OwnershipShape` and `TruthContract`) to
   `DefaultDomainDefinitions` in `registry.go` only when the domain is
   unconditionally wired. Adapter-gated domains such as
   `DomainAWSCloudRuntimeDrift` use an additive helper so the runtime cannot
   register a domain that has no durable publication path. Add the gated
   registration to the matching themed sibling helper in
   `defaults_additive_domains_*.go` (correlation, supply_chain, secrets_drift,
   cloud_nodes, cloud_relationships, cloud_posture, or incident_code) — not the
   `appendAdditiveDomainDefinitions` orchestrator, which only chains the helpers.
   Registration is keyed by `Domain` in `Registry.Register`, so the append order
   across helpers is not runtime-observable.
5. Wire the backend adapters in `cmd/reducer/main.go` `DefaultHandlers`.
6. If the domain consumes `resolved_relationships`, add a post-Phase-3
   reopen in `bootstrap-index/main.go` after ReopenDeploymentMappingWorkItems.
7. Add telemetry: at minimum the service-level `telemetry.SpanReducerRun` span
   and `eshu_dp_reducer_executions_total` counter, plus a domain counter when
   the domain is counter-emission truth such as package source correlation or
   AWS runtime drift.
8. Write a failing test first; confirm it fails for the right reason.

### Change reducer queue claim semantics

- Any change to `WorkSource.Claim`, `BatchWorkSource.ClaimBatch`, or
  `WorkSink.Ack`/`Fail` is a concurrency change. Follow the
  `concurrency-deadlock-rigor` skill fully before writing code.
- Prove idempotency: a duplicate claim or partial failure must converge on
  the same graph truth, not produce duplicate or absent rows.

### Add a new graph projection phase or keyspace

1. Add the constant to `gpphase/keyspace.go` (keyspace) or `gpphase/phasekey.go` (phase) — see `gpphase/README.md`. Callers spell the single `gpphase` name; do not add root aliases (retired in #6626 batch 6 bucket 9).
2. Verify the new constant does not conflict with existing keyspace usage in
   `worker.ReadinessPhase` and `worker.ReadinessKeyspace` (`intents/shared/worker/domains.go`).
3. Update `internal/storage/postgres` schema DDL if a new readiness row
   shape is needed.
4. Update `worker.ReadinessPhase` in `intents/shared/worker/domains.go` if the
   new phase gates a shared-projection domain.

### Change shared projection runner config

- Env var parsing lives in `worker.LoadConfig`
  (`intents/shared/worker/config.go`; the root keeps the `LoadSharedProjectionConfig` spelling); constants live in `cmd/reducer/config.go`.
- Update both the runner config and the README config table in the same PR.

## Failure modes

- **Stuck `deployment_mapping`**: queue shows `deployment_mapping` items in
  `pending` or `failed` state long after bootstrap. Check whether
  ReopenDeploymentMappingWorkItems ran in the bootstrap pipeline;
  cross-reference `graph_projection_phase_state` for
  `backward_evidence_committed` rows.
- **Missing phase publication causing edge domain blocking**: shared
  projection logs "skipped intents until their prerequisite graph phase is committed"
  at high frequency. Check `graph_projection_phase_state` for
  `semantic_nodes_committed` or `canonical_nodes_committed` rows for the
  affected `AcceptanceUnitID`.
- **Repair queue growth**: `graph_projection_phase_repair` table grows
  without drain. Check `repair.Repairer` logs for
  `graph_projection_repair_publish_failed`; verify the phase publisher's
  Postgres connection is healthy.
- **Generation supersession flood**: `reducer_executions_total{status="superseded"}`
  rises. Investigate whether the ingester is emitting new generations faster
  than the reducer can drain old ones.
- **Heartbeat lease failure**: `lease_heartbeat_failure` in logs means the
  lease expired mid-execution; the intent will be re-claimed. Root cause is
  usually slow graph writes or Postgres saturation.
- **Slow `code_call_materialization` extraction**: if the completion log shows
  high `extract_duration_seconds` with low fact count, inspect large
  JavaScript `function_calls` arrays and run
  BenchmarkExtractCodeCallRowsLargeJavaScriptDynamicCalls (package `code/call`) before changing
  graph or queue code.

## Evidence notes

### Namespace environment-alias binding (#5434)

First benchmark pass (`BenchmarkExtractKubernetesWorkloadNodeRows`,
5,000-pod-template corpus including a populated `selector` label map, the
realistic Deployment shape) measured a regression exceeding the
diagnostic-rigor ~10% band: BEFORE (raw map, commit 7df868370) 7.76ms/9.34MB/
150,025 allocs -> naive typed decode 12.34ms/12.74MB/215,026 allocs (+59%
time, +36% allocs). Root-caused to `sdk/go/factschema/decode_map.go`'s
`assignField`, whose `map[string]string`/`*map[string]string` fields (the
pod-template `Selector`/`Labels`, and AWS's pre-existing `Tags
*map[string]string`) fell through to the bounded `jsonRoundTripValue`
marshal/unmarshal fallback on every fact — the same fallback path the AWS
migration's own benchmark never exercised (its benchmark payload never
populates `tags`). Proven with a throwaway microbenchmark isolating
`jsonRoundTripValue`'s marshal/unmarshal round trip against a direct
type-assert/coerce path on the same map shape: 15.3x speedup
(1.42µs/op -> 92ns/op), with an explicit equivalence check (0 symmetric diff)
proving the fast path returns identical output to the fallback for every
value it accepts. Applied the theory as a genuine fix rather than a
kubernetes_live-local workaround: `decode_map.go` gained a `map[string]string`
fast path (`anyToStringMap`, reached from both the `Ptr`-to-map and plain
`Map` cases in `assignField`) that shares the exact type-assert/coerce shape
the existing `map[string]any` fast path already used one branch above it, so
every family decoding through `decode_map.go` (AWS, IAM, incident, GCP,
Azure, kubernetes_live) benefits, not only this wave's kind. Re-measured after
the fix: 8.27ms/9.54MB/150,019 allocs (+6.6% time, +2.1% memory, -6 allocs
versus the pre-migration baseline) — within the diagnostic-rigor band.
Re-ran the AWS/GCP/Azure/incident family benchmarks
(`BenchmarkExtractCloudResourceNodeRows`,
`BenchmarkExtractAWSRelationshipEdgeRows`,
`BenchmarkBuildCloudResourceJoinIndex`,
`BenchmarkExtractGCPCloudResourceNodeRows`,
`BenchmarkExtractGCPRelationshipEdgeRows`,
`BenchmarkExtractAzureCloudResourceNodeRows`,
`BenchmarkExtractAzureRelationshipEdgeRows`,
`BenchmarkBuildIncidentRoutingEvidenceInputs`) after the shared `decode_map.go`
change: all stayed within their previously documented bands (for example
`ExtractCloudResourceNodeRows` 15.3-15.6ms before this wave -> 16.6ms after,
`ExtractGCPCloudResourceNodeRows` 16.6ms -> 16.75ms), confirming the fast path
is additive (new accept paths only; every other shape still falls through to
the identical `jsonRoundTripValue` fallback) and introduces no regression for
the families that do not exercise `map[string]string` fields on their hot
path. Measured `go test ./internal/reducer -run '^$' -bench
'BenchmarkExtractKubernetesWorkloadNodeRows' -benchmem -count=3` (darwin/
arm64, Apple M1 Max; backend: in-memory extractor; input: 5,000 synthetic
`kubernetes_live.pod_template` facts each carrying a one-entry `selector`
label map). Result class: Correctness win with a bounded, measured handler
cost (well within the diagnostic-rigor band after the shared decode-path
fix), plus a reusable performance improvement for every already-migrated
family.
`TestAnyToStringMap_CoercesJSONBNativeShape` and
`TestDecodeMapInto_MapStringStringFastPath` (new,
`sdk/go/factschema/decode_map_test.go`) lock the fast path's accept/
reject/equivalence contract: a string-valued `map[string]any` and an
already-typed `map[string]string` both decode correctly (including the
empty-but-present map case staying non-nil), while a map carrying a
non-string value or a non-map input falls back to `jsonRoundTripValue` rather
than silently coercing or dropping data.

First benchmark pass (`BenchmarkBuildSupplyChainImpactIndexWithQuarantine`,
2,000-advisory corpus each emitting one `vulnerability.cve` +
`vulnerability.affected_package` + `vulnerability.os_package` fact) measured a
regression exceeding the diagnostic-rigor ~10% band: BEFORE (raw map, commit
321e4aada1be3d418ab0f0a4d2628e6e68df02c9) 4.98ms/3.53MB/70,095 allocs -> naive
typed decode 9.7-15.4ms/4.82MB/78,097 allocs (roughly +100-200% time, +11.5%
allocs). Root-caused to `sdk/go/factschema/decode_map.go`'s `assignField`
having NO fast path for `float64`/`*float64` fields — every prior migrated
family (AWS, IAM, incident, GCP, Azure, kubernetes_live) has zero float fields,
so this gap was invisible until `vulnerability.cve`'s `CVSSScore *float64`
became the first float field decoded through this seam. Every `*float64` value
fell through `assignField`'s `Ptr` case to the `default: jsonRoundTripValue`
branch (a `json.Marshal`+`json.Unmarshal` round trip per field), the exact
shape of gap the kubernetes_live wave found and fixed for `map[string]string`.
Proven with a throwaway microbenchmark isolating a single-value JSON round trip
against a direct `raw.(float64)` type assertion on the same input: ~250x
speedup (500ns/op, 3 allocs -> ~2ns/op, 0 allocs). Applied the theory as a
genuine fix rather than a vulnerability-local workaround: `decode_map.go`
gained a `float64`/`*float64` fast path (`jsonNumberToFloat64`, reached from
both the plain `Float64` case and the new `Ptr`-to-`Float64` case in
`assignField`), mirroring the existing `Int32`/`Int64` pointer cases, so every
family decoding a float field in the future benefits, not only this wave's
`vulnerability.cve`. Re-measured after the fix (darwin/arm64, Apple M1 Max,
`-count=5`, alternating with the baseline to control for machine load):
BEFORE 4.76-5.27ms/3.53MB/70,095 allocs (median ~4.98ms) -> AFTER
5.6-6.7ms/4.50MB/72,093 allocs (median ~6.3ms) — roughly +27% time, +2.8%
allocs. `go test ./internal/reducer -run '^$' -bench
'BenchmarkBuildSupplyChainImpactIndexWithQuarantine' -benchmem -count=5` (this
branch) versus the same command against commit
321e4aada1be3d418ab0f0a4d2628e6e68df02c9's `BenchmarkBuildSupplyChainImpactIndex`
(added identically on that commit for the comparison). The residual +27% is
NOT a new decode_map.go gap: a follow-up CPU profile (`-cpuprofile`) after the
fix shows no `jsonRoundTripValue` frame in the top 25 nodes; the remaining cost
is `assignField`/`decodeAndValidate` reflection dispatch scaling with
`vulnerability.affected_package`'s ten struct fields (nine named plus
Attributes-free — this kind has no untyped pass-through) versus the old raw
string-keyed map lookups, incurred TWICE per Go-ecosystem advisory fact: once
in the main index loop and once in `extractGoAffectedPackages`
(`go_reachability_extract.go`), a double-decode structure that
predates this migration byte-for-byte (the pre-migration `extractGoAffectedPackages`
already independently re-read the same envelopes' `payloadStr` fields a second
time for Go-specific filtering). The typed path buys the accuracy guarantee (a
`vulnerability.cve`/`.affected_package`/`.os_package` fact missing a required
identity field — `advisory_id` for the first two;
`distro`/`distro_version`/`package_manager`/`name`/`arch`/`installed_version_raw`
for `os_package` — dead-letters `input_invalid` via
`partitionDecodeFailures`/`recordQuarantinedFacts` instead of silently
producing a blank-identity index row) at a bounded, root-caused, measured cost;
the pre-existing double-decode of `affected_package` for Go-ecosystem
filtering is flagged as a follow-up consolidation opportunity, not blocking
this migration. Result class: Correctness win with a bounded, measured handler
cost (the real perf gap — the float64 fallback — found and fixed; the residual
is architectural reflection overhead, not an unmeasured regression).
`TestBuildSupplyChainImpactFindingsQuarantinesOSPackageMissingInstalledVersion`
(new flagship regression test, `build_input_invalid_test.go`) failed
before `buildSupplyChainImpactIndexWithQuarantine` routed a
missing-`installed_version_raw` `vulnerability.os_package` fact through the
decode seam (the old `payloadStr` lookup returned `""` for the absent key and
silently produced a row with no operator signal), then passed after: the
malformed fact is recorded on one `input_invalid` quarantine naming the field,
and a valid sibling CVE/affected_package/os_package trio in the same batch
still produces its finding.
`TestBuildSupplyChainImpactFindingsOSPackagePresentButEmptyVendorAdvisorySourceDecodes`
proves the absent-vs-empty distinction holds for `vulnerability.os_package`
specifically: `RepositoryClass`/`VendorAdvisorySource` are optional on the
typed struct, so a present-but-empty `vendor_advisory_source` (the collector's
own "ambiguous/unknown vendor origin" fail-closed observation) is a VALID
decode, not a quarantine — the pre-existing `osPackageMatchesAffectedPackage`
matcher still simply does not match it, byte-identical to pre-typing
behavior. `TestDecodeMapInto_Float64FastPath` (new,
`sdk/go/factschema/decode_map_test.go`) locks the float64 fast path's
accept/reject contract: a plain `float64` field, a present `*float64` field,
and an absent `*float64` field (stays nil) all decode correctly, while a
non-numeric value fails closed with an error rather than silently zeroing the
field.

`osPackageMatchesAffectedPackage` (match.go) is UNCHANGED
by this migration: it still decides `vulnerability.os_package` impact purely
by `RepositoryClass=="vendor"` plus a `VendorAdvisorySource` string match
against the affected package's classified vendor source
(`classifyAffectedPackageAdvisorySource`), never by comparing
`InstalledVersion` (installed_version_raw, preserved verbatim through the
typed struct) against any upstream/fixed version — the exact vendor-backport
accuracy contract the migration's caveat exists to protect. No version
comparison was added or removed anywhere in this diff.

Digest-anchor the OS-package scan tier (#5463): `addSupplyChainImpactIndexEntry`
now decodes the sibling `scanner_worker.analysis` fact
(`factschema_decode_scannerworker.go`) and indexes it on
`supplyChainImpactIndex.scannerAnalyses`, keyed by ScopeID+GenerationID
(`supplychainmodel.ScopeGenerationKey`, which defers to `facts.Envelope.
ScopeGenerationKey()` so the join key matches the platform's durable
scope-generation boundary). The sibling analysis must first be LOADED, and a
scanner_worker.analysis fact lives in the os_package's own scan scope, not the
intent's vulnerability-intelligence scope — so `supplyChainImpactFactKinds`
(the intent-scope base load) cannot reach it. A new `loadSupplyChainImpactEvidence`
stage (`loadSupplyChainImpactScannerAnalysisScopeFacts`) runs right after the
active-evidence stage and queries each LOADED os_package envelope's own
ScopeID+GenerationID for its sibling analysis (every pair loads since #7154;
the per-intent evidence budget bounds the total, see
(#7154)).

No-regression (#5463): the sibling-load stage adds sequential scoped
`loadFactsForKinds` reads per intent (bounded by the #7154 evidence budget, not
a pair cap) — one per DISTINCT os_package scan scope
already present in the envelope set, deduped by
`supplychainmodel.ScopeGenerationKey`, and zero when no os_package fact was loaded
(the common case for a non-vendor intent). It follows the same bounded,
data-gated shape as the existing active-evidence / repository / manifest-
dependency stages, and its per-intent cost is guarded by the CI reducer
per-handler latency gate (`reducer per-handler latency gate`) and the reducer
contention gate re-run against the now-live os_package path on push, not only
the inert head. The stage is instrumented (`load_scanner_analysis_scope`
sub-duration, `scanner_analysis_scope_facts` sub-signal) so a per-intent
regression is observable at runtime; batching the sibling reads into one
scoped multi-scope query is a tracked follow-up if a vendor-heavy scope ever
approaches the load ceiling.

End-to-end liveness (#5463/#5705): `supplyChainImpactFactKinds()` intentionally
OMITS `vulnerability.os_package` and `scanner_worker.analysis` — both live
cross-scope, not in the intent's vulnerability-intelligence scope, so a base-kind
entry would query the wrong scope and return nothing. Instead
`loadSupplyChainImpactEvidence` loads them through two dedicated stages:
`loadSupplyChainImpactOSPackageAdvisoryFacts` pulls `vulnerability.os_package`
cross-scope by vendor-advisory ecosystem through the same advisory-target reader
(`ListOSPackageAdvisoryFactEnvelopes` → `ListOSPackageAdvisoryTargets`) the
vulnerability-intelligence coordinator uses, and
`loadSupplyChainImpactScannerAnalysisScopeFacts` then loads each LOADED
os_package's sibling analysis from that package's own scan scope. So os_package
envelopes DO reach this stage in the real Handle pipeline, the digest join below
fires end-to-end, and `list_supply_chain_impact_findings` returns the scanned CVE
with a real `subject_digest` — the B-7 golden floor asserts exactly that
(`list_supply_chain_impact_findings` minimum_results:1 for CVE-2026-00010 with
`subject_digest` required and its value pinned; it goes red if the os_package
load, the scanner-scope join, or the digest stamp regresses). Repository binding
of the same finding (RepositoryID anchor) is #5464's work, not this PR. The
scope-aware `TestSupplyChainImpactHandlerLoadsScannerAnalysisFromOSPackageScanScope`
guards the sibling-load STAGE (it fetches the analysis from an os_package's own
scan scope given os_package facts in the envelope set); the golden floor guards
the full production Handle path end-to-end. The stage's timing/count surface
through the existing
`sub_duration_*`/`sub_signal_*` structured-log fields
(`load_scanner_analysis_scope`, `scanner_analysis_scope_facts`), adding no new
metric instrument or label.
`classifySupplyChainImpactPackage` joins an
`os_package` finding to its sibling analysis by that key and sets
`SubjectDigest` from the analysis's real content-addressed `image_digest`
(and `ImageRef` from its `image_reference`) instead of the opaque scope_id.
The former `finding.SubjectDigest = firstNonBlank(osPackage.scopeID, ...)`
fallback is DELETED: a scope_id is a scan-target locator, never a sha256, so it
could never match a deployment digest. When no sibling analysis resolves (or
its digest is blank), `SubjectDigest` stays whatever it already was (empty, or
an SBOM-derived digest) rather than a fake. Deleting the fallback surfaced a
latent accuracy bug — `supplyChainImpactFindingHasOwnedAnchor` silently relied
on the fake digest always being non-blank, so with it gone every vendor-matched
OS-package finding would have been dropped; a matched `os_package`
(repositoryClass=="vendor" + VendorAdvisorySource match, both required by
`firstOSPackageImpactPath` before this point) is now its own owned anchor, so
real findings are retained. Finding-count parity holds on the invariant that a
scanner-emitted `os_package` fact always carries a non-blank envelope ScopeID
(the field the deleted fallback keyed on): the old anchor was
`SubjectDigest != "" (== scopeID) || RepositoryID != ""`, so the new
EvidencePath-based anchor is equivalent for every fact with a non-blank ScopeID,
and a ScopeID-less os_package fact — practically unreachable, since ScopeID is a
mandatory platform envelope field — is the one case where the new anchor retains
a vendor-matched finding the old one would have dropped (the more correct
outcome, not a spurious finding). Proof is the reducer
fixture `TestBuildSupplyChainImpactFindingsAnchorsOSPackageSubjectDigestOn
ScannerAnalysis` (scanned digest matches a CI-declared deployment digest) plus
the regression `...LeavesOSPackageSubjectDigestBlankWithoutScannerAnalysis`.

`buildSecretsIAMIndex` now returns a `[]quarantinedFact` alongside the index (as
it already did for `aws_iam_principal`): a `vault_auth_role` fact missing its
required `role_join_key`, or a `k8s_service_account`/
`k8s_workload_identity_use`/`eks_irsa_annotation`/
`eks_pod_identity_association` fact missing its required
`service_account_join_key` (or `role_arn` for the two EKS kinds), is quarantined
per-fact via `partitionDecodeFailures` rather than silently vanishing from
`index.vaultRoles`/`index.serviceAccounts`/`index.workloads`/`index.irsa`
under `addByKey`'s pre-typing blank-key guard -- while every valid sibling
fact's exact chain still resolves. `TestBuildSecretsIAMTrustChainReadModelsQuarantinesVaultAuthRoleMissingRoleJoinKey`
and `TestBuildSecretsIAMTrustChainReadModelsQuarantinesK8sServiceAccountMissingJoinKey`
(new, `secrets_iam_input_invalid_test.go`, mirroring
`TestGCPResourceMaterializationQuarantinesMissingFullResourceName`) failed
before the conversion (quarantine count 0; the old `payloadString`/
`payloadStrings` lookups returned `""`/`nil` for the absent key and
`addByKey`'s blank-key guard silently dropped the fact from the index with no
operator signal, not even a posture gap), then passed after: the malformed
fact is recorded on `input_invalid_facts` and the valid sibling workload-to-
vault-secret chain in the same batch still resolves to `SecretsIAMTrustChainStateExact`
with its full secret access path.

The VAULT/K8S facts are decoded exactly ONCE at index-build time and the
decoded struct is stored alongside the source envelope in a small per-kind
pair type (`secretsIAMServiceAccount`, `secretsIAMWorkload`, `secretsIAMIRSA`,
`secretsIAMVaultRole`, `secretsIAMVaultPolicy`, `secretsIAMVaultKV`,
`secretsIAMGCPBinding`), mirroring the existing `secretsIAMPrincipal` pattern
#4568 established for `aws_iam_principal` -- every downstream trust-chain
function (`secretsIAMExactChains`, `secretsIAMChain`, `secretsIAMVaultPaths`,
`exactIAMRoleTrust`, `secretsIAMWildcardVaultAuthRoleObservations`,
`vaultPolicyRules`) reads the pre-decoded struct fields rather than re-decoding
the envelope, so there is no double-decode of any VAULT/K8S fact.
`VaultACLPolicy.Rules` is a fully typed `[]secretsiamv1.VaultACLPolicyRule`
(not a `map[string]any` pass-through), matching the collector emitter's
closed `{path_fingerprint, path_depth, capabilities}` shape per rule.

First benchmark pass (`BenchmarkBuildSecretsIAMTrustChainReadModels`, new,
2,000-service-account corpus, each producing one full exact
workload-to-vault-secret chain with a 2-rule `vault_acl_policy`) surfaced a CPU
profile with a small (~2-4% cumulative) but real `jsonRoundTripValue` fallback
hit, isolated via `go tool pprof -peek` to `assignField`'s `Ptr` case for a
`*int` field -- `VaultACLPolicyRule.PathDepth` and `VaultKVMetadata.PathDepth`
were the first `*int`-shaped payload fields any migrated family decoded through
this seam (every prior family's optional integer fields used `*int32`/
`*int64`/`*float64`, each already fast-pathed by Wave 4a/4c). This is the same
gap class the kubernetes_live wave (`map[string]string`) and the
vulnerability_intelligence wave (`float64`) found and fixed for their own
first-occurrence field shapes. Proven with a throwaway microbenchmark isolating
a single `*int` JSON round trip against a direct `float64`/`int` type-switch
coercion on the same input: ~500x speedup (166ns/op, 2 allocs -> 0.32ns/op, 0
allocs). Applied the theory as a genuine fix: `decode_map.go` gained a
`jsonNumberToInt` helper and an `Int` case in `assignField`'s `Ptr` switch,
mirroring the existing `Int32`/`Int64`/`Float64` pointer cases, so every family
decoding an optional platform-`int` field in the future benefits, not only this
wave's `PathDepth` fields. Re-measured after the fix
(`go test ./internal/reducer -run '^$' -bench
'BenchmarkBuildSecretsIAMTrustChainReadModels' -benchmem -count=7 -benchtime=2s`,
darwin/arm64, Apple M1 Max; backend: in-memory extractor; input: 2,000
synthetic service accounts x {k8s_service_account,
k8s_workload_identity_use, eks_irsa_annotation, aws_iam_principal,
aws_iam_trust_policy, vault_auth_role, vault_acl_policy (2 rules),
vault_kv_metadata} = 16,000 fact envelopes). BEFORE (raw map, commit
b12a335eb) -> AFTER (typed decode + `*int` fast path), both measured
alternately on the same machine to control for ambient load: median 29.5ms /
320,331-320,335 allocs / 32.96MB/op -> median 29.0ms / 314,329-314,332 allocs /
34.19MB/op (~0% time delta within noise, -1.9% allocs, +3.7% memory). The
allocation count went DOWN, not up, and the small memory increase is the typed
struct fields themselves costing more bytes than an untyped `map[string]any`
entry -- both well inside the diagnostic-rigor ~10% band. The repo's existing
GATED perfcontract benchmark for this handler,
`BenchmarkSecretsIAMGCPGrantObservations` (`handler_budget_secrets_iam_gcp_grant_observations`,
ceiling 8,300,000 ns/op, `testdata/benchmarks/reducer-handler-budgets.txt`),
builds its index purely from `gcp_iam_principal`/`gcp_iam_permission_policy`
facts -- the deferred GCP IAM lane this wave leaves untouched -- so it never
exercises the converted VAULT/K8S switch arms; measured before/after on this
branch it stayed flat (median ~8.5ms both before and after, allocs unchanged
at 116,039-116,043), confirming this wave's decode-path change is invisible to
that specific gated benchmark, as expected. Every prior migrated family's own
benchmark (`BenchmarkExtractCloudResourceNodeRows`,
`BenchmarkExtractAWSRelationshipEdgeRows`,
`BenchmarkExtractGCPCloudResourceNodeRows`,
`BenchmarkExtractGCPRelationshipEdgeRows`,
`BenchmarkExtractAzureCloudResourceNodeRows`,
`BenchmarkExtractAzureRelationshipEdgeRows`,
`BenchmarkExtractKubernetesWorkloadNodeRows`,
`BenchmarkBuildIncidentRoutingEvidenceInputs`,
`BenchmarkBuildSupplyChainImpactIndexWithQuarantine`,
`BenchmarkBuildSBOMAttestationAttachmentDecisions`) was re-run after the
shared `decode_map.go` change and stayed within its previously documented
allocation counts, confirming the new `*int` fast path is additive-only (a new
accept path only; every other shape still falls through to the identical
`jsonRoundTripValue` fallback). `TestDecodeMapInto_IntFastPath` (new,
`sdk/go/factschema/decode_map_test.go`) locks the fast path's accept/reject
contract: a plain `int` field, a present `*int` field, and an absent `*int`
field (stays nil) all decode correctly, while a non-integral float and a
non-numeric value both fail closed with an error rather than silently
truncating or zeroing the field. Result class: Correctness win with no
measured handler regression (the real perf gap -- the missing `*int` fast
path -- found and fixed before it could regress this or any future family).

First benchmark pass (`BenchmarkExtractProviderSecurityAlerts`, 5,000-alert
corpus carrying the full Dependabot field set including a `cwes`
`[]map[string]string` list and `epss` `map[string]string` object) measured a
regression far exceeding the diagnostic-rigor ~10% band: BEFORE (raw map)
~14.3ms/25.6MB/210,014 allocs -> naive typed decode ~25ms/32.9MB/325,028 allocs
(roughly +75% time, +55% allocs). Root-caused via CPU + alloc profile to
`sdk/go/factschema/decode_map.go`'s `assignField` having NO fast path for a
`[]map[string]string` field — every prior migrated family's slice fields are
`[]string` or `[]Struct`, so `security_alert.repository_alert`'s `CWEs
[]map[string]string` (shared shape with `gcpv1.IAMPolicyObservation.Members`)
became the first slice-of-map field decoded through this seam and every element
fell through to the `default: jsonRoundTripValue` marshal/unmarshal branch —
the exact gap class the kubernetes_live (`map[string]string`) and vulnerability
(`float64`) waves closed for their own first-occurrence shapes. Applied the fix
as a genuine shared decode-path improvement, not a security-alert-local
workaround: `decode_map.go` gained a `[]map[string]string` fast path
(`anyToStringMapSlice`, reached from the new `reflect.Map` case in the
`reflect.Slice` switch), mirroring the existing `map[string]string` map fast
path, so every family decoding a slice-of-string-map field benefits (gcp
Members included). `TestDecodeMapInto_StringMapSliceFastPath` (new,
`sdk/go/factschema/decode_map_test.go`) locks its accept/reject/equivalence
contract. A second alloc profile then showed the EPSS/CWEs string maps were
double-allocated (decode allocates the map, then the normalizer cloned it), so
the reducer normalizers (`normalizeSecurityAlertStringMap`/
`normalizeSecurityAlertStringMapSlice`) now trim/drop-empty IN PLACE on the
solely-owned decode result (`normalizeSecurityAlertStringMapInPlace`, re-keying
a padded key after the range to stay iteration-safe;
`TestNormalizeSecurityAlertStringMapInPlace` locks the contract). After both
fixes: ~27.0MB/225,017 allocs versus the raw-map baseline's 25.6MB/210,014 —
+5.5% bytes, +7.1% allocs, within the diagnostic-rigor ~10% band (allocations/
bytes are the load-independent proxy; wall-clock ns/op tracked the same ratio
when measured un-contended, ~17ms typed vs ~14ms raw floor). This is a COLD
per-scope-generation reconciliation projection path (once per reconciliation
intent over the loaded alert facts, not a per-edge hot loop), and the residual
buys the accuracy guarantee (a `repository_id`-less alert dead-letters instead
of seeding a blank-repository reconciliation row or empty-identity impact
finding). Result class: Correctness win with a bounded, measured handler cost
(the real perf gap — the `[]map[string]string` fallback — found and fixed; the
double-alloc removed).

Review fixes (Wave 4e, PR #4735): (1) the shared `decode_map.go`
`anyToStringMap`/`anyToStringMapSlice` fast paths now defensively CLONE their
already-typed `map[string]string` / `[]map[string]string` input branches, not
just the JSONB `map[string]any` / `[]any` branches, so a decode result is always
a fresh owned value. The reducer normalizes epss/cwes in place, so aliasing an
in-memory caller's `env.Payload` would have mutated the original payload; the
JSONB decode path always allocated (that branch is not hit in production), so the
clone is free in prod and keeps the decode side-effect-free for every input shape
(`TestDecodeMapInto_TypedMapInputNotMutated` locks non-mutation of an
already-typed input). (2) `ExtractProviderSecurityAlerts` — the LENIENT
pre-filter/scoping wrapper — no longer drops a `repository_id`-less alert; it
reconstructs it best-effort from the raw payload
(`providerSecurityAlertFromRawPayload`) so the security-alert evidence-scoping
fence (`supplyChainImpactUsesSecurityAlertScope` /
`scopeSupplyChainImpactEvidenceToSecurityAlerts`) still narrows by the alert's
package/ecosystem identity when every alert in a security-alert-triggered
`supply_chain_impact` intent is malformed, exactly as pre-typing. Without this,
all-malformed alerts skipped scoping and unrelated active dependency/vulnerability
facts (loaded earlier from the malformed alert's package/CVE hints) could publish
unscoped impact findings. The DURABLE reconciliation and impact-seeding paths keep
using the strict `ExtractProviderSecurityAlertsWithQuarantine`, so the malformed
fact still dead-letters as `input_invalid`; only the non-durable scoping signal is
preserved (`TestSupplyChainImpactSecurityAlertScopingSurvivesAllMalformedAlerts`).

Measured with `go test ./internal/reducer -run '^$' -bench
'BenchmarkExtractDocumentationEdgeRowsWithQuarantine|BenchmarkBuildDocumentationDeltaScopeWithQuarantine'
-benchmem -count=5` (darwin/arm64, Apple M1 Max; backend: in-memory
extractor; input: 5,000 synthetic `documentation_entity_mention` facts each
carrying one candidate ref, and one repository-delta fact plus 5,000 matching
`documentation_document` facts) against the equivalent pre-typing benchmark
added identically on commit 8749b7c1d
(`BenchmarkExtractDocumentationEdgeRows`/`BenchmarkBuildDocumentationDeltaScope`).
BEFORE (raw map, commit 8749b7c1d) -> AFTER (typed decode):
`ExtractDocumentationEdgeRows` 3.28-4.31ms/3.71MB/75,064 allocs (median
~3.66ms) -> `ExtractDocumentationEdgeRowsWithQuarantine` 5.21-7.98ms/4.83MB/
95,062 allocs (median ~5.34ms), roughly +46% time, +30% allocs;
`buildDocumentationDeltaScope` 3.15-3.51ms/2.98MB/5,261 allocs (median
~3.21ms) -> `buildDocumentationDeltaScopeWithQuarantine` 5.77-6.07ms/5.54MB/
20,261 allocs (median ~5.84ms), roughly +82% time, +285% allocs. Both deltas
exceed the diagnostic-rigor ~10% band by percentage, so this was investigated
as a candidate `decode_map.go` fast-path gap the way kubernetes_live's
`map[string]string` and vulnerability's `float64` gaps were: a CPU profile
(`-cpuprofile`) of `BenchmarkExtractDocumentationEdgeRowsWithQuarantine` shows
NO `jsonRoundTripValue` frame in the top 20 nodes — every field shape this
family's structs use (`*string`, `*bool`, `*ACLSummary` as a pointer-to-struct,
`[]EvidenceRef`/`[]OwnerRef` as a slice-of-struct, `map[string]string`) already
has a `decode_map.go` fast path from a prior wave. The cost is `decodeMapInto`
iterating its full per-type field plan (9 fields for `EntityMention`, versus
the 4-5 keys the pre-typing code read via direct map indexing) plus one nested
`assignStructSlice`/`decodeMapInto` recursion per candidate ref — genuine,
proportional reflection and allocation cost for a richer struct shape, not an
unhandled gap; profiling shows GC/allocation overhead (`runtime.mallocgc`,
`runtime.mapassign_faststr`) as the dominant cost alongside `decodeMapInto`
itself, not a marshal/unmarshal round trip. `DomainDocumentationMaterialization`
is a per-scope-generation materialization domain (registered like
`DomainInheritanceMaterialization` in `registry.go`), not a shared-projection
hot-per-edge-loop domain, so this is a COLD path — the same "measured for a
no-regression bound rather than a tight microbench" classification the
incident family's Wave 4a evidence used for its own once-per-scope-generation
cost (2.42-2.57ms for 500 incidents x 4 fact kinds = 2,000 fact envelopes on
that wave's benchmark corpus, ~= 1.2 microseconds/fact). This wave's
benchmark corpus is 5,000 facts per function (matching the kubernetes_live/
sbom scale), so the fact-normalized cost is directly comparable:
`ExtractDocumentationEdgeRowsWithQuarantine` costs ~5.34ms / 5,000 facts ~=
1.07 microseconds/fact, and `buildDocumentationDeltaScopeWithQuarantine`
costs ~5.84ms / 5,000 facts ~= 1.17 microseconds/fact — both landing within
the same ~1.1-1.2 microseconds/fact band the incident precedent's own
2.42ms/2,000-fact corpus already accepted as a cold-path cost, confirming
the percentage delta (+46%/+82%) is a magnitude artifact of this family's
faster absolute baseline (this benchmark's raw-map baseline ran in
3.2-3.7ms, versus incident's un-migrated baseline being a slower starting
point), not a growing or unbounded per-fact cost.

WASTED-DECODE CHECK (coordinator-directed, before accepting the cold-path
bound): both functions were audited to confirm the typed decode call sits
behind the cheapest available pre-filter, not merely after some filter.
`ExtractDocumentationEdgeRowsWithQuarantine` checks `env.FactKind !=
docs.EntityMentionFactKind || env.IsTombstone` (a struct-field
read, no decode) BEFORE calling `decodeDocumentationEntityMention`; there is
no cheaper pre-filter available before checking `ResolutionStatus ==
"exact"` because `ResolutionStatus` is itself a required struct field that
does not exist until the payload decodes (the pre-typing code paid the
identical cost via `payloadStr(env.Payload, "resolution_status")`, a raw map
lookup of the same key, not a skip). `buildDocumentationDeltaScopeWithQuarantine`
checks `scope.hasDelta` BEFORE entering the `documentation_document` loop at
all (no repository delta means zero document facts are decoded), and within
the loop checks `env.FactKind != docs.DocumentFactKind ||
env.IsTombstone` before calling `decodeDocumentationDocument`; there is no
cheaper pre-filter for `document_id`/`source_metadata.path` because both are
struct fields the pre-typing code also had to read via
`semanticPayloadString`/`sourceMetadataString` for every document fact in
the batch, an identical per-fact read count to the typed path. Neither
function decodes a fact whose result is then discarded by a filter that
could have run first; the ordering is already optimal, so the regression is
irreducible reflection/allocation cost for this family's field count and
nested-slice shape, not a wasted-work bug.

The residual cost buys the accuracy guarantee (a fact missing a required
identity field dead-letters `input_invalid` instead of silently producing an
empty-identity edge or being dropped from delta scope with no operator
signal). Result class: Correctness win with a bounded, measured, root-caused
cold-path cost (no `decode_map.go` gap found, no wasted-decode ordering bug
found; the higher-than-prior-waves percentage reflects this family's richer
per-kind field count and nested-slice shape against a fast absolute
baseline, not a missed fast path or an avoidable extra decode).

SCHEMA-VERSION NORMALIZATION (corpus-gate P0, PR #4753): the git collector
emits `file`/`repository` with NO `SchemaVersion`, but the Postgres persist
layer stamps a version-less fact as the sentinel `"0.0.0"`
(`go/internal/storage/postgres/facts.go`, `facts_streaming.go`, calling
`facts/payload` `payloadstore.EmptyToDefault(SchemaVersion, "0.0.0")`). A fact LOADED for reduction
therefore carries `SchemaVersion="0.0.0"`, not `""`. `decodeLatestMajor`
accepts only `major=="1"` and `major("0.0.0")=="0"`, so before the fix EVERY
real `file`/`repository` fact dead-lettered as an unsupported major and the
whole code graph collapsed (12 golden-corpus rc gates to zero: rc-2/8/10/11/12/
13/15/23). `factschemaEnvelope` now normalizes BOTH version-less spellings —
`""` and the persisted `"0.0.0"` sentinel — to the latest major, so a real
loaded code-graph fact decodes. `"0.0.0"` is never a real emitted schema
version (it is exclusively the persist-layer's empty marker), so this is safe
for every other family, all of which stamp a concrete `"1.0.0"`. The fix does
NOT weaken accuracy: a genuine unsupported major (`"2.0.0"`) still dead-letters,
and a fact missing `repo_id`/`relative_path` still dead-letters `input_invalid`.
`TestDecodeCodegraphAcceptsPersistedVersionlessSchemaVersion` and
`TestExtractCodeCallRowsProducesRowsForPersistedVersionlessFacts` reproduce the
corpus P0 at unit scale (a `file` fact with `SchemaVersion="0.0.0"` must decode
and produce >=1 code-call row);
`TestDecodeCodegraphTreatsAbsentSchemaVersionAsLatestMajor` covers the empty
spelling. The unit tests that used ABSENT (`""`) version passed while the corpus
broke because they never exercised the persisted `"0.0.0"` a real loaded fact
carries — the coverage gap this regression pair closes.

Dead-letter Evidence (epic #4566 §1, the migration's core deliverable): a fact
missing a required identity field — `function_uid` (taint),
`source_function_uid`/`sink_function_uid` (interproc), `function_id` (summary),
`function_id`/`kind` (source) — is routed through `partitionDecodeFailures` to
a visible `quarantinedFact` the handler records via `recordQuarantinedFacts`,
feeding `eshu_dp_reducer_input_invalid_facts_total` (labeled `domain` =
`code_taint_evidence`/`code_interproc_evidence`/`code_function_summary`,
existing label values), the structured "reducer input_invalid fact
quarantined" error log, and `Result.SubSignals["input_invalid_facts"]`. This
REPLACES the initial (rejected-in-review) swallow-to-zero-value behavior; the
production-path proof is `TestCodeTaintEvidenceHandlerQuarantinesMalformedFact`,
`TestCodeInterprocEvidenceHandlerQuarantinesMalformedFact`,
`TestCodeFunctionSummaryHandlerQuarantinesMalformedFact`, and
`TestCodeFunctionSummaryHandlerQuarantinesMalformedSourceFact` — each feeds a
malformed fact through the ACTUAL loader -> handler path and asserts
`SubSignals["input_invalid_facts"] == 1` while a valid sibling still projects.
A P0 was ruled out: `taint.ExtractEvidenceRows`/`taint.ExtractInterprocEvidenceRows`
already `continue` on an empty uid, so a decode-failed fact never wrote an
empty-key graph node even before this dead-letter wiring landed. The interproc
handler reads a NEW `taint.InterprocEvidenceFactLoader` (envelopes) distinct from
the fixpoint projector's `taint.InterprocEvidenceLoader` (typed inputs from an
in-memory solve, no raw decode) so the projector path is untouched. The
graph-id view reads the SAME `code_function_summary` facts the summary-effects
view already quarantines, so its quarantines are discarded to avoid
double-counting one malformed fact.

## #5444 — broadened environment detection beyond path regexes (evidence)

`extractOverlayEnvs` (`candidate_loader.go`) previously unioned only
`ExtractOverlayEnvironments`'s four directory-pattern regexes
(`overlays/<env>/`, `env|environments/<env>/`, `inventory/<env>`,
`group_vars/<env>`). `environment_signals.go` adds two more sources, each
independently alias-gated through `environment.IsKnownToken`/
`environment.Canonical` (`internal/environment`, #5473) so an unrecognized
value never invents an environment:

- `helmValuesFilenameEnvironment` — the Helm `values-<env>.yaml`/
  `values.<env>.yaml` filename convention. Whole-suffix match, not a token
  split, so `values-production-notes.yaml` and `values.schema.yaml` are
  correctly rejected rather than matching `production`/`schema` as a false
  positive.
- `namespaceEnvironment` (via `collectNamespaceEnvironmentsFromFileData`) —
  the destination namespace ArgoCD Applications/ApplicationSets
  (`dest_namespace`, parsed by `internal/parser/yaml/argocd.go`) and
  Kustomize overlays (`namespace`, parsed by
  `internal/parser/yaml/kustomize_semantics.go`) already carried but
  `deploymentEnvironments` never read. Exact-token or `-`/`_`/`.`-delimited
  compound-token match (mirrors `environmentFromArtifactPath`,
  `cross_repo_evidence_artifacts.go`), so `kube-system` and `product-catalog`
  do not falsely resolve to an environment via substring matching.

Deliberately excludes the generic `k8s_resources[].namespace` field: an
arbitrary Deployment/Service namespace is workload-placement evidence
(`WorkloadCandidate.Namespaces`), not deployment-environment evidence, and
folding it in would over-admit environments the alias contract's
no-invention rule forbids.

Both call sites of `extractOverlayEnvs` pick up the broadened detection: the
per-repo candidate-extraction loop in `ExtractWorkloadCandidates`, and the
cross-repo deployment-source enrichment path
(`ExtractOverlayEnvironmentsFromEnvelopes`, consumed by
`enrichDeploymentRepoEnvironments` in
`environment_signals.go`) — so a config-only
deploy-manifests repo referenced as a candidate's `DeploymentRepoID` gets the
same environment evidence a service repo would.

## Anti-patterns

- Do not add `if backend == nornicdb` (or equivalent) logic inside domain
  handlers. Backend differences belong in `storage/cypher` narrow seams.
- Do not skip `gpphase.PhaseRepairQueue.Enqueue` on publish failure.
  Swallowing the error hides missing readiness rows.
- Do not build a new domain that writes edges before confirming the
  appropriate readiness phase is published. Edge writes without a readiness
  gate produce silent partial graph truth.
- Do not use `ResultStatusFailed` for superseded intents. Use
  `ResultStatusSuperseded`; it avoids incrementing the retry counter.
- Do not change the default fields of `SharedProjectionIntentInput` used by
  `stableIntentID` without auditing all in-flight intents in the Postgres
  shared-intent table. Use `IdentityKey` only when the stored `PartitionKey`
  must group multiple durable rows and tests prove those rows keep distinct
  `intent_id`s.

## What NOT to change without an ADR

- The `deployment_mapping` Phase 3 reopen requirement.
- The domain `OwnershipShape` invariants (cross-source, cross-scope,
  durable canonical write or bounded counter emission).
- The heartbeat / lease / retry contract in `service.go`.
- The `BuildSharedProjectionIntent` SHA256 identity function.
- The `gpphase.PhaseRepairQueue` contract (removing it breaks
  the non-atomic write/publish recovery path).
- The ordering of phases in `worker.ReadinessPhase`
  (`intents/shared/worker/domains.go`).

## #4771 — docker-compose runtime signal repair (evidence)

`extractArtifactSignals` (`candidate_loader.go`) now detects the docker-compose
workload signal off `artifact_type == "docker_compose"` (the classification
`templated_detection.go` already emits) instead of the never-produced
`docker_compose_services` key, and drops three other never-produced key reads
(`github_actions_workflow_triggers`, `github_actions_reusable_workflow_refs`,
`jenkins_pipeline_calls`) whose signals already fire via `artifact_type`, the
`Jenkinsfile` path, and groovy `pipeline_calls`.

## #4885 — search-vector build sweep hot-loop fix (evidence)

Two coupled defects made the reducer's search-vector build sweep hot-loop
forever (~every 2.7s), pinning ~2 Postgres cores 24/7 with zero useful output
on a drained full-corpus stack.

Defect B (root cause): the pending lister derived the document id via
`fact.payload->'document'->>'id'`, but `searchdocs.Document` has no JSON tags,
so its `ID` field marshals as the capitalized key `"ID"` and the lowercase
nested read returned NULL. The terminal-metadata `NOT EXISTS` join then never
matched, so every active repository scope was returned as pending on every
sweep regardless of whether its vectors were already built. Fixed by reading
the top-level `payload->>'document_id'` key the writer already emits
(`eshuSearchDocumentPayload`, `eshu_search_document_writer.go:427`).

Defect A (defense-in-depth): `SearchVectorBuildRunner.Run` only backed off when
`PendingScopes==0`; a sweep with pending scopes but no durable output re-looped
immediately. Now a no-progress sweep (`DocumentCount==0 && VectorCount==0 &&
DisabledCount==0`) applies the poll-interval backoff.

Live-corpus confirmation (drained `e2e3586persist` full-corpus stack, 831 active
search-document scopes over 2,595,922 facts): the OLD `payload->'document'->>'id'`
key produced 0 non-NULL document_ids out of 2,595,922 (the nested `id` is always
NULL because `searchdocs.Document.ID` marshals as `"ID"`), so the pending query
selected 831/831 scopes — 100% of active scopes, permanently, which is the
never-draining set that pinned ~2 Postgres cores 24/7. The NEW
`payload->>'document_id'` key produced 2,595,922 non-NULL document_ids and the
pending query selected 556 scopes: the 275 fully-built scopes correctly drop out
and the remaining 556 are genuinely unbuilt work that leaves the set as it
builds, so the sweep reaches `pending=0` and rests on the 30s poll.

Remote runtime proof (`eshu-remote-validation`, live `e2e3586persist` stack):
a musl-built fixed `eshu-reducer` was swapped into the running resolution-engine
and compared against the shipped (buggy) binary. BEFORE: 9 sweeps/30s, each
`document_count=0`/`vector_count=0` — the sweep looped on the already-built
low-`scope_id` batch (`ORDER BY scope_id LIMIT 100`) the broken lister kept
returning, so it never reached the genuine unbuilt backlog and built ZERO
vectors while Postgres sat at 154-280%. AFTER: sweeps build ~30k vectors each
(`document_count`/`vector_count` = 31276, 29370, 31032), no no-progress
backoffs (real work exists), Postgres doing productive write work draining the
backlog toward idle. So the fix is not only a spin fix — it restores
search-vector building for the unbuilt scope backlog that the NULL join key had
silently starved.

## #4893 — TAINT_FLOWS_TO edge + CodeTaintEvidence node retract anchored by uid via ledgers (evidence)

The reducer's value-flow retracts scanned the whole graph on NornicDB. The
TAINT_FLOWS_TO **edge** retract used an unanchored `(:Function)-[rel]->(:Function)
WHERE rel.<prop>` shape that NornicDB plans as `traverseGraphParallel`/`findPaths`
over the whole Function adjacency; the CodeTaintEvidence **node** retract used
`MATCH (n:CodeTaintEvidence) WHERE n.scope_id ... WITH n LIMIT k DETACH DELETE n`,
which the NornicDB delete-with-limit hot path (`collectDeleteWithLimitCandidates`)
serves via `GetNodesByLabel` — decoding the whole node population when no
`scope_id` property index exists. Both are fired per-scope by
`CodeValueFlowStaleCleanupRunner` (~896 scopes) and per-intent by the
materialization/fixpoint paths.

Fix: durable `code_interproc_projected_edge` and `code_taint_evidence_projected_node`
ledgers record the source-Function uid / node uid of every projected artifact,
written BEFORE the graph write so each ledger is a superset of the graph. Retract
enumerates uids from the ledger and anchors the delete by the indexed uid
(`UNWIND $uids MATCH (s:Function {uid})-[rel:TAINT_FLOWS_TO]->() WHERE <pred> DELETE rel`
and `UNWIND $uids MATCH (n:CodeTaintEvidence {uid}) WHERE <pred> DETACH DELETE n`),
then prunes the ledger. An empty ledger enumeration is the existence guard: the
reducer sends no graph delete when a scope has no projected artifacts, so a
zero-taint corpus does zero graph work (this also side-steps the
`collectDeleteWithLimitCandidates` fail-open, which broad-scans on a 0-result
`WITH n LIMIT` delete — the reason a naive `scope_id` property index would not
have fixed the zero-taint case; NornicDB hot-path cookbook §8.5 recommends
indexed key-list deletes). A one-time, count-guarded startup backfill seeds each
ledger from existing graph artifacts so pre-deploy edges/nodes remain retractable.

### Non-deterministic supply-chain image-identity anchor (#5887)

`preferSupplyChainImageIdentity`'s same-tier tie-break (`supply_chain_impact_anchor_tier.go`) compared `factID`, a SHA-256 whose input embeds `generation_id`. A `reducer_container_image_identity` row derived from a live git snapshot gets a fresh `generation_id` every collector run (`GitCollectorSnapshotRun`'s `source_run_id` embeds the run's wall-clock `observed_at`, `git_source_processing.go`), so two same-tier rows disagreeing on repository could pick a different winner run to run with no change in the underlying evidence. #5854 made `container_image_identity`'s fact ID outcome-independent (closing a real stale-duplicate-row bug), and a side effect — `planContainerImageIdentityPublications` collapsing a scope's canonical decision down to fewer `source_repository_ids` entries — could flip a row from tier B/C into tier A, making the draw-dependent tie-break reachable. Restoring `outcome` to the writer's identity map was considered and rejected: it would reopen the #5854 bug to close this one.

`preferSupplyChainImageIdentityConsensus` (`supply_chain_impact_anchor_consensus.go`) replaces the same-tier, different-repository tie-break with corroboration count — how many rows in that tier name each candidate repository, computed once per envelope batch by `buildSupplyChainImageIdentityConsensus`. Tier precedence and every same-repository/unresolved comparison are unchanged, deferring to the existing `preferSupplyChainImageIdentity`.

Review follow-up (PR #5908, codex, P1): the consensus vote count above was counting one vote per stored fact ID, not per logical decision. `WriteContainerImageIdentityDecisions`' own doc comment explains that during the #5854 v2 cutover, a completeness warning can hold a legacy outcome-keyed row live in the same pass that publishes its stronger v2 replacement for the identical (scope_id, generation_id, image_ref) decision -- two fact IDs, one decision. That inflated one repository's vote count by one for as long as the legacy row stayed live, and the count silently dropped back down the instant a later pass retired it: the same class of bug #5887 fixed (the anchor moving with no change in underlying evidence), reached through the cutover boundary instead of a per-run factID draw. `dedupeContainerImageIdentityByLogicalRow` (`supply_chain_impact_anchor_consensus.go`) now collapses every envelope to at most one row per logical key (scope_id, generation_id, image_ref, digest) before voting, preferring the v2-formatted row when both exist. `TestBuildSupplyChainImageIdentityConsensusCollapsesLegacyAndV2Duplicate` constructs the exact legacy+v2 duplicate pair and asserts the anchor does not move when the legacy row is removed: EXIT=1 before the fix (the anchor visibly flips from the legacy/v2-duplicated repository to the single-writer repository when the legacy row is dropped, exactly the bug shape), EXIT=0 after. Mutation-kill: bypassed the dedup entirely (reverted to one-vote-per-envelope), the new test failed (EXIT=1) reproducing the flip, restored byte-identically (shasum `8ba4637b79453e36e3588ac82b4c940b403cebd72a60a0e0d8fc0e40ae174ad7` verified before and after), reran green. The dedup key needed `digest` alongside scope/generation/image_ref because the shared `containerImageIdentityImpactFact` test fixture previously left ScopeID/GenerationID/image_ref at their zero value for every synthetic row, which the dedup would otherwise have collapsed onto each other regardless of digest; the fixture now derives ScopeID/GenerationID/image_ref from each call's factID so distinct synthetic rows stay distinct, and a test that wants to simulate a real duplicate must explicitly share those fields (as the new test does). Full reducer suite green after this fix (`go test ./internal/reducer/... -count=1`, EXIT=0).
