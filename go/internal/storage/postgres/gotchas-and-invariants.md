# storage/postgres Gotchas And Invariants

This companion note keeps the package README focused while preserving detailed
operational lessons that future storage changes still need to respect.

## Query And Queue Invariants

- `ProjectorQueue.Ack` runs six SQL statements inside a transaction. Pass a
  `SQLDB` or an `InstrumentedDB` wrapping a `SQLDB`; a plain `db.ExecQueryer`
  without `db.Beginner` will cause Ack to fail.
- `upsertFacts` deduplicates by `fact_id` before batching (`facts.go:206`).
  Skipping deduplication causes `SQLSTATE 21000` on `ON CONFLICT DO UPDATE`
  when the same `fact_id` appears twice in one batch.
- `ListFactsByKind` keeps a stable `(observed_at, fact_id)` keyset cursor
  (`listFactsByKindQuery` and `listFactsByKindCursorQuery` in
  `facts_filtered.go`). Lowering the page size below the write batch size
  can make reducer-only reads spend most of their time in Postgres round trips
  rather than extraction or graph writes.
- Never fold the first-page and cursor statements back into one. Both keyset
  loaders deliberately ship two statements each, because the combined form
  needs a guard like `$4::timestamptz IS NULL OR (observed_at, fact_id) > (...)`
  and that disjunction cannot be pushed down as an index qual under a generic
  plan — which is where these statements land, since the services prepare them
  through `database/sql`. Every fact in a generation shares one `observed_at`,
  so the cursor can only advance on `fact_id`; without the index qual each page
  rescans and re-sorts the whole generation and pagination goes quadratic. This
  is invisible to an `EXPLAIN` with literal parameters, which plans the folded
  form at 4.7ms while production pays 580ms per page. Probe with `PREPARE` plus
  `SET plan_cache_mode = force_generic_plan` instead. `#6154` carries the
  measurements, and `facts_filtered_keyset_test.go` fails on any disjunction in
  either cursor predicate.
- A large `= ANY($n)` or `<> ALL($n)` array that filters many scanned rows
  through the pooled pgx statement cache needs a custom plan: pass
  `pgx.QueryExecModeCacheDescribe` (or `QueryExecModeExec`) as the first
  query argument. From the sixth execution of a SQL text on a connection the
  default `QueryExecModeCacheStatement` statement runs the generic plan, which
  compares a Param array linearly per row; only a custom plan's Const array
  of nine or more elements is hashed. Prove the trap with `PREPARE` under both
  forced plan modes first. `ListActivePackageManifestDependencyFacts` carries
  the measurements (#9586,
  `docs/internal/evidence/9586-manifest-read-custom-plan.md`).
- `ListFactsByKindAndPayloadValue` is only for top-level JSON payload fields
  that are part of a reducer domain's truth contract. Do not use it to paper
  over missing parser metadata or to guess at nested payload shape.
- Shared projection intents are idempotent by `intent_id`. Writers should
  upsert the same row on retry rather than minting a new ID. The 2000-row
  upsert batch keeps each statement below Postgres' parameter limit while
  avoiding small-batch round trips on code-call-heavy repositories.
- Current source-run history is distinct from prior acceptance-unit history.
  `HasCompletedAcceptanceUnitDomainIntents` intentionally ignores
  `source_run_id` so new accepted runs can detect prior graph state;
  `HasCompletedAcceptanceUnitSourceRunDomainIntents` includes `source_run_id`
  so chunked code-call projection can skip only same-run retractions.

## Fact Readback Invariants

