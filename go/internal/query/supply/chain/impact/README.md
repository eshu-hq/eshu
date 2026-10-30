# Impact query read models

## Purpose

Reads the supply-chain vulnerability-impact surface from active reducer
facts in Postgres: the impact findings list and explanation
(`GET /api/v0/supply-chain/impact/*`, anchored to a CVE, advisory,
package, repository, digest, image, workload, or service), the impact
aggregates (counts and inventory rollups), the evidence-readiness
snapshots (per-family counts and freshness), the read-time
runtime-context and runtime-environment evidence joins, and the
scanner-filter and suppression-mutation adapters. Rows are
reducer-owned impact truth: what is affected, how urgently, and what
evidence is still missing.

## Ownership boundary

This package owns the read models: the findings/aggregates/readiness
store ports, their Postgres implementations, the bounded SQL texts, the
finding/explanation/aggregation builders, the finding-row and
remediation decoders, the family-local typed-decode seam for the SBOM,
package-registry, and service-catalog source-fact kinds, the
readiness families and normalization, the runtime filter SQL, the
suppression split SQL, the version-resolution and source-state models,
and the profile, limit, and vocabulary constants.

It does not own auth, the HTTP handlers, the response envelope,
capability registration, or the runtime probes that attach read-time
evidence to rows.

