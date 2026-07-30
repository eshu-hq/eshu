# Reducer Gotchas — Container Image Identity

Split from `gotchas-supply-chain-and-vulnerabilities.md` (issue #5786) to keep
that file under the repository's 500-line cap. Digest-first identity
admission, the `#5847` not-generation-authoritative writer bug, the
`EvidenceAsOf` fencing-token upsert guard, and the full No-Regression /
No-Observability-Change test evidence for `ContainerImageIdentityHandler`
live here.

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
