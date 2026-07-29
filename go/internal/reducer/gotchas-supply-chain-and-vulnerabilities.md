# Reducer Gotchas — Supply-Chain And Vulnerability Domains

Split from `README.md` (issue #5786). Domain-shape, drift, container
image identity, SBOM, supply-chain impact, suppression, and
ecosystem-parity invariants live here; keep the top invariants and a
pointer in `README.md`.

- **All reducer domains must be cross-source, cross-scope, and truth-emitting**
  — enforced by `OwnershipShape.Validate`; domains either write canonical graph
  truth, publish durable reducer facts such as `aws_cloud_runtime_drift`, or
  emit bounded counters such as `package_source_correlation`.
- **AWS runtime drift publication is graph-neutral for this slice** —
  `AWSCloudRuntimeDriftHandler` writes `reducer_aws_cloud_runtime_drift_finding`
  facts through `PostgresAWSCloudRuntimeDriftWriter`; graph nodes and MCP/API
  read models need their own frozen shape before Cypher lands.
- **Multi-cloud runtime drift shares one path keyed on `cloud_resource_uid`** —
  `MultiCloudRuntimeDriftHandler` reuses the AWS structural join
  (`cloudruntime.Classify`) but joins on the canonical identity keyspace so AWS,
  GCP, and Azure emit one orphaned/unmanaged/ambiguous/unknown vocabulary. It
  writes `reducer_multi_cloud_runtime_drift_finding` facts through
  `PostgresMultiCloudRuntimeDriftWriter`, read back by
  `postgres.MultiCloudRuntimeDriftFindingStore` (issues #1997, #1998). The domain
  is additive: it registers only when both a `MultiCloudRuntimeDriftEvidenceLoader`
  and writer are wired, so an unwired loader leaves the domain unregistered rather
  than dropping intents. The Postgres uid-join evidence loader (joining
  `reducer_cloud_resource_identity`, Terraform-state, and Terraform-config facts
  by `cloud_resource_uid`) and the provider-generalized query/MCP surfaces are the
  next slices; graph nodes stay deferred exactly like the AWS drift domain.
- **Container image identity is digest-first** —
  `ContainerImageIdentityHandler` writes `reducer_container_image_identity`
  facts only for explicit digest or single-tag-to-digest matches. Ambiguous,
  unresolved, and stale tag outcomes stay diagnostic counters until stronger
  evidence proves safe identity. Git parser facts can expose image references
  through `entity_metadata.container_images`; the reducer also accepts the
  older `metadata.container_images` fixture shape for compatibility. CI/CD
  `container_image` artifacts can also seed image identity when they carry an
  artifact digest. The reducer uses the matching CI run's `repository_id` as the
  source anchor, upgrades mutable tag refs to digest refs when both are present,
  and admits digest-only artifacts only when one OCI registry observation proves
  the digest's repository. Digest-only artifacts with multiple registry
  repositories stay ambiguous and produce no canonical write.

  The writer is **not** generation-authoritative, and #5847 is the open bug that
  names why. The fact identity embeds `outcome` and `image_ref`, so a replay that
  re-classifies an image writes a NEW `fact_id` beside the old one, and a replay
  that demotes an image out of the two canonical outcomes (`exact_digest`,
  `tag_resolved`) writes no row to overwrite it with. The read path serves
  whatever is live — `ListContainerImageIdentities` has no `DISTINCT ON`,
  `GROUP BY`, or per-digest latest-wins — so a stale decision is returned
  alongside the corrected one and counted twice in the aggregate rollups. The
  domain is in the bootstrap maintenance reopen slice precisely so replays happen
  once the cross-scope OCI generation activates, so this is the ordinary path.
  Removing the superseded row takes a generation-authoritative retire, tracked as
  #5854; the analysis, the measured traps, and the reason a retire cannot land
  before the OCI collector's bounded-degradation paths are fixed are in
  `docs/internal/evidence/5847-container-image-identity-retire.md`.

  What DOES ship for the colliding case is a **fenced upsert**. Two passes that
  agree on the outcome mint the same `fact_id`, so they collide rather than
  duplicating — and they can still disagree on the PAYLOAD, because
  `source_revision`, `source_revision_provenance` and
  `build_provenance_repository_ids` are filled in by cross-scope enrichment whose
  visibility depends on which generations were active at load time. The write
  therefore carries a watermark: `ContainerImageIdentityWrite.EvidenceAsOf`,
  captured before the handler's first fact load, rendered into
  `fact_records.fencing_token` and stamped by `reducerFactBatchInsertQuery` on the
  INSERT. Evidence-read time, not write time — write time ranks a worker that
  stalled past its lease highest, which is the inversion the watermark exists to
  stop, and the reducer queue does not order those two for you (its in-flight
  exclusion requires a LIVE lease, while an expired lease is re-admitted, and an
  expired lease IS the stalled-worker case since heartbeat loss is quarantined
  only after `Handle` returns). A zero watermark is a hard error: `0` is what
  rows carry by table default, so a domain that forgot it would look fenced and
  behave exactly like the six writers that never opted in.

  That insert's conflict update is **guarded**, not merged:
  `WHERE fact_records.fencing_token <= EXCLUDED.fencing_token` rejects a stale
  pass's upsert whole, content columns included. Raising only the token while
  assigning content unconditionally (`GREATEST`) protects the token and nothing
  else, which is worse than no fence — the row would end up carrying stale
  content behind a fresh watermark, and any consumer that ranks by that token,
  #5854's retire included, would trust the wrong row. `<=` rather than `<`,
  because a retry, a redelivery, or a second chunk of the same pass carries the
  same evidence-read watermark and `<` would discard all of them while reporting
  success. The guard is inert for the six callers that bind `0` against rows at
  `0`, proven live rather than assumed.

  What the guard does NOT close: a pass fenced out in WHOLE still reports
  `CanonicalWrites=N`, which is byte-identical to a pass that landed normally.
  The rows are right either way; the summary cannot tell an operator which of the
  two happened. Reading back the accepted `fact_id`s (the `#4444`
  `upsertFactBatchReturningAccepted` shape) would close that, and needs its own
  live and concurrency proof.

  No-Regression Evidence: `go test ./internal/reducer
  ./internal/replay/costcounting ./cmd/bootstrap-index -count=1` covers the
  fence: `TestContainerImageIdentityFencingTokenOrdersByEvidenceReadTime`
  (direction — the earlier evidence read ranks lower),
  `TestContainerImageIdentityWriterStampsTheFencingTokenOnTheInsert` (the
  watermark reaches the durable row rather than resting at `0`),
  `TestContainerImageIdentityWriterRejectsMissingEvidenceAsOf` (hard error, no
  statement issued), `TestContainerImageIdentityHandlerStampsEvidenceReadTime
  BeforeLoading` (watermark taken before the load), and
  `TestReducerFactBatchInsertFreezesItsConflictGuard` with
  `TestReducerSQLNormalizerKeepsNonASCIIWhitespace` (the shared insert's whole
  `ON CONFLICT` clause is frozen, and the normalizer that compares it does not
  erase a byte PostgreSQL rejects). The real-Postgres proofs live in
  `go/internal/storage/postgres/reducer_fact_batch_insert_fence_live_test.go` and
  run on `ESHU_POSTGRES_DSN` alone: a stale pass cannot overwrite a fresher row's
  content, an equal-token retry still applies, and the guard is inert for a
  writer that never opted in. The write path gains no statement — the
  `container-image-identity` cost budget stays at 1 statement per intent
  execution and its N+1 negative control still costs 2.

  No-Observability-Change (fence): the fenced insert adds no metric. It runs
  inside the already-instrumented `InstrumentedDB.ExecContext` wrapper that
  records `eshu_dp_postgres_query_duration_seconds`, a write rejected by
  `validateContainerImageIdentityFence` before any statement is issued surfaces
  as a non-success `status` on `eshu_dp_reducer_executions_total` (labeled
  `domain`=`container_image_identity`), and the existing
  `eshu_dp_container_image_identity_decisions_total` counter and reducer run
  spans are unchanged.

  No-Regression Evidence: `go test ./internal/reducer -run
  'TestBuildContainerImageIdentityDecisions(ConsumesCICDArtifactDigestWithRepositoryAnchor|PrefersCICDArtifactDigestOverMutableTag|IgnoresNonContainerCICDArtifacts|RejectsDigestOnlyCICDArtifactWhenRegistryDigestIsAmbiguous)|Test(BuildContainerImageIdentity|ContainerImageIdentity|PostgresContainerImageIdentity)'
  -count=1` proves CI/CD artifact evidence joins through a repository-anchored
  run, prefers immutable digests over mutable tags, ignores non-container
  artifacts, and fails closed for digest collisions. The implementation extends
  the existing in-memory reducer fact pass and registry index; it adds no graph
  round trip, queue domain, worker, schema change, or API/MCP route.

  No-Observability-Change: container image identity decisions continue to emit
  `eshu_dp_container_image_identity_decisions_total` by domain and outcome after
  durable writes succeed. Existing reducer run spans, fact-load timings,
  execution counters, evidence summaries, and durable
  `reducer_container_image_identity` payload fields expose source repository
  anchors, evidence fact IDs, outcomes, identity strength, and missing/ambiguous
  evidence without adding high-cardinality metric labels. #5423 threads a
  digest-matched ci.run's commit into the decision as the scalar
  `source_revision_provenance` payload field (`oci_config_source_label` or
  `ci_run_commit`), reachable via the ci.artifact container_image_identity
  projector trigger and replayed by the bootstrap maintenance reopen; it reuses
  the same decision counter, reducer run spans, and query/MCP handler
  instrumentation, and adds no new metric, span, log key, queue domain, or
  runtime knob.
- **SBOM attachment keeps trust dimensions separate** —
  `SBOMAttestationAttachmentHandler` writes
  `reducer_sbom_attestation_attachment` facts for attached verified,
  unverified, parse-only, subject mismatch, ambiguous subject, unknown subject,
  and unparseable outcomes. Component evidence stays evidence only; this
  domain must not emit vulnerability priority or affected-by findings. The
  SBOM attachment index treats multiple distinct attestation subjects as
  ambiguous, not as a first-subject match. OCI referrer rows can seed the
  active evidence walk by subject or referrer digest, but attachment facts are
  emitted only when explicit SBOM document/component or attestation evidence
  proves the subject.
- **Supply-chain impact is evidence-first** —
  `SupplyChainImpactHandler` writes `reducer_supply_chain_impact_finding`
  facts only from explicit vulnerability, affected package, owned
  package-consumption, SBOM component, attachment, or image identity evidence.
  Exact package-manifest or lockfile dependency versions can prove an observed
  package version. The reducer preserves the exact installed version, the
  requested manifest range, the selected fixed version, and the match reason as
  separate finding fields. Version/range evaluation is ecosystem-aware for npm,
  Cargo, Go modules, Pub, Hex, Swift, Composer constraints, NuGet semantic
  versions, Maven version/range ordering, PyPI PEP 440, vendor-backed OS
  package matching, and RubyGems `Gem::Version`-style requirements.
  Swift impact requires exact
  `Package.resolved` remote source-control pin evidence and a source-backed OSV
  `SwiftURL` package identity. Malformed advisory ranges fail closed as
  partial evidence with explicit missing-evidence reasons. Unsupported matcher
  ecosystems do not publish impact findings; the query readiness envelope
  surfaces observed unsupported dependencies as coverage gaps with stable
  reason codes. Npm `package-lock.json`, PHP `composer.lock`, and Ruby Bundler
  `Gemfile.lock` rows also preserve the ordered dependency path, depth,
  direct/transitive flag, and runtime/dev scope when the lockfile proves the
  chain, so vulnerability impact can explain whether a finding came from a
  direct dependency, through an owned transitive chain, or from development-only
  evidence.
  Vulnerability-scoped impact runs also load active manifest dependency facts
  by advisory ecosystem and package name, so exact source dependency evidence
  can publish repository impact before package-registry enrichment catches up.
  Package-registry identity facts can still bound active vulnerability lookups,
  and the active evidence walk expands through package IDs, PURLs, CVEs, SBOM
  document IDs, subject digests, image refs, repository IDs, and CPE criteria
  until no new bounded join key appears. Raw OCI manifest, index,
  tag-observation, and referrer rows can trigger repair and supply digest,
  repository, or image-reference anchors, but impact findings still require
  joined vulnerability, package, SBOM attachment/component, or reducer-owned
  image identity evidence. Package-registry version facts are upstream metadata
  and must not be treated as installed versions. CVSS, EPSS, and KEV stay risk
  signals; they never prove reachability without package or runtime evidence,
  and missing deployment evidence remains visible. Exact repository-scoped
  `reducer_service_catalog_correlation` facts are attached to the finding
  evidence path, but they do not create `service_ids` or `workload_ids` unless
  the fact carries those anchors. Repository-only catalog facts preserve
  `catalog_entity_refs` and `catalog_owner_refs` when present. If the finding
  already has workload evidence and the catalog fact names a catalog entity,
  that entity ref is the service-catalog anchor, so the reducer does not emit
  `service/workload catalog anchor missing`; `service_ids` still stay empty
  unless the catalog fact carries an explicit service id. Catalog evidence that
  lacks a service id, workload id, and catalog entity ref still reports
  `service/workload catalog anchor missing` so API and MCP callers can
  distinguish present but incomplete catalog evidence from absent
  service-catalog correlation evidence.

  No-Regression Evidence: `go test ./internal/reducer -run
  'TestBuildSupplyChainImpactFindings(ConsumesRepositoryOnlyServiceCatalogEvidence|ReportsScopedUnresolvedServiceCatalogEvidence|AttachesWorkloadIdentityWithoutServiceCatalog|AttachesDeploymentLaneEvidence|AttachesRepositoryScopedOperationalAnchors)'
  -count=1` failed before repository-only exact service-catalog evidence
  produced a precise missing-hop reason and before catalog owner/entity anchors
  were preserved on impact findings, then passed after the reducer attached the
  catalog fact while leaving service/workload anchors empty unless explicit IDs
  exist.

  No-Observability-Change: the change only adjusts in-memory finding
  finalization over facts already loaded through
  `ListActiveSupplyChainImpactFacts`; existing reducer counters, persisted
  `evidence_path`, `evidence_fact_ids`, `service_ids`, `workload_ids`,
  `catalog_entity_refs`, `catalog_owner_refs`, and `missing_evidence` remain
  the operator-facing signals.

  #5469 added CI-declared-artifact digest selection on top of this bullet; see
  `docs/internal/evidence/5469-ci-declared-artifact-digest-selection.md`.
- **Go-vulnerability reachability is classified, not invented** —
  `ClassifyGoVulnerabilityReachability` joins `vulnerability.go_module_evidence`
  facts (parsed from repository `go.mod` and `go.sum`), Go ecosystem
  `vulnerability.affected_package` facts, and `vulnerability.go_call_reachability`
  facts (parsed from govulncheck JSON output) into one finding per
  (advisory, module, repository) tuple with one of five reachability levels:
  `symbol_reachable`, `package_import_reachable`, `not_called`, `module_only`,
  or `unknown`. Before emitting, the classifier compares the module's
  effective version (replacement when a `replace` directive applied, declared
  `required_version` otherwise) against the advisory's SEMVER ranges and
  fixed versions; safe (post-fix) findings are dropped, advisories with
  missing or unparseable range data are kept with an explicit
  "advisory affected-range evidence missing" note, and findings backed by
  govulncheck evidence bypass the filter because govulncheck already proved
  the binary actually used the vulnerable code. The reducer does not re-run
  govulncheck or re-derive the call-graph; it preserves the
  govulncheck-compatible JSON evidence and records the rule
  (`symbol`/`import`/`not_called`/`module`/`unknown`) used to choose the
  level so API/MCP can explain the decision.
- **Suppression evidence is first-class** —
  Performance Evidence: VEX/operator suppression evaluation runs in-process
  against the bounded fact set the impact handler already loads, so it adds
  no extra queue, lease, or graph write paths. Per finding the work is
  O(suppressions × scope keys) with case-insensitive string compares and
  short-circuit returns; for the largest fact set the handler observes in
  CI fixtures (`TestSupplyChainImpactHandlerLoadsActiveEvidenceAndWritesFindings`
  in `supply_chain_impact_test.go` and the new
  `supply_chain_suppression_handler_test.go` cases) the additional decode
  and evaluate steps stay under one millisecond per finding on the same
  scope, so the existing `go test ./internal/reducer -count=1` gate is the
  baseline.
  No-Regression Evidence: the additions to the bounded Postgres active
  evidence query (`go/internal/storage/postgres/facts_active_supply_chain_impact.go`)
  reuse the existing OR-branch shape and only add four `payload->'scope'->>...`
  predicates and one extra `fact_kind` value; row counts in
  `TestListActiveSupplyChainImpactFactsQueryIncludesVulnerabilitySuppression`
  show the same bounded page semantics, so no new full table scan is
  introduced. Operators can re-run the same `cd go && go test
  ./internal/storage/postgres -count=1` gate to confirm the predicate set
  before/after a change.
  Observability Evidence: `SupplyChainSuppressionDecisions`
  (`eshu_dp_supply_chain_suppression_decisions_total`) is registered in
  `internal/telemetry/instruments.go` and emitted from
  `SupplyChainImpactHandler.emitCounters` with the closed-enum `outcome`
  label (active, not_affected, accepted_risk, false_positive, ignored,
  expired, provider_dismissed, scope_mismatch), so a 3 AM operator can
  detect VEX or operator-policy drift without re-running the reducer.
  `SupplyChainImpactHandler` evaluates `vulnerability.suppression` facts
  against each finding via `EvaluateSupplyChainSuppression`. The decision is
  always populated (`state=active` when nothing matched) and persisted on the
  finding payload, including the source (`vex_statement`, `eshu_policy`,
  `provider_dismissal`), justification, author, timestamps, reason, evidence
  reference, and optional VEX document/statement IDs. Suppressions apply only
  when every populated scope key (`cve_id`, `advisory_id`, `package_id`,
  `purl`, `repository_id`, `subject_digest`, `evidence_path`, `environment`,
  `workload_id`, `service_id`) matches the
  finding identity; mismatched scope yields the `scope_mismatch` state so the
  finding stays visible and operators can audit drift. Expired suppressions
  surface as `expired` rather than hidden. Provider dismissals are evidence:
  the reducer surfaces them as `provider_dismissed` and never auto-excludes
  the finding from the default API view. The handler emits
  `eshu_dp_supply_chain_suppression_decisions_total` per state so operators
  can detect VEX/policy drift without re-running the reducer.
  Operator-authored facts arrive through the authenticated suppression mutation
  route as one immutable full-set generation under
  `operator:vulnerability_suppressions`. Reducer admission uses
  `factschema.DecodeVulnerabilitySuppression`; missing identity, provenance,
  authorship time, or scope fields are quarantined as `input_invalid` instead
  of reaching the evaluator. A malformed expiry remains fail-closed evidence
  and can never turn into an active hidden decision.
  Temporary operator decisions are evaluated again at query time against one
  request-bound UTC clock. When that clock reaches `expires_at`, direct,
  materialized, aggregate, and explain reads expose the same immutable operator
  row as `expired` without waiting for unrelated evidence or a reducer replay.
- **Suppression scope supports deployment-context narrowing (#5466)** —
  `environment`, `workload_id`, and `service_id` are optional conjuncts on an
  identity-anchored `vulnerability.suppression` scope. At least one of
  `cve_id`, `advisory_id`, `package_id`, `purl`, `repository_id`, or
  `subject_digest` remains mandatory; deployment context never acts as a
  cross-vulnerability wildcard or SQL discovery key. Environment values use
  the shared canonical alias contract, and every populated deployment field
  must match baked finding evidence. Missing or ambiguous evidence fails
  closed to `scope_mismatch`. Because one canonical finding stores one
  suppression decision while deployment dimensions are flattened lists, every
  referenced deployment dimension must have one unambiguous observed value.
  A stage predicate cannot hide a canonical finding that also carries prod.
  The live golden suppression is identity-anchored and narrowed to the
  singleton `prod` context, so its hidden-then-expired transition proves the
  real reducer path; a production-finalized prod+stage aggregate stays visible.
  Performance Evidence:
  `docs/internal/evidence/5466-env-scoped-suppression-scope.md` records the
  current-base benchmark and exact stable-sort equivalence proof.
- **Safe-upgrade remediation is advisory-only** —
  `SupplyChainImpactHandler` attaches a `Remediation` block to every finding
  via `BuildSupplyChainImpactRemediation` (issue #595). The block records the
  installed version, source-reported vulnerable range, selected fixed-version
  source, match reason, first patched version, every published fixed-version
  branch, the manifest range preserved from package consumption evidence, a
  tri-state manifest_allows_fix decision (`allowed`, `blocked`, `unknown`),
  the direct/transitive designation, the parent package the caller would need
  to upgrade for transitive findings, the ecosystem the recommendation was
  computed for, an `exact|partial|unknown` confidence label, and a closed
  reason enum (`direct_upgrade_allowed`, `direct_range_blocked`,
  `transitive_parent_upgrade_required`, `no_patched_version`,
  `multiple_patched_branches`, `package_manager_unsupported`,
  `manifest_range_missing`, `manifest_range_malformed`,
  `installed_version_missing`, `installed_version_malformed`).
  `installed_version_missing` fires when the advisory publishes more than
  one fixed-version branch and Eshu has no parseable installed version
  to anchor the branch selector — without that anchor the lowest fix
  across all branches could be a downgrade or unnecessary cross-major
  bump, so the reducer blanks the recommendation rather than committing
  to either branch. `installed_version_malformed` fires whenever the
  observed version is non-empty but fails the ecosystem-specific version
  parser. The reducer delegates manifest allowance and fixed-branch ordering
  to the same ecosystem-aware matcher families used for impact classification:
  npm, Go modules, PyPI, Maven/Gradle, NuGet, Cargo, Composer, RubyGems, and
  vendor-gated RPM, Debian/dpkg, and Alpine/APK OS packages. Debian and
  Alpine recommendations require vendor advisory provenance, parseable distro
  version ordering, a parseable installed OS package version, and one
  source-attributed fixed branch. OS package evidence without that provenance
  still reports `package_manager_unsupported` with structured missing evidence
  such as `advisory_provenance_missing`, `fixed_version_branch_ambiguous`, or
  `version_ordering_unsupported` rather than guessing. The reducer also
  captures `VulnerableRange` from the same provenance observation that
  supplies `RangeSource`, persists it on the canonical finding payload
  (top-level `vulnerable_range` and inside the `remediation` block), and
  decodes it through the read model so list-route callers see the same
  vulnerable range as the explain route. Eshu never auto-opens a pull
  request from this block; remediation is strictly advisory.

  Performance Evidence: remediation runs in-process over the bounded
  per-finding inputs the impact handler already owns (observed version,
  requested range, fixed versions, dependency chain). Per finding the work
  is O(branches) string compares plus one caret/tilde expansion of the
  manifest range, which the existing comparator engine evaluates without
  any extra fact load, queue claim, lease, or canonical write. The reducer
  handler test suite (`go test ./internal/reducer -count=1`) is the
  baseline; the new `supply_chain_impact_remediation_test.go` cases each
  finish in microseconds on the developer fixture set.
  No-Regression Evidence: `BuildSupplyChainImpactRemediation` is the only
  per-finding addition to `appendSupplyChainImpactFinding`; no Postgres,
  Cypher, queue, or lease path changed. The bounded `findings` slice is
  not re-sorted or re-traversed, so the existing handler throughput and
  the `cd go && go test ./internal/reducer -count=1` gate stay green.
  Observability Evidence: `SupplyChainRemediationDecisions`
  (`eshu_dp_supply_chain_remediation_decisions_total`) is registered in
  `internal/telemetry/instruments.go` and emitted from
  `SupplyChainImpactHandler.emitCounters` with the closed-enum
  `outcome` label (confidence: `exact`, `partial`, `unknown`) and
  `reason` label (closed remediation-reason enum). A 3 AM operator can
  watch how often Eshu produces exact upgrade paths versus how many
  findings still need ecosystem support to graduate from `unknown`
  without re-running the reducer.
- **Advisory provenance is preserved** — multi-source CVE and affected_package
  observations for the same advisory identity are consolidated into one
  finding per `(cve_id, package_id)` anchor. `supplyChainImpactProvenance`
  selects severity, fixed-version, and vulnerable-range using documented
  per-ecosystem source priority (vendor advisory beats GLAD/GHSA/OSV/NVD for
  OS package classes; GHSA beats GLAD/OSV/NVD for language ecosystems) and
  records the selected source, every alternate severity, every source-reported
  fixed-version branch with originating source, and per-source advisory IDs
  with update and withdrawal timestamps. Withdrawn advisories are excluded
  from selection but remain visible as observations so operators can explain
  why a vendor or upstream record was skipped.
- **Detection profile is recorded** — every owned-anchor finding is tagged
  with `DetectionProfile` (`precise` or `comprehensive`) before the writer
  persists the row. Precise requires an exact installed-version anchor
  (lockfile, manifest with pinned version, or SBOM component with an
  explicit version) plus an ecosystem-aware exact match. Comprehensive
  covers range-only manifests, SBOM/CPE-derived image paths without an
  exact version, malformed advisory ranges, and missing observed versions.
  Unsupported matcher ecosystems are not finding rows and are reported by
  readiness instead. PyPI exact lockfile matches qualify as precise only after
  the PEP 440 matcher proves the advisory range or known-fixed boundary. The
  tier is persisted alongside the truth
  labels (status, confidence, runtime_reachability) and missing-evidence
  reasons; readers (API, MCP, parity gate) decide which tier they want.

  #5735 added exact-DPKG/APK precise-profile admission on top of this bullet;
  see `docs/internal/evidence/5735-os-package-precise-profile.md`.
- **OS package evidence is vendor-gated** — `vulnerability.os_package`
  rows from RPM-family, Debian dpkg, and Alpine apk snapshots can seed
  supply-chain impact only when the row is vendor-class, carries distro and
  distro-version evidence, includes arch and the source-recorded installed
  version, and its `vendor_advisory_source` matches the selected vendor
  advisory source. Debian and Alpine matching is exact-version only; the
  reducer does not try to compare backport or apk release ordering unless a
  source fact names the exact installed version. Third-party, unknown, and
  ambiguous vendor-origin OS package rows are source warnings only; the
  reducer does not use them as image OS-package impact evidence.

  No-Regression Evidence: `go test ./internal/reducer -run
  'TestBuildSupplyChainImpactFindingsUsesVendor(DPKG|APK|RPM)OSPackageEvidence|TestBuildSupplyChainImpactFindingsRejectsLanguageAdvisoryForDPKGOSPackage|TestBuildSupplyChainImpactFindingsSkipsAmbiguousRPMOSPackageEvidence'
  -count=1` failed for dpkg/apk on `origin/main`, then passed after Red Hat RPM
  EVR, Debian dpkg, and Alpine apk facts joined only matching vendor advisory
  evidence; GHSA language advisory evidence against a Debian package and
  ambiguous-origin rows produced no impact finding.
  No-Observability-Change: this is an in-memory admission change over facts
  already loaded by the supply-chain impact handler. Existing reducer run
  spans, reducer duration metrics, reducer execution counters, durable
  finding payloads/evidence paths, warning facts, and API/MCP readiness
  envelopes remain the operator-visible signals; no new queue domain, graph
  write, route, runtime knob, metric instrument, or metric label was added.
- **Pub parity is hosted-lockfile gated** —
  Pub `pubspec.lock` consumption rows can now produce `affected_exact` and
  `not_affected_known_fixed` findings when OSV Pub advisory ranges or fixed
  versions match the exact hosted `pub.dev` version. `pubspec.yaml` ranges,
  git/path dependencies, private-hosted rows, mismatched lockfile names, and
  dependency overrides remain partial or non-evidence and do not publish
  precise Pub impact.

  No-Regression Evidence: `go test ./internal/reducer -run
  'BuildSupplyChainImpactFindingsMatchesPub' -count=1` proves exact hosted
  Pub lockfile rows admit precise findings while manifest range-only rows keep
  missing installed-version evidence.
- **RubyGems parity is exact-version gated** —
  Ruby Bundler lockfile consumption rows can now produce `affected_exact` and
  `not_affected_known_fixed` findings when a RubyGems advisory range or fixed
  version matches the exact installed version. Git and path Bundler
  dependencies still stop at source evidence because `source_ambiguous=true`
  prevents public RubyGems registry admission, and rows without a proven
  lockfile chain keep `direct_dependency=null` instead of guessing directness.

  No-Regression Evidence: `go test ./internal/reducer -run
  'TestBuildSupplyChainImpactFindings.*RubyGems|TestBuildPackageConsumptionDecisions.*RubyGems'
  -count=1` proves Bundler lockfile exact-version impact, known-fixed
  behavior, four-segment RubyGems versions, dependency-chain propagation, and
  git/path ambiguity rejection. The tests are in-memory reducer fixtures; they
  add no Postgres, graph, queue, worker, or hosted-runtime work.

  No-Observability-Change: RubyGems parity only adds reducer-side version
  comparison for already-admitted package-consumption facts. Operators continue
  to diagnose the path through existing parser-stage timing,
  `reducer_package_consumption_correlation`,
  `reducer_supply_chain_impact_finding`, `match_reason`, `dependency_path`,
  `missing_evidence`, and the supply-chain impact API/MCP readiness envelope.
  No metric instrument, span, log key, queue, graph write, scanner worker,
  route, or runtime knob was added.
- **Provider alerts are dependency-gated before impact admission** —
  `SupplyChainImpactHandler` can seed a `reducer_supply_chain_impact_finding`
  from an open `security_alert.repository_alert` only when active owned
  dependency evidence matches the same repository, package identity, and
  manifest path. Provider-scoped repository IDs are preserved separately as
  provider evidence and resolve to canonical Eshu `repository_id` values only
  when owned dependency evidence proves one unambiguous repository match. The
  finding preserves provider advisory IDs, vulnerable range, patched version,
  severity, dependency path, manifest scope, and missing-evidence reasons.
  Provider-only, stale, ambiguous, dismissed, and fixed alerts do not become
  impact rows.
- **Provider alert reconciliation stays explicit** —
  `SecurityAlertReconciliationHandler` writes
  `reducer_security_alert_reconciliation` facts from
  `security_alert.repository_alert`, package-consumption correlation, active
  manifest dependency evidence, and supply-chain impact facts. It preserves
  provider alert state and Eshu impact state in separate payload fields, keeps
  raw `provider_repository_id` separate from canonical `repository_id`,
  classifies rows as matched, unmatched, stale, dismissed, fixed, or
  provider-only, and explains when a provider alert was not admitted into
  impact truth because owned dependency evidence was missing, stale, or
  ambiguous. Its durable replacement identity uses provider, provider alert id
  or number, provider evidence scope, package id, and advisory ids so
  provider-only rows are replaced by later matched or stale rows for the same
  provider alert instead of remaining active beside them.
- **Package ownership is conservative** —
  `PackageSourceCorrelationHandler` writes ownership candidates from registry
  source hints and package-version publication evidence but leaves
  `canonical_writes=0`; manifest dependency facts are the first admitted
  package consumption truth because they combine registry identity with Git
  source declaration. Package identity alone is enough to schedule the
  correlation pass because popular registry responses may omit source hints.
  Publication fact identity includes source-hint kind, fact ID, and version
  scope so repository and homepage hints with the same URL do not overwrite one
  another.