The supply-chain hub (`internal/query/supply/chain`, hub PR3, #6060)
keeps the handlers (`findings_handler.go`, `explain_handler.go`,
`findings_aggregates_handler.go`, `security_alerts.go`, the
investigation-packet files), the `Handler` struct, and the probes
(`*_probe.go`, `findings_scope.go`). Root package `query` keeps only the
capability matrix rows (`contract_supply_chain.go`) and the compatibility
alias file (`compat_supply_chain.go`, #6642) with the store types,
constructors, and read-model types `cmd/api`, `cmd/mcp-server`,
`internal/serviceintelhttp`, and `internal/cli`
still call as `query.NewPostgres*` / `query.SupplyChainImpact*`.
Root performs capability registration deliberately: root owns the
router and always links into the production binary.

## Exported surface

The store ports (`FindingStore`,
`AggregateStore`, `ExplanationStore`,
`ReadinessStore`,
`VulnerabilitySuppressionMutationStore`) and the Postgres
implementations with their constructors
(`NewPostgresFindingStore` with its
`WithReadModel` and `WithReadStore` variants, `NewPostgresAggregateStore`
with its `WithReadStore` variant,
`NewPostgresReadinessStore`,
`NewPostgresReadinessStoreWithReadStore` (guarded read-only snapshots),
`NewPostgresVulnerabilitySuppressionMutationStore`) and queryer ports
(`FindingQueryer`,
`AggregateQueryer`,
`ReadinessQueryer`), the values crossing those ports
(filters, rows, results, envelopes, path hops, remediation,
provenance), the builder entry points
(`BuildExplanation` and its no-evidence/ambiguous
variants, `BuildReadiness` and its unavailable
variant, `BuildFindingResult`), the SQL texts
(`ListFindingsQuery` and its winners variant,
`ExplainFindingQuery`, the aggregates/readiness/
runtime/suppression texts), the request helpers the staying handlers
call (`RequestedProfile`, `FilterProfile`,
`RequiredFindingLimit`,
`ParseIncludeSuppressed`,
`IsSupportedSuppressionState`,
`ParseScannerSeverity`, the scanner-filter sets and
`RejectUnsupportedVulnerabilityScannerFilters`,
`FirstNonEmptyQueryParam`,
`TrimExplanationFilter`,
`ExplanationAmbiguousCandidateCount`,
`FindingReadinessScope`, `PriorityFilter` with its
bucket/score helpers, `HasScope`/`HasBoundedScope`/`ReadinessScope`),
the decode entry points (`DecodeFindingRow`,
`DecodeRemediation`,
`NormalizeSort`, `ReadinessMissingContains`,
`AddRuntimeContextFact`,
`RecordRuntimeEnvironmentEvidence`), the profile, limit,
fact-kind, vocabulary, readiness-state, evidence-family, and
missing-evidence constants, and the relocated runtime-evidence types
(`RuntimeContext`, `RuntimeContextResult`,
`RuntimeEnvironmentEvidenceProbe`,
`RuntimeEnvironmentCandidate`,
`KubernetesRuntimeWorkloadRef`, `KubernetesRuntimeProbeMetadata`,
`VulnerabilitySuppressionMutationResult`).

For an explicit NVD CVE, the readiness SQL treats source state as health and
calls a target covered only when a complete, fresh, warning-free
active-generation source snapshot carries a matching versioned coverage
descriptor. The comparison uses the current configured selector (CVE ids
compared in upper case, as the collector matches them) and the endpoint
actually used, inside the same repeatable-read snapshot as target resolution.
Missing legacy metadata, stale or partial snapshots, and an in-place selector
change leave the target uncovered, which is the only source of
`nvd:committed_snapshot_missing`. A covered snapshot that counted records is
not a gap: the CVE's advisory fact decides between `ready_zero_findings` and
`evidence_incomplete`. Only a covered snapshot with `result_count = 0`
(`coverage_zero`, carried per state row and never exposed) certifies advisory
absence. A target that lists several CVEs therefore certifies none of them
absent once NVD returned any record for that target, and the target's
`source_states[]` row says so with `multi_cve_target: true` (#9322).
Source-state freshness ages by `last_success_at` only; the
collection window is reported, not aged. The window is
`vulnsource.DefaultFreshnessWindow` (14 days), the one window every writer
uses for OSV, NVD, KEV, EPSS, and GLAD, and each read ages every row against
one cutoff (#9423). Malformed CVE elements, duplicate
scope IDs, and window fields the collector's config parse refuses fail the
readiness lookup closed as `source_targets_malformed`, and the findings and
explain handlers log the fail-closed cause. A present `modified_start` or
`modified_end` must be strict RFC 3339 with a `Z` or `+-hh:mm` zone (no hour
24 or second 60) that `pg_input_is_valid` accepts, and a present
`results_per_page` must be a JSON integer of magnitude at most 999,999,999,
which keeps the int4 cast in range. The descriptor casts
each field only in the `THEN` arm of a `CASE WHEN` on the same guard, so one
garbage target can no longer abort the read as `query_error`. A comma
fraction or a `+24:00` offset, which Go accepts and Postgres refuses, reads
as malformed (fail closed).

Derived owned-package OSV scopes (#9227 F7) never reach readiness by scope-id
parsing. The collector writes each derived scope's package identities to
`vulnerability_source_state_packages` (canonical `packageidentity` ecosystem,
every consumption-key candidate) in the same statement as its source-state
upsert. `readinessDerivedSourceStateCTE` joins the request's keys to that table
by equality: package, CVE, and image requests use the resolved target keys,
and a repository-only request takes keys from the active consumption facts.
Per (instance, source, ecosystem, requested package) the newest state wins,
the distinct winners are aged by the read cutoff, and one aggregate
`SourceState` row (`Derived`, `derived://<source>/<ecosystem>`) reports the
worst state and the counts. A requested package with no matching scope makes
the row pending, so an enabled derived ecosystem with no coverage reads
`osv:pending`. When every unmatched package carries planner skip evidence
from the instance's latest `workflow_runs` row, the aggregate reads `skipped`
instead and reports `skipped_package_count` with the distinct planner
`skip_reasons` (#9612); the planner records canonical
(`packageidentity.ConsumptionKeys`) identities per skipped package, the same
shape the collector writes, so the join cannot drift. Configured targets keep
one row per scope. The 200-row bound
counts configured scopes plus aggregate rows. Known limitations: a state written
before the key table existed matches nothing until its scope is collected
again, with no backfill, and a run written before per-package skip names
existed joins nothing, so those skipped packages keep reading `pending`. Swift derived keys join the key table like every
other ecosystem (proven by a live package-key join proof), and
OS-distribution derived scopes join through their distro spellings
(`alpine`, `debian`, `ubuntu`) to the canonical `os` keys the collector
writes; Rocky and AlmaLinux stay unmatched until `packageidentity` maps them
to a canonical ecosystem.

Performance Evidence: the window-field guards (#9435 review) were measured
with the same kind of scratch harness, base SQL (`08241776a0`) against guarded
SQL, 21 warm samples each with alternating first mover in one repeatable-read
transaction, on disposable PostgreSQL 18.6 with 50 instances and 10,000
configured targets and states (200 NVD targets selecting the CVE). Plans are
node-for-node identical (266 nodes) with 645 shared hits in both. With
CVE-only targets the median moved 21.257 ms to 21.684 ms. With 9,800 non-NVD
targets carrying window strings and a page size, the worst case, it moved
34.056 ms to 92.334 ms. The cost is the invalid-target filter (1.052 ms per
instance against 0.131 ms without window strings), about 4.6 microseconds
per window-carrying target for two regex matches, two `pg_input_is_valid`
calls, and one numeric check (a standalone shim put one regex match at about
1.1 microseconds and one `pg_input_is_valid` at 0.2). The descriptor guards run only
on the 200 relevant rows. The regex is kept because it mirrors the
collector's strict parse; a target without window fields pays nothing. The
host load average was 14-39 on 18 cores, so these are relative figures.

Performance Evidence: the #9227 F7 derived-scope join was measured with a
scratch harness that ran `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` on the
assembled `ListReadinessQuery` with production arguments, alternating the
pre-F7 SQL (`98e8b15af9`) and the new SQL in one repeatable-read transaction,
nine warm samples each, on disposable PostgreSQL 18.6. Fixture: 50 instances
with 10,000 configured NVD targets and states (200 selecting the CVE), plus
either 25,000 single-package derived npm scopes or 250 batch scopes of 100
keys each. New plans read the key table and the states primary key by index
only, with no states sequential scan, zero reads, and zero temp blocks.
Medians, old to new: a package request on single scopes 132.185 ms to
14.984 ms (shared hits 1,515 to 98), a 10-package CVE 163.867 ms to
27.108 ms, a 300-package repository 133.166 ms to 28.963 ms; on batch scopes
24.234 ms to 18.677 ms, 29.856 ms to 24.010 ms, and 25.246 ms to 32.693 ms.
The batch repository read costs more because the old SQL refused it
(250 relevant scopes exceeded the bound) while the new SQL answers it. A
CVE-only configured read stayed level: 31.395 ms to 31.292 ms with 25,000
keys present, and 27.082 ms to 28.432 ms with none. The host's one-minute
load average was 12-14 on 18 cores; these are plan-shape and relative
figures, not a deployed latency claim. The key write cost is measured in the
vulnerability store README.

Earlier Performance Evidence: the #9227 F1/F5 change (covered no longer requires
`result_count = 0`, a per-target `coverage_zero` aggregate replaces the
`EXISTS`, and CVE selectors compare in upper case) was measured with a
scratch harness that wraps the read transaction and runs `EXPLAIN (ANALYZE,
BUFFERS, FORMAT JSON)` on the assembled `ListReadinessQuery` with production
arguments. Fixture: disposable PostgreSQL 18.6, 50 active collector instances,
10,000 configured NVD targets, 10,000 source-state rows, CVE-only request.
Before (`6a2f420830`) and after SQL ran alternately in one transaction on the
same data, nine warm samples each. Plans are node-for-node identical except
the per-target coverage `Result` node became an `Aggregate` over the same 200
loops (0.4-1.4 ms in both). Shared hits were equal: 630 for 200 and 250
relevant targets, 1,986 with 100 committed zero-result snapshots, with zero
reads and zero temp blocks. Median execution time for 200 targets went from
34.979 ms to 37.921 ms; the 250-target and positive-snapshot medians stayed
inside run-to-run noise. A SQL shim put the added `UPPER` at about 0.9 ms per
10,000 configured CVE elements (`COLLATE "C"` saved nothing). The host ran at
load average 82-105 on 18 cores, so absolute times are about twice the
earlier quiet-host figures and are not a deployed latency claim. The 250-target
read still detects the 201st target before any state lookup. The earlier
figures in this paragraph's history (baseline `16fad519` 14.795 ms; corrected
query 19.933-22.691 ms) were quiet-host runs of the pre-F1 SQL.

Observability Evidence: `source_states[]` reports collector health and
`source_snapshots[]` reports committed source health; a derived aggregate row
names its worst scopes in `scope_ids` and its uncovered packages in
`unmatched_package_count`, so an operator can find the scope behind
`osv:failed` or `osv:pending`; when every uncovered package carries planner
skip evidence, `readiness_state=target_skipped` and
`incomplete_reasons[]=osv:skipped:<reason>` identify the terminal verdict
without package names; when no current NVD
snapshot qualifies for absence coverage, `readiness_state=target_incomplete`
and `incomplete_reasons[]=nvd:committed_snapshot_missing` identify the gap
without exposing the descriptor, endpoint URL, or API key. A fail-closed read
(`ErrReadinessSourceTargetsMalformed`, `ErrReadinessSourceTargetsOverflow`,
`ErrReadinessConsumptionKeysNotReady`, or a query error) answers
`readiness_unavailable`; the handlers log one WARN `readiness_snapshot` stage
line whose `readiness_unavailable_cause` comes from
`ReadinessUnavailableCause`.

Every export names a staying root caller, an external `query.X`
caller, or a carried pre-existing export; see `AGENTS.md` for the
per-symbol list. See `doc.go` for the godoc-rendered contract.

## Dependencies

Internal packages, all of them leaves that never import root package `query`:

- `internal/query/querycontract` — `StringVal`, `BoolVal`, `IntVal`,
  `FloatVal`, `StringSliceVal` row-value decoders plus `QueryParam`
  and `WriteError` (root forwards to the same functions, so behavior
  is identical on both sides of the move).
- `internal/query/decode` — the classified decode failure the
  copied source-fact wrappers return via `decode.New`, plus the shared
  `DefaultSchemaMajorVersion` and `DerefString`.
- `internal/storage/postgres/array` — the array scan/build surface
  the stores read through.
- `internal/storage/postgres` — the suppression-mutation storage
  adapter.
- `internal/truth` — truth-vocabulary helpers the result model reads.

Plus `sdk/go/factschema` (typed SBOM, package-registry,
service-catalog, and vulnerability-suppression decode seams) and the
standard library.

The `compactStrings`/`firstNonEmptyString`/`appendUniqueString`/
`stringMapVal` payload helpers, the
`supplyChainFactDecodeInput` bundle, the `supplyChainSchemaEnvelope`
adapter,
the six source-fact decode wrappers, the
`decodeSupplyChainComponentEvidence` seam, the
`serviceCatalogCorrelationFactKind`/`cicdRunCorrelationFactKind`
literals, and the `supplyChainImpactFindingMaxLimit`/
`maxSupplyChainRuntimeEnvironmentCandidates` bounds are family-local
copies of trivial root values that cannot cross the package boundary;
each carries a provenance comment naming its root source.

## Telemetry

This package emits no metrics, spans, or logs of its own. The impact
routes are traced in the staying root handlers
(`telemetry.SpanQuerySupplyChainImpactFindings`,
`telemetry.SpanQuerySupplyChainImpactExplanation`,
`telemetry.SpanQuerySupplyChainImpactAggregate`, and the
`eshu.query.freshness_state` / `eshu.query.runtime_context_*` span
attributes).

## Move evidence (#6060)

This package was created by moving thirty-five files out of root
package `query` (`git mv`, no logic changes), plus a
runtime-evidence types file for the five read-model declarations the
moved rows name, and seventeen paired unit-test files that test only
moved subjects. The two assertions below are structural rather than
promissory — each names what a reader can check.

No-Regression Evidence: the move is a package relocation, not a
rewrite. `git diff -M --find-renames` pairs each file with its root
predecessor; the only statement-level changes are the `package`
clause, the `impact.` qualification at staying root call sites, the
`querycontract.` qualification of the value decoders and HTTP helpers
(root forwards to the identical functions), the export renames listed
in `AGENTS.md`, the relocated read-model type declarations (root
keeps `type X = impact.X` aliases in
`compat_supply_chain.go`), and the family-local copies of the
decode seam, deref/map helpers, fact-kind literals, and limit bounds,
each documented with its root source and verified
behavior-preserving by the re-qualified root test suite
(`go test ./internal/query/...`) and the moved package suite
(`go test ./internal/query/supply/chain/impact`), which pin the
filtering, grouping, paging, normalization, SQL shape, placeholder
binding, and dead-letter behavior. The source-path-asserting
placeholder test was repointed at the new file location and still
parses the production list function.

No-Observability-Change: there is no observability to change — this
package emits none, and the handler/probe spans, attribute names, and
capability strings keep their exact values (the relocated profile
constants and the copied error constructor produce identical values;
only their package qualifier changed).

## Gotchas / invariants

- The `vulnerability_source_state_candidates` CTE counts only rows whose
  collector instance is enabled and not deactivated (#9420), filtered before
  its `LIMIT 200`. A retired instance's rows stop updating, so without the
  filter its last `fresh` rows kept a target ready after the in-tree
  collector was cut over. `TestReadinessSourceScopeTableAgreesWithSharedSourceParser`
  keeps the scope-id to source table in step with `vulnsource.SourceForScopeID`.
  QA read replica EXPLAIN (576 rows): the `vulnerability_source_states` seq scan
  is unchanged; a hash join on the 30-row `collector_instances` table is added.
- Do not import root package `query`. Root's
  `compat_supply_chain.go` already imports this package, so the
  reverse import cycles.
- The capability is registered in ROOT (`contract_supply_chain.go`),
  not here — root owns the router and always links into production.
- `FindingFilter` must carry an anchor (`HasScope`);
  the store rejects anchorless reads before running SQL, and the
  handlers reject them before reaching the store. The page limit is
  bounded by `supplyChainImpactFindingMaxLimit` (family-local copy —
  keep byte-identical to root's `handler.go` value).
- A dropped decode contribution is a dead-lettered malformed fact
  (`input_invalid`), not missing data. The typed wrappers drop
  rather than zero-fill; reducer-derived kinds with no sdk struct yet
  stay on the raw path per the #4784 ADR note in
  `decode_helpers.go`.
- `ReadinessState*`, `EvidenceFamily*`, `MissingEvidence*`,
  `UnsupportedTargetKind*`, and `FreshnessLabel*` are closed
  vocabularies pinned by readiness tests on both sides of the move;
  adding a member means updating the normalization, the SQL legs, and
  both suites.
- `package.consumption` never treats fleet-wide manifest rows as evidence for
  a package, CVE, digest, or image anchor. The read model resolves canonical
  package identities in one repeatable-read snapshot, intersects combined
  anchors, and fails coverage closed while the consumption sidecar is dirty,
  backfilling, or has ambiguous registry-key ownership. A trusted
  default-registry PURL or package ID can use source manifest evidence without
  a registry owner row. Repository-only reads retain their active repository
  index path.
- A central version pin (`central_version_declaration`, such as a NuGet
  `PackageVersion`) is never `package.consumption` evidence (#8824). The
  repository arm filters it with `storagepostgres.NotCentralVersionDeclarationSQL`
  after the index probe; the package-anchored arm reads the consumption
  sidecar, which never stores a pin. A repository with only pins therefore
  reports `owned_packages` missing.
- `package_manifest_active` (both payload shapes) and
  `package_dependency_gap_active` are repository-anchored only (#7088). Each
  starts from the repository's own scopes (`ingestion_scopes.source_key = $11`,
  which covers repository and `repository_ref` scopes) and probes
  `fact_records` once per scope through a LATERAL subquery. The `OFFSET 0`
  fence keeps that subquery unflattened, so the probe binds repository,
  scope and active generation in the Index Cond of migration 121's index
  (entity_metadata shape) or migration 159's (legacy top-level `config_kind`
  shape and the five gap kinds). Do not reintroduce a `$11 = '' OR` escape:
  under a generic plan it forces a probe of every active scope. Do not pin
  through `scope_id = 'git-repository-scope:' || $11`: that drops
  `repository_ref` scopes. Keep the gap `IN` list textually identical to
  migration 159's predicate, or the planner cannot prove the implication.
  PRECONDITION: the `source_key = $11` anchor reaches the same facts as the
  `repo_id` payload predicate only when every active git dependency-variable
  fact sits in a scope whose `source_key` equals the fact's payload `repo_id`.
  Collector-written scopes hold it (`buildScope` stamps both from `repo.ID`),
  and the gap CTE already relied on it (#7007); `scopestore.SourceKey` falls
  back to the scope id when scope metadata has no `source_key` and
  `parserfixture.Emitter` writes scopes with none, so such scopes drop out.
  On QA all 799 repository scopes satisfy it and a 50-scope sample of
  117,010 active `content_entity` rows had 0 violations (teammate-reported,
  2026-10-02); a fleet-wide fact-level count is NOT_CHECKED.
  Proof: `TestSupplyChainImpactReadinessRepoArmScopeLive` (scheduled, not run
  in CI) and the CI-run static guards in
  `readiness_repo_arm_static_guards_test.go`, which pin the three `OFFSET 0`
  fences, the `scope.source_key = $11` anchors, the absent `$11 = '' OR`
  escape, the three `generation_id` binds and `is_tombstone = FALSE` filters,
  the arm-1/arm-2 exclusivity, the empty-repository-implies-`$20` table test,
  and the match between both readers' predicates and the embedded
  migration's index predicate. `TestReadinessRepoArmExecutableTextIsPinned`
  also pins the full executable text of the three probes to
  `testdata/readiness_repo_arm_probes.golden`; a deliberate edit to either
  statement regenerates it with `-update-repo-arm-golden` and carries the plan
  proof. Evidence:
  `docs/internal/evidence/7088-readiness-repo-scope.md`.
- Image-reference targets batch the current digest set and active registry identity
  lookups. SBOM component counts join each active component to its active
  document by scope, generation, and document ID; the subject digest is on the
  document. This keeps the component family and package target anchored to the
  same source evidence.
- Performance Evidence: on disposable PostgreSQL 18, a 513-digest image with
  513 package-ID-only components required 515 sequential target queries before
  batching and three after. A representative 80k-registry-fact shim measured
  513 point lookups at 347.013 ms median versus one 513-ID array lookup at
  13.642 ms median. The document-to-component index shim measured 2362.449 ms
  without its index versus 0.297 ms with it on 6k documents and 18k components.
  The exact SBOM-warning query over 513 target documents and 513 unrelated
  active warnings fell from 830.324/818.126 ms custom/generic to
  1.973/3.791 ms with a partial document-ID index; cross-scope warning results
  were identical. On matched 10k warning inserts, indexed median was 59.991 ms
  versus 36.619 ms without the index (+23.372 ms); unrelated insert runs were
  too variable to establish a regression. Two full 513-component reads after
  the index took 60.849 and 49.440 ms. Local full-read samples before the
  index ranged from 634 ms to 1.556 s; those were not controlled comparisons
  or deployed endpoint p95 proof.
- No-Regression Evidence: the disposable live tests assert exact manifest and
  SBOM component counts for digest anchors, 513 current image digests, 513
  package IDs, and at most three resolver SQL calls. A duplicate document ID
  across two active scopes retains the warning by document-ID semantics. Tests
  also cover CVE PURL and package-ID-only targets, absent packages, collisions,
  and unready fences.

- The winners-read cutover (`ReadFromWinners`,
  `WinnersReadEnv`) keeps output byte-identical
  between the legacy dedup and the maintained read model; the
  placeholder-binding test pins the production argument list.
- The handler-driving tests that share helpers, fakes, or corpus with
  staying files remain in root package `query` (see `AGENTS.md`): they
  drive the hub handlers through `Mount` there. Do not "reunite" them
  here; the probe, scope, and live suites that need no root helpers
  moved with the handlers to the hub package
  (`internal/query/supply/chain`).

## Related docs

- [HTTP API Reference](../../../../../docs/public/reference/http-api.md)
- [Telemetry](../../../../../docs/public/reference/telemetry/index.md)