- `ListCICDRunFactsForRunKeys` is the narrow history read for a generation that
  contains a `ci.artifact` but no `ci.run`. It accepts exact
  provider/run/attempt keys plus stable keys for payload-empty artifact
  tombstones, reads only the same scope, and ranks tombstones before filtering
  normal retained evidence. For a requested artifact tombstone stable key only,
  it separately returns the latest older payload-bearing artifact as routing
  identity; the reducer must remove that exact stable key before classification.
  `ListCICDRunFactsForScopePatch` selects run keys from the newest older
  generation containing `ci.run`, then unions exact keys from current live artifacts.
  The normal generation is a lower bound for run-scoped, workflow-image, and
  deployment evidence. A live artifact may recover an older omitted `ci.run`
  anchor, but it cannot recover that run's pre-baseline environment, step,
  trigger, workflow-image, deployment, or artifact evidence. Current artifact
  tombstones are stable-key controls only; they never add a run key, and an
  identity already absent from the authoritative window is a no-op. This keeps
  unaffected runs from the latest normal window without resurrecting runs or
  evidence that window omitted, even when an unpublished predecessor reducer
  result was superseded. It preserves the typed optional-repository fallback
  when either side omits `repository_id` and rejects more than 12,000 returned
  facts.
  After this read, the reducer gives every current non-artifact fact authority
  over retained history only for its exact raw `(fact_kind, stable_fact_key)`
  identity, while artifacts retain their normalized stable-key and live run-key
  rules. Valid current tombstones are control rows and never reach typed decode;
  a blank tombstone identity fails closed instead of leaving stale evidence live.
  These reads support the reducer's patch path only; a generation containing a
  run stays on the existing same-generation path.
