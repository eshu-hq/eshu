# storage/postgres Gotchas And Invariants

This companion note keeps the package README focused while preserving detailed
operational lessons that future storage changes still need to respect.

## Query And Queue Invariants

- `ProjectorQueue.Ack` runs five SQL statements inside a transaction. Pass a
  `SQLDB` or an `InstrumentedDB` wrapping a `SQLDB`; a plain `ExecQueryer`
  without `Beginner` will cause Ack to fail.
- `upsertFacts` deduplicates by `fact_id` before batching (`facts.go:206`).
  Skipping deduplication causes `SQLSTATE 21000` on `ON CONFLICT DO UPDATE`
  when the same `fact_id` appears twice in one batch.
- `ListFactsByKind` keeps a stable `(observed_at, fact_id)` keyset cursor
  (`facts_filtered.go:71`). Lowering the page size below the write batch size
  can make reducer-only reads spend most of their time in Postgres round trips
  rather than extraction or graph writes.
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

- `ListOwnedPackageDependencyTargets` serves workflow-coordinator derivation.
  Package-registry callers use package-level identities so repeated versions of
  one package cannot starve later packages. Vulnerability-intelligence callers
  use package-version identities and retain dependency `source_location` so
  Swift OSV planning can send the source Git URL required by OSV `SwiftURL`.
  The rotation offset lets bounded full-corpus runs advance past the first
  sorted page without changing worker counts or query scope.
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
  waiting for package-registry enrichment to finish.
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
  finding to a `reducer.ServiceVulnerabilityRecord` grouped by repository id. It
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
  predicate for JavaScript, JSX, TypeScript, and TSX. Non-JS/npm findings must
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
- `ListActiveSupplyChainImpactFacts` must be able to select a
  `vulnerability.suppression` fact scoped ONLY by `environment`/`workload_id`/
  `service_id` (#5466), not only by `cve_id`/`advisory_id`/`package_id`/
  `purl`/`subject_digest`/`repository_id`. Before this fix, the SQL had no
  predicate that could ever match such a suppression -- wired in the scope
  struct and matcher (`go/internal/reducer/supply_chain_suppression*.go`) but
  dead on the real load path: the fact was accepted and stored, but a reducer
  intent that never happened to load it from the SAME scope/generation batch
  would never see it, so the operator's suppression silently never applied.
  The fix adds a dedicated `fact.fact_kind = 'vulnerability.suppression' AND
  (payload->'scope'->>'workload_id' = ANY(...) OR ...->>'service_id' =
  ANY(...) OR ...->>'environment' = ANY(...))` branch and three new
  `SupplyChainImpactFactFilter` fields (`Environments`, `WorkloadIDs`,
  `ServiceIDs`) populated in `supplyChainImpactFilter`
  (`go/internal/reducer/supply_chain_impact_active_filter.go`) from
  already-loaded `reducer_ci_cd_run_correlation` (environment),
  `reducer_workload_identity`, and `reducer_service_catalog_correlation`
  (workload_id/service_id) evidence -- the same "known anchor from already-
  loaded evidence" pattern `RepositoryIDs` already uses, not a new lookup
  mechanism.
  No-Regression Evidence: `go test ./internal/storage/postgres -run
  'TestListActiveSupplyChainImpactFactsQuery' -race -count=1` and `go test
  ./internal/reducer -run
  'TestSupplyChainImpactFilter|TestSupplyChainImpactFollowUpFilter' -race
  -count=1` pass, including the pre-existing sibling-predicate assertions
  unchanged. The REAL load-path proof (not a query-text assertion, and not a
  hand-built envelope handed straight to the evaluator) is
  `TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedOnlyByDeploymentContextLive`
  (`facts_active_supply_chain_impact_scope_live_test.go`, build tag
  `integration`): a real Postgres instance seeded with two
  environment-scoped-only suppression facts (`stage`, `prod`) in their own
  ingestion scopes proves `FactStore.ListActiveSupplyChainImpactFacts` with
  `Environments: ["stage"]` returns exactly the `stage` fact and never the
  `prod` one.
  P1-1 follow-up (case/alias exact-match gap): the first cut of this
  predicate compared the raw payload value with exact-match `= ANY(...)`.
  `decodeVulnerabilitySuppressionScope`
  (`go/internal/reducer/supply_chain_suppression_decode.go`) canonicalizes
  `environment` through `environment.Canonical` (`"production"` -> `"prod"`)
  and only `strings.TrimSpace`s `workload_id`/`service_id`, while the
  matcher (`suppressionScopeMatchesFinding`) compares all three with
  case-insensitive `strings.EqualFold`. A payload authored as
  `{"environment":"production"}` -- the literal shape
  `TestBuildVulnerabilitySuppressionsDecodesEnvironmentWorkloadServiceScope`
  proves decodes correctly -- could never be SELECTED by an exact-match
  `= ANY('{prod}')` predicate, so it was silently inert in production even
  though decode and the matcher both accept it. The fix changed the
  predicate to `lower(btrim(fact.payload->'scope'->>'environment', E'
  \t\n\v\f\r')) = ANY($14::text[])` with `$14` expanded through
  `environment.Aliases()` (`expandEnvironmentAliasFilterValues`,
  `facts_active_supply_chain_impact.go`) so a canonical `"prod"` filter also
  binds every alias spelling, and the identical `lower(btrim(..., E'
  \t\n\v\f\r'))` treatment for `workload_id`/`service_id` (`$12`/`$13`)
  with the bind values passed through `lowerCleanedStringFilterValues` to
  match. All three predicates now share one whitespace/case treatment; see
  the round-2 review F-4 entry below for why `btrim` with an explicit
  character class, not plain `trim`, was required. Failing-test-first
  proof: `TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByEnvironmentAliasLive`
  seeds a suppression payload with the literal alias `"production"` and a
  filter of `Environments: ["prod"]`; run against the pre-fix predicate it
  failed (`len = 0, want 1`), and passes after the `lower()`+alias-expansion
  fix (later widened to `lower(btrim(...))` by the F-4 follow-up below).
  P2-1 follow-up ($12/$13 coverage and bind-order): the live test above only
  ever exercised `$14` (Environments). `$12` (WorkloadIDs) and `$13`
  (ServiceIDs) had no load-path proof, and a query-text `strings.Contains`
  assertion cannot catch a bind-order swap of
  `filter.WorkloadIDs`/`filter.ServiceIDs` in
  `listActiveSupplyChainImpactFactsPage`, which would silently make both
  anchors inert while every other test in the repo still passes.
  `TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByWorkloadAndServiceIDsLive`
  seeds two DISTINCT facts (workload-only and service-only, each with
  mixed-case/whitespace payload values) and queries with both filter fields
  populated, proving both predicates AND that a 12/13 swap would fail it.
  `TestListActiveSupplyChainImpactFactsBindsWorkloadAndServiceIDsToDistinctPlaceholders`
  (hermetic, no DSN required, `facts_active_supply_chain_impact_test.go`)
  asserts the bound `$12`/`$13` argument values directly against
  `db.queries[0].args[11]`/`args[12]` using distinct workload/service
  values, so a bind-order regression fails even when no live Postgres is
  configured.
  Index/sargability evidence (pre-`btrim` measurement -- see the F-4 entry
  immediately below for why the shipped predicate gained `btrim` after this
  was measured, and why re-measuring was not required): `EXPLAIN (ANALYZE,
  BUFFERS)` on a 300,000-row seeded `vulnerability.suppression` table with
  environment values drawn from a realistic ~7-token closed domain (`prod`,
  `production`, `qa`, `stage`, `staging`, `dev`, `uat` -- matching
  `environment.Aliases()`'s token set, not a single low-cardinality
  synthetic value) shows the then-current
  `lower(payload->'scope'->>'environment') = ANY('{prod,production}')`
  predicate, the pre-fix exact-match `= ANY('{prod}')` predicate, and the
  pre-existing sibling `payload->'scope'->>'cve_id'` predicate all produce
  the IDENTICAL plan shape (`Parallel Seq Scan` on `fact_records`, no index
  used by any of the three, 14-27ms at this scale) -- the `lower()` wrapper
  and the wider alias-expanded array do not change the scan strategy, they
  only return a higher row count by construction (2/7 of rows for the
  2-value alias-expanded array vs 1/7 for the single-value exact match),
  which is expected and bounded by the total suppression-fact count, not a
  new category of scan. No new index was added; per this skill's Index
  Doctrine, adding one requires evidence the query is hot enough for the
  predicate shape to benefit, and this call has no such evidence (see the
  corrected frequency note below).
  Round-2 review F-4 (whitespace-class mismatch, fixed): Postgres `trim(x)`
  is `btrim(x, ' ')` -- it strips ASCII space only. Go's
  `strings.TrimSpace` (used by `payloadStr` and, again, inside
  `environment.Canonical`) strips the full Unicode `White_Space` property.
  Proven live on Postgres 16.14: `lower(trim(' Production '))` ->
  `'production'` matches `ANY['prod','production']` (true), but
  `lower(trim(E'\tProduction\n'))` -> `E'\tproduction\n'` does NOT match
  (false) -- so `{"environment":"\tProduction\n"}` decoded to `"prod"` and
  matched the in-memory matcher while the prefilter silently never loaded
  it. This applied to all three of `$12`/`$13`/`$14` identically, not just
  environment. Fixed by widening every `trim(...)` in this branch to
  `btrim(..., E' \t\n\v\f\r')`, an explicit ASCII whitespace class (space,
  tab, newline, vertical tab, form feed, carriage return) -- this closes
  the gap for realistic operator-authored payloads (tab/newline padding)
  but does NOT close it for exotic non-ASCII Unicode whitespace (NBSP
  U+00A0, the U+2000-200A run, U+2028/2029, U+202F, U+205F, U+3000, ...),
  which Postgres has no built-in primitive to trim; that residual gap is a
  documented, accepted limitation, not an unstated assumption. Re-running
  `EXPLAIN (ANALYZE, BUFFERS)` with `btrim(..., E' \t\n\v\f\r')` in place
  of `trim(...)` on the same 300,000-row realistic-distribution seed
  produces the IDENTICAL plan shape as the pre-`btrim` measurement above
  (`Parallel Seq Scan` on `fact_records`, 28,572 rows per worker / 71,428
  rows removed by filter per worker, no index used) -- `btrim` with a
  literal character-class argument is exactly as unindexable as `trim`, so
  the pre-`btrim` EXPLAIN evidence above stands without needing a fresh
  300k-row re-run.
  Round-3 review F-6 (sibling suppression-scope predicates had the
  identical exact-match defect, fixed): the five OTHER suppression-scope
  predicates -- `package_id`, `purl`, `cve_id` (the "pre-existing sibling"
  measured in the Index/sargability evidence above, from BEFORE this
  follow-up -- that measurement is a historical snapshot, not the shape
  this predicate ships as today), `subject_digest`, `repository_id` -- were
  still exact-match `->'scope'->>'X' = ANY($1/$2/$3/$4/$7)`, the identical
  defect class F-4 fixed for environment/workload_id/service_id:
  `scopeAnchorMatches` compares all five with `strings.TrimSpace` +
  `strings.EqualFold`, so a payload of `{"cve_id":"cve-2026-1234"}`
  (lowercase) decoded and matched in Go but was never selected here. Fixed
  by REPLACING (not supplementing) those five `->'scope'->>'X'` exact-match
  predicates with `lower(btrim(fact.payload->'scope'->>'X', E'
  \t\n\v\f\r')) = ANY($N::text[])` on five NEW placeholders (`$15`
  package_id, `$16` purl, `$17` cve_id, `$18` subject_digest, `$19`
  repository_id), bound to `lowerCleanedStringFilterValues` of the SAME
  filter values already bound to `$1`/`$2`/`$3`/`$4`/`$7`. New placeholders
  were required rather than reusing `$1`-`$4`/`$7` because those are ALSO
  bound to the top-level (non-`"scope"`) sibling predicates on the same
  lines, which serve other fact kinds (`vulnerability.affected_package`,
  `sbom.component`, ...) whose exact-match behavior must not change --
  only the `"scope"`-nested comparisons were rewritten, with no exact-match
  fallback left for them.
  No-Regression Evidence:
  `TestListActiveSupplyChainImpactFactsQueryNormalizesSuppressionScopeSiblings`
  and
  `TestListActiveSupplyChainImpactFactsBindsNormalizedSuppressionScopeSiblings`
  (`facts_active_supply_chain_impact_scope_normalize_test.go`) prove the
  predicate shape and `$15`-`$19` bind positions hermetically. The real
  load-path proof is
  `TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByLowercaseCVEIDAndPaddedPURLLive`
  (`facts_active_supply_chain_impact_scope_normalize_live_test.go`, build
  tag `integration`): seeds a suppression scoped only by a lowercase CVE ID
  and another scoped only by a whitespace-padded PURL, both selected when
  the reducer's derived filter carries the conventional
  uppercase/unpadded form. Index/sargability: `$15`-`$19` are additional
  `OR`-ed string comparisons of the identical predicate shape already
  measured for `$14` above (`lower(btrim(...)) = ANY(...)`, no index used,
  `Parallel Seq Scan`); only the JSON key name and bind values differ, so
  no separate EXPLAIN re-run was performed for `$15`-`$19` specifically.
  Round-4 review F-10 (`advisory_id` had NO load-path predicate at all,
  fixed): unlike the F-6 keys above, which at least had a stale exact-match
  predicate before normalization, `advisory_id` was a dead scope key from
  the start -- `scopeAnchorMatches`, `suppressionScopeIsEmpty`, and the
  reasons string in `supply_chain_suppression_reasons.go` all accept/
  advertise it as a sufficient sole anchor, but this query's `WHERE` clause
  had no predicate for it at all. Corrected source-of-truth (an earlier
  investigation wrongly named `security_alert.repository_alert`'s
  `ghsa_id` as the only distinct raw source -- that was wrong, and a
  SECOND wrong inference was caught and corrected below):
  `vulnerability.cve`/`affected_package` each carry a raw, top-level
  `advisory_id` field. `vulnerability.affected_product` does NOT -- the
  `AffectedProduct` struct (`sdk/go/factschema/vulnerability/v1/affected_product.go`)
  is CVEID/Criteria/MatchCriteriaID/Vulnerable only, its sole emitter
  `newNVDAffectedProductEnvelope`
  (`go/internal/collector/vulnerabilityintelligence/nvd_envelope.go`)
  builds no `advisory_id` key, and
  `sdk/go/factschema/vulnerability/v1/README.md`'s required-fields table
  lists `AdvisoryID` for `CVE`/`AffectedPackage` only. `advisory_id` is
  separately indexed by
  `fact_records_vulnerability_active_advisory_lookup_v2_idx`
  (`schema_fact_records_vulnerability_indexes.go`) -- **root cause of the
  wrong `affected_product` inference, worth internalizing:** that index's
  `fact_kind IN (...)` predicate names six kinds (including
  `affected_product`, `epss_score`, `known_exploited`, `reference`)
  because it constrains which ROWS get indexed, not which payloads carry
  the `advisory_id` key; nothing stops the index expression evaluating to
  `NULL` for a kind lacking the field. Payload-shape claims must come from
  `sdk/go/factschema` and the emitter, never from a DDL predicate list.
  `supplyChainCVEID` is `firstNonBlank(cve_id, advisory_id)`
  (`go/internal/reducer/supply_chain_impact_summary.go`), so whenever a
  fact already has a populated `cve_id` (the common case), its DISTINCT
  `advisory_id` (e.g. a GHSA ID alongside an NVD CVE ID) never reached
  `CVEIDs` and had nowhere else to go -- a cross-scope suppression scoped
  ONLY by `advisory_id` was unreachable. Fix: a new `AdvisoryIDs []string`
  field on `SupplyChainImpactFactFilter`, collected in
  `supplyChainImpactFilter` SEPARATELY from `CVEIDs` for
  `vulnerability.cve`/`affected_package` and from
  `vulnerability.suppression`'s own `scope.advisory_id`, threaded through
  `empty()`/`supplyChainImpactFollowUpFilter`/`mergeSupplyChainImpactFactFilters`
  the same way `Environments`/`WorkloadIDs`/`ServiceIDs` were in P1-1. New
  SQL placeholder `$20`: `lower(btrim(fact.payload->'scope'->>'advisory_id',
  E' \t\n\v\f\r')) = ANY($20::text[])`, same normalization shape as
  `$15`-`$19`. What this does NOT cover:
  `security_alert.repository_alert`'s `ghsa_id`/`ghsa_ids` are not
  collected; `SupplyChainImpactFinding.AdvisoryID` (the classification-time
  provenance-selected value used elsewhere) is unrelated and unchanged --
  this fix only affects which facts the active-evidence prefilter can
  select.
  No-Regression Evidence:
  `TestSupplyChainImpactFilterCollectsAdvisoryIDsSeparatelyFromCVEIDs`,
  `TestSupplyChainImpactFilterAdvisoryIDOnlyIsNotEmpty`, and
  `TestSupplyChainImpactFollowUpFilterTracksAdvisoryIDs`
  (`go/internal/reducer/supply_chain_impact_active_filter_test.go`) prove
  the collection/empty/follow-up behavior.
  `TestListActiveSupplyChainImpactFactsQueryNormalizesSuppressionScopeAdvisoryID`
  and
  `TestListActiveSupplyChainImpactFactsBindsNormalizedSuppressionScopeAdvisoryID`
  (`facts_active_supply_chain_impact_scope_normalize_test.go`) prove the
  predicate shape and `$20` bind position hermetically. The real load-path
  proof is
  `TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByAdvisoryIDOnlyLive`
  (`facts_active_supply_chain_impact_scope_normalize_live_test.go`, build
  tag `integration`): seeds a suppression scoped only by `advisory_id` and
  proves it is selected. Index/sargability: `$20` is the identical
  predicate shape already measured for `$14`-`$19`; no separate EXPLAIN
  re-run was performed.
  P2-3 follow-up (frequency correction): the original evidence here
  described this as a "once-per-generation" call. That was wrong.
  `ListActiveSupplyChainImpactFacts` is reached per supply-chain-impact
  INTENT via `loadSupplyChainImpactEvidence` -> `loadActiveSupplyChainImpactFacts`,
  and `loadActiveSupplyChainImpactFactsUntilStable`
  (`go/internal/reducer/supply_chain_impact_handler_helpers.go`) issues up
  to `maxSupplyChainImpactActiveEvidenceLoads = 8` paginated rounds per
  intent, not once. Separately, adding `Environments`/`WorkloadIDs`/
  `ServiceIDs`/`AdvisoryIDs` to `SupplyChainImpactFactFilter` widened
  `SupplyChainImpactFactFilter.empty()`
  (`go/internal/reducer/supply_chain_impact_active_filter.go`) to return
  `false` for a NEW class of intents: one carrying only deployment evidence
  (environment/workload/service) or only an advisory_id, with no
  package/CVE/digest/repository anchor at all. `AdvisoryIDs` (#5466
  round-4 review F-10) widens `empty()` identically to
  `Environments`/`WorkloadIDs`/`ServiceIDs` (#5466 P1-1): an
  advisory-only-scoped suppression intent now yields a non-empty follow-up
  filter where the load previously short-circuited. Before #5466 that
  intent's derived filter was fully empty
  and `loadActiveSupplyChainImpactFacts` short-circuited to `nil, nil`
  without issuing a query; after #5466 it issues a full paginated Seq Scan.
  This new-invocation class is UNMEASURED -- no benchmark or production
  metric isolates it yet -- but it is bounded the same way every other
  invocation is: at most `maxSupplyChainImpactActiveEvidenceLoads = 8`
  rounds per intent, the same cap
  `TestSupplyChainImpactHandlerStopsActiveEvidenceExpansionConservatively`
  (`go/internal/reducer/supply_chain_impact_active_test.go`) already asserts
  for the pre-existing loop. The no-index CONCLUSION still stands regardless
  of this correction: the predicate is OR-ed into a query that already
  performs a `Parallel Seq Scan` for its other branches at this scale, so an
  index on this one predicate would not change the scan strategy chosen for
  the query as a whole.
  No-Observability-Change: same as the parser-file-follow-up entry above --
  only the SQL predicate and reducer filter keys changed; the existing
  Postgres query duration metric, reducer run spans/counters, and durable
  suppression decision payload are unaffected.
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
- `AWSFreshnessStore` treats AWS Config and EventBridge events as trigger
  evidence only. The AWS collector must still scan the affected service tuple
  before cloud inventory becomes fresh.
- `IncidentFreshnessStore` treats PagerDuty and Jira webhooks as source-scoped
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