- `ListOwnedPackageDependencyTargets` serves workflow-coordinator derivation.
  Package-registry callers use package-level identities so repeated versions of
  one package cannot starve later packages. Vulnerability-intelligence callers
  use package-version identities and retain dependency `source_location` so
  Swift OSV planning can send the source Git URL required by OSV `SwiftURL`.
  The read is a hash-slot window (#9422): each distinct target hashes to slot
  `sha256(key)` mod P, with P the smallest power of two where
  `P * TargetLimit >= 2 * T`, computed from `COUNT(*) OVER ()` in the same
  statement. A read returns slot `RotationBucket mod P` in the global order and
  the window stats (T, P, slot, slot_size, rows read). A change in T never moves
  a target to another slot, so every target is planned within P windows. Keep
  the hash on the distinct rows (about 30k on QA), not the 2.7M scanned rows;
  the slot predicate depends on the window aggregate, so it cannot be pushed
  below it. The key COALESCEs NULL parts: `sha256(NULL)` would drop a target
  from every slot. Do not use `md5()`: the QA server runs in FIPS mode. The
  reader fails the read when the statement's P or slot disagrees with
  `workflow.OwnedPackageTargetSlotCount`. The stats come from a one-row
  `window_stats` CTE LEFT JOINed to the slot rows, so an empty slot or T = 0
  still reports them; an inner join would return no row and no stats.
  Vulnerability-intelligence callers also set `ExpandPackageManagerAliases`
  (#9285) and `IncludeSBTOwnedTargets` (#9313): the filter selects every raw
  `package_manager` alias of each configured ecosystem (`gradle` for `maven`,
  `gomod`, `go-module`, and `go_module` for `go`) and the `ecosystem` column
  folds them onto the canonical name inside SQL, so a pair declared under two
  aliases is one `DISTINCT ON` row and one rank. A `maven` read with the sbt
  flag also selects raw `sbt` rows with their
  `sbt_cross_version`/`sbt_binary_artifact` columns; `sbt` stays out
  of the alias table on purpose, so the `ecosystem` column keeps reporting
  those rows as `sbt` and the derivation maps them onto Maven targets through
  the cross-version rule instead. The alias table lives in `workflow` and also
  drives the derivation's Go mapping, so the two cannot disagree. The filter stays a raw
  `= ANY($1)` on the indexed expression. Package-registry callers set the alias
  flag but leave the sbt flag off, so they keep the #9285 selection
  byte-identical and select no sbt rows.
- `ListOSPackageAdvisoryTargets` and `ListSBOMComponentAdvisoryTargets` serve
  vulnerability-intelligence installed-evidence derivation. OS package reads
  stay on active `vulnerability.os_package` facts joined to the active
  generation and filtered by vendor advisory source/distro ecosystem. SBOM
  component reads stay on active `sbom.component` facts that have active
  same-scope attached `reducer_sbom_attestation_attachment` evidence and filter
  by PURL ecosystem before applying the bounded rotated limit. SBOM rows derive
  exact package identity from the PURL; component payload versions that
  conflict with the PURL version are dropped before planning. The readers return
  exact source facts only; the coordinator owns admission and partial-evidence
  skip reasons.
- `ListActivePackageManifestDependencyFacts` serves both package-source
  correlation and supply-chain impact. The query stays indexed on active Git
  dependency entities by `(package_manager, entity_name)`, so vulnerability
  impact can load repository lockfile evidence for one advisory package without
  waiting for package-registry enrichment to finish. A Maven ecosystem filter
  expands to `gradle`, `maven`, and `sbt` rows and adds a second branch for sbt
  `%%` rows, whose artifact name rides in `sbt_binary_artifact` (#8961); see
  `facts_active_package_manifest_dependency.go`. The branch reads the `sbt`
  prefix of the same index, so its cost follows the number of sbt dependency
  rows. Both branches over-select; `packageidentity.ManifestConsumptionKeys` is
  the accuracy gate.
  Stored sbt rows have no sidecar keys until the reducer's marker-gated sbt
  repair adds them (#8961). It writes the sidecar outside the dirty fence on
  purpose: it only inserts, under the scope row lock, so readiness never flips.
  A rolling deploy can drop keys it added; see `consumption/README.md`.
- `ListActiveJVMReachabilityFacts` serves JVM vulnerability reachability
  enrichment after Maven or Gradle dependency evidence has already proven a
  canonical repository and resolver-backed API package prefix. The query is
  bounded by repository IDs, the JVM file partial index, and the resolver API
  package list across parser imports, parser calls, and SCIP calls; reducers
  still perform the API-prefix match and keep missing source-set, resolver,
  reflection, dependency-injection, and generated-code evidence visible.
  No-Regression Evidence: `go test ./internal/storage/postgres -run
  'TestListActiveJVMReachabilityFacts' -count=1` failed before the SQL passed
  the API package list into the active-file query, then passed with the
  repository/API/language bound and a matching Java parser-import row. `go test
  ./internal/reducer -run
  'TestSupplyChainImpactHandlerLoadsActiveJVMReachabilityFacts|TestBuildSupplyChainImpactFindingsMarksJVMReachableFrom(ParserImport|SCIPEvidence)|TestBuildSupplyChainImpactFindingsKeepsJVMGapsUnknownWithoutAPIIdentity|TestBuildSupplyChainImpactFindingsNeverMarksJVMNotCalledWithoutAnalyzer'
  -count=1` proves the reducer still sends the repository/API filter and keeps
  parser and SCIP evidence accurate. No-Observability-Change: the read path
  still uses the existing instrumented Postgres query span and
  `eshu_dp_postgres_query_duration_seconds` metric from the reducer's
  Postgres adapter, plus reducer execution spans/counters and the persisted
  supply-chain impact reachability/missing-evidence payloads; no route, queue,
  graph write, worker, runtime knob, metric name, or metric label changed.
- `ListActiveSupplyChainImpactFacts` includes provider security alerts in the
  same package/repository-bounded read used for vulnerability, package, SBOM,
  image, OCI registry, and service evidence. The selector includes raw OCI
  manifest, index, tag-observation, and referrer facts only behind package,
  digest, repository, or image-reference predicates, so reducers can recover
  image/SBOM anchors without scanning the whole registry fact set. This lets
  alert-seeded impact admission reuse active owned dependency evidence without
  scanning all repository alerts. Reducer reconciliation keeps provider-scoped
  repository IDs separate from canonical `repository_id` values, so Postgres
  fact payloads should preserve both when the source uses a provider-owned
  repository namespace.
- `GetSupplyChainAdvisoriesForRepos` (issue #2127) is the repo-scoped read that
  sources the service vulnerabilities evidence family (#1990). It loads active,
  non-tombstone `reducer_supply_chain_impact_finding` facts filtered by
  `payload->>'repository_id'`, paged by the `fact_id` keyset, and maps each
  finding to a `servicecatalog.ServiceVulnerabilityRecord` grouped by repository id. It
  is served by the partial index
  `fact_records_supply_chain_impact_repository_lookup_idx`
  (`payload->>'repository_id'`, `fact_id ASC`, `generation_id`) under the
  `reducer_supply_chain_impact_finding` + `is_tombstone = FALSE` predicate. A
  service is attributed an advisory only through a real impact finding on its
  repository; there is no fuzzy advisory-to-service name match.
- `ListActiveSBOMAttestationAttachmentFacts` keeps attachment repair bounded by
  subject digest, document id/digest, statement id/digest, payload digest, and
  referrer digest. It may read active SBOM document/component and attestation
  evidence plus OCI referrer facts, but it must not infer an attachment unless
  reducer-owned subject evidence can prove the join.
- Supply-chain impact parser-file follow-up is separate from normal repository
  follow-up. Repository IDs still load bounded context facts such as workload,
  service, image, CI/CD, and suppression evidence, but active `file` facts only
  load through the JS/TS parser-file repository filter and the SQL language
  predicate for JavaScript, JSX, TypeScript, and TSX. Since #8880 that is the
  `file_repo_id` and `file_scope` branches on
  `fact_records_jsts_reachability_repo_file_idx`, issued only when
  FileRepositoryIDs is non-empty (the #5237 gate as "no file statement"). Non-JS/npm findings must
  not use broad repository IDs to pull every active file fact for a repository.
  No-Regression Evidence: `go test ./internal/reducer -run
  'TestSupplyChainImpactHandlerRequestsParserFilesOnlyForNPMReachability|TestBuildSupplyChainImpactFindingsUsesJSTSPackageAPIReachability|TestBuildSupplyChainImpactFindingsKeepsJSTSMissingAndAmbiguousEvidenceExplicit'
  -count=1` and `go test ./internal/storage/postgres -run
  'TestListActiveSupplyChainImpactFactsQuerySeparatesParserFileFollowUp|TestListActiveSupplyChainImpactFactsQueryBoundsRepositoryFollowUp'
  -count=1` prove non-JS/npm repository follow-up excludes parser files while
  npm JS/TS reachability still requests JS/TS file evidence.
  No-Observability-Change: the change only narrows the existing
  `FactStore.ListActiveSupplyChainImpactFacts` SQL predicate and reducer filter
  keys; operators continue to diagnose the path through
  `eshu_dp_postgres_query_duration_seconds`, reducer run spans/counters, and
  durable supply-chain impact finding payloads.
- Advisory evidence reads stay bounded by first-class advisory identity fields,
  package IDs, or PURLs before active-generation validation. Performance
  Evidence: issue #868 changed the read path from a broad active vulnerability
  CTE to selector-first identity branches backed by
  `fact_records_vulnerability_active_*_lookup_v2_idx`; representative
  preserved-volume proof returned `CVE-2021-44228` in 0.691s cold and
  0.435s/0.439s warm, while `EXPLAIN ANALYZE` completed the present-CVE SQL in
  472.419ms using those indexes. No-Observability-Change: the API route still
  emits `query.advisory_evidence`, Postgres query duration metrics, truth
  envelope metadata, status/error bodies, `count`, `limit`, `truncated`, and
  `next_cursor`; no graph query, queue, reducer lane, worker, runtime knob, or
  metric label changed.

## Runtime And Fencing Invariants

- `GetResolvedRelationshipsForReposWithCorpusFence` must stay ONE statement.
  Its fence CTE reuses `incompleteScopeRelationshipGenerationsPredicate`, and
  the verdict is only trustworthy because it shares the rows' statement
  snapshot (#6740). Splitting it back into a fence query and a read reopens
  the window where a foreign scope retires after the fence, the read misses
  its rows, and it re-activates before any recheck.
  `TestCorpusFenceSnapshotRetireAndReactivateBetweenStatements` fails on that
  split. The LEFT JOIN always yields at least the fence row; rows with a NULL
  `relationship_type` are filler, and no row at all is an error, never a
  verdict. No-Regression Evidence and plans:
  `docs/internal/evidence/6740-corpus-fence-snapshot.md`.
- The NornicDB semantic gate in `ReducerQueue.Claim` is gated on a boolean
  parameter and must not be removed without an ADR; it prevents
  `semantic_entity_materialization` storms on NornicDB label indexes.
- `PackageRegistryIdentityLocker` uses transaction-scoped
  `pg_advisory_xact_lock` keys to coordinate package UID canonical writes
  across ingester, standalone projector, and bootstrap-index processes. It
  de-duplicates and sorts package IDs before acquiring locks, commits after the
  protected canonical write succeeds, and rolls back on callback failure so
  Postgres releases the lock automatically. No-Regression Evidence: `go test
  ./internal/storage/postgres -run 'TestPackageRegistryIdentityLocker' -count=1`
  proves sorted/de-duplicated lock acquisition and rollback-on-error behavior.
  Observability Evidence: waits over 100ms emit a structured
  `package registry identity advisory locks acquired` log with
  `package_uid_count`, `lock_key_sample`, and `wait_s`; existing Postgres
  transaction failures still surface as wrapped callback or commit errors.
- `aws_relationship_materialization`, `observability_coverage_materialization`,
  `iam_can_assume_materialization`, `s3_logs_to_materialization`,
  `s3_external_principal_grant_materialization`,
  `rds_posture_materialization`, `iam_instance_profile_role_materialization`,
  and `s3_internet_exposure_materialization` claims wait on the exact
  `cloud_resource_uid` / `canonical_nodes_committed` readiness row for the
  same scope, generation, and `entity_key`. This keeps relationship work and
  CloudResource node-property work pending or retrying until
  `aws_resource_materialization` has made `CloudResource` nodes visible, while
  allowing the resource materialization row in the same conflict key to claim
  and publish the phase.
- `WorkflowControlStore` claim mutations use `ErrWorkflowClaimRejected` for
  fenced writes; callers must stop processing when this error is returned.
- `WorkflowControlStore.FailClaimTerminal` uses a dense seven-argument SQL
  mutation because terminal failures do not requeue and therefore do not need a
  `visible_at` placeholder. Do not leave skipped parameter numbers in workflow
  claim SQL; Postgres must infer every prepared-statement parameter type before
  it can persist the terminal failure.
- `AWSScanStatusStore` mutations must keep their fencing guards. A stale AWS
  worker must not overwrite per-tuple scanner or commit state from a newer
  claim. ObserveAWSScan and CommitAWSScan stay pinned to the exact
  `(generation_id, fencing_token)` so stale collectors cannot clobber a newer
  owner. StartAWSScan accepts a cross-generation overwrite when the prior row is
  terminal OR the new `last_started_at` is strictly newer than the stored value
  (or the row has none), which lets a fresh workflow generation reclaim the
  per-target slot after an orphaned `running`/`pending` row was left by a
  collector that died mid-flight. Without this widening one orphaned row blocks
  every future generation and the workflow runtime spins stale-fence retries;
  see issue #612.
- `AWSScanStatusStore` returns `awscloud.ErrScanStatusStaleFence` when a
  mutation affects zero rows; callers wrap and route the failed claim to
  terminal (the AWS claimed runtime does this via
  `awsruntime.FailureClassStaleFence`) instead of looping it on the retryable
  queue.
- `AWSScanStatusStore.CommitAWSScan` clears previous commit failure class and
  message when a retry finally commits a scan whose scanner-side status is
  `succeeded`. Scanner-side failed, partial, budget-exhausted, and credential
  failures remain in the row so status readback can still explain active
  degraded scopes.
- `WebhookTriggerStore` treats webhook payloads as trigger evidence only. It
  preserves merged pull-request number, URL, and title provenance for bounded
  read-model enrichment, but the Git collector must still fetch the repository
  before freshness becomes true.
- `awsfreshnessstore.AWSFreshnessStore` treats AWS Config and EventBridge
  events as trigger evidence only. The AWS collector must still scan the
  affected service tuple before cloud inventory becomes fresh.
- `incidentfreshnessstore.IncidentFreshnessStore` treats PagerDuty and Jira webhooks as source-scoped
  trigger evidence only. It coalesces repeated delivery events by
  `freshness_key`, claims queued rows with `FOR UPDATE SKIP LOCKED`, and records
  handed-off or failed rows after the workflow coordinator authorizes a
  configured collector `scope_id`.
- `FactStore.LoadIncidentRoutingRawEvidence` returns the RAW PagerDuty
  incident-routing evidence for the graph materialization domain: the
  `incident.record` and same-generation `incident_routing.*` fact envelopes
  undecoded, plus the Terraform-source `PagerDutyDeclaration` content rows
  read through a lowercased service-name allowlist derived from the incident
  facts' service summaries. It no longer decodes fact payloads or filters
  applied evidence by resource class — the reducer decodes each fact through
  the typed `sdk/go/factschema` seam, so a malformed required field
  dead-letters as a per-fact input_invalid quarantine instead of a silent
  empty read here. The service-name allowlist is only a query bound (never
  authoritative correlation truth), so reading it raw carries no accuracy
  hazard. The declared `content_entities` metadata read stays storage-decoded
  because it is entity metadata, not a fact payload. Routing facts without an
  incident anchor do not trigger a cross-scope graph mutation.
- Schema definitions in `bootstrapDefinitions` are applied in slice order.
  Tables with foreign key constraints on other tables must appear after their
  dependencies.

- `ListActiveSupplyChainImpactFacts` resolves fact IDs once per call (#8880).
  `storage/postgres/facts/impact` generates every branch statement, its
  anchor, the foreign-key guard, and migrations 217-229 from one table. Each
  statement reads `fact_records` only in a MATERIALIZED candidate CTE through
  its anchor and applies the active-generation join once per distinct
  (scope, generation) pair: inlined, the planner misestimates that join at one
  row and walks every active scope. Do not add an arm to one statement only;
  add it to `retiredArms` so the guard, the bounded statements, and the
  exactness tests see it. The frozen pre-#8880 statement is the differential
  oracle (`supply_chain_impact_evidence_live_test.go`); see
  `docs/internal/evidence/8880-supply-chain-impact-id-resolution.md`.
- Vulnerability-suppression deployment context is narrowing-only (#5466).
  `ListActiveSupplyChainImpactFacts` discovers suppressions through
  `cve_id`, `advisory_id`, `package_id`, `purl`, `repository_id`, or
  `subject_digest`; it MUST NOT scan by `environment`, `workload_id`, or
  `service_id` alone. The rejected deployment-only query shape matched
  85,715 of 300,000 synthetic rows and took about 22.7 seconds through the
  real paginated Go path. Keeping deployment fields out of the SQL discovery
  filter removes that broad-scan class while the reducer applies them as
  conjunctive, fail-closed match constraints after an identity-anchored fact
  is loaded. Scope identity predicates use normalized `lower(btrim(...))`
  binds with the complete Unicode White_Space set used by Go
  `strings.TrimSpace`, so SQL selection agrees with the reducer's
  case-insensitive matcher. A fact-kind guard avoids paying that normalization
  cost on non-suppression rows. The 2,000-row per-call cap remains defense in
  depth: the suppression statement (the only one that returns the kind) stops
  at 2,001 rows in byte order, the loader keeps 2,000, and it reports
  truncation only when another row actually exists.
- The #7926 destructive-successor guard sizes like for like (#8750). A full
  successor is compared with the prior generation's collector rows:
  `successorGuardCountQuery` counts collector rows, all rows, and file rows in
  one index-only pass, and reducer-prefixed rows (written back into the
  generation after it activates, never streamed by a collector) stay out of
  the comparison. A git delta successor is sized by the file-level union it
  will project over its anchor, the newest projected full generation at its
  `DeltaBaselineCommitSHA`. The anchor comes from `resolveDeltaBaseAtCommit`,
  the same resolver the projector's overlay uses, so the guard and the
  projection cannot disagree about the base. Path matching goes through
  `facts.DeltaScope.FilePath`. The probe of the anchor's file paths
  (`successorGuardAnchorFilePathsQuery`) runs only when the zero-query lower
  bound fails. A delta with no anchor stores, so the projector's base-missing
  refusal arms the reindex watermark; a generation whose stream disagrees on
  being a delta is sized as a full and logs `successor_delta_scope_mismatch`.
  The prior is the larger of the duplicate-check candidate and the scope's
  active generation (review F1): the claim sweep lets a newer full supersede
  a pending generation unprojected, so a pending candidate alone does not
  bound what a successor retracts. `successorGuardActiveGenerationQuery`
  uses `activeGenerationPredicate`, never `backfill.ActiveScopeGenerationQuery`,
  whose COALESCE returns a pending row when none is active. It runs only for
  a pending candidate; a pending candidate whose projector work is not live
  is not sized next to an active generation, and every read failure fails
  the commit closed.
- The guard sizes its prior on the base connection before the commit
  transaction opens, so a same-scope commit or Ack can land in between and
  leave the prior stale and smaller than the truth (#8750 window). Once
  `upsertIngestionScope` holds the scope row (its ON CONFLICT DO UPDATE arm
  row-locks it until commit), no same-scope generation can be stored or
  activated and the active pointer cannot move. `recheckSuccessorPrior`
  therefore runs after the scope upsert and before `upsertScopeGeneration`
  (after which the successor's own row is the newest candidate): it re-reads
  the candidate tuple (generation id, status, work_live) on the transaction,
  plus the active id when the in-tx candidate is pending. On any difference
  it re-sizes on the transaction with `successorPriorForGuard` and uses that
  result alone, never the larger of the two; a failed read or count fails
  the commit closed. Reads that run on the transaction share one connection,
  so each must close its cursor before the next statement (pgx refuses with
  "conn busy"); the delta-union resolver reads target and base through
  `readDeltaUnionTarget` and `readDeltaUnionBase` for that reason. Do not move
  the recheck, do not compare a subset of the tuple, and do not widen it to
  un-hinted generations here (that is a separate issue).
  See `docs/internal/evidence/8750-successor-guard-sizing.md`.

## Owned-package alias expansion evidence (#9285)

Performance Evidence: the owned-package reader query keeps the same plan with
alias expansion on: `fact_records_active_package_dependency_entity_idx` index
scan on the raw `package_manager` expression, then the scope and generation
nested loops. Synthetic corpus on a private PostgreSQL 18.6 (the production
bootstrap schema, 185,000 fact rows, 85,000 dependency facts of which 65,000 are
active, 4% of active dependency facts Gradle, 22% of Gradle pairs sharing a
Maven twin), `PREPARE`/`EXECUTE` with `EXPLAIN (ANALYZE, BUFFERS)`, limit 1000,
three warm runs after one discarded warm-up. Maven only, before and after:
128.4, 136.3, 126.3 ms and 175.2, 171.7, 172.1 ms; buffers 106,141 and 127,306.
The difference is the work the change adds. It returns 15,305 distinct targets
instead of 12,722, 2,583 of them Gradle-only pairs. The `CASE` fold alone,
measured on the same maven-only filter, costs about 5 ms (134.1, 136.9, 140.8
ms against 126 to 136 ms). Go, Maven, npm, and PyPI together, before and after:
594.6, 606.2, 602.1 ms and 681.9, 683.9, 702.9 ms (+14%).
The alias-off query is byte-identical to the previous query, and the relative
order of every pre-existing target is unchanged. Absolute ranks shift once when
Gradle-only pairs join the ranked list, so the first rotation window after
deploy covers a different slice.

Observability Evidence: no new signal. The derivation reports skipped targets
by reason and ecosystem through the existing derived-target skip evidence; a
skipped Gradle dependency now appears under `maven`. Existing Postgres
query-duration telemetry covers the read.
