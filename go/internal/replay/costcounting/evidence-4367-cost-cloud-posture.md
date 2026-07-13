# Evidence: C-14 (#4367) projection-COST slice — cloud posture domains

## Scope

Six `cost` gaps in `specs/replay-coverage-manifest.v1.yaml`
(`projection:<reducer_domain>|cost`) covered with real R-16 deterministic
cost-counting scenarios, following the approved pilot pattern on this branch
(`55c1456e0`, `evidence-4367-projection-cost-pilot.md`), one per reducer
projection domain:

- `projection:aws_cloud_runtime_drift` (family `aws`) — new scenario
  (`aws_cloud_runtime_drift_cost_test.go`) driving
  `reducer.PostgresAWSCloudRuntimeDriftWriter.WriteAWSCloudRuntimeDriftFindings`
  (the writer `reducer.AWSCloudRuntimeDriftHandler.Handle` calls,
  `go/internal/reducer/aws_cloud_runtime_drift.go:106`, constructed at
  `go/cmd/reducer/wiring_handlers.go:61`). This writer is a Postgres fact
  writer, not a Cypher graph writer, wired over
  `storage/postgres.InstrumentedDB` — the same wrapper
  `go/cmd/reducer/observed_service_wiring.go` `buildObservedReducerService`
  applies to the real `*sql.DB` (`instrumentedDB`) before threading it into
  `buildReducerService` as `database`, which `wiring_handlers.go` receives.
  The primary instrument is `eshu_dp_postgres_query_duration_seconds`, a
  histogram; the assertion reads its observation count (see "Instrument
  selection" below).
- `projection:ec2_instance_node_materialization` (family `ec2_instance_posture`)
  — new scenario (`ec2_instance_node_cost_test.go`) driving
  `storage/cypher.EC2InstanceNodeWriter.WriteEC2InstanceNodes` (constructed at
  `go/cmd/reducer/canonical_graph_writers.go:51`, wrapped by
  `graphowner.NewEC2InstanceGatedWriter` at line 59).
- `projection:rds_posture_materialization` (family `rds_posture`) — new
  scenario (`rds_posture_cost_test.go`) driving
  `storage/cypher.RDSPostureNodeWriter.WriteRDSPostureNodes` (constructed at
  `go/cmd/reducer/canonical_graph_writers.go:53`, wrapped by
  `graphowner.NewRDSPostureLockedWriter` at lines 77-79).
- `projection:s3_external_principal_grant_materialization` (family
  `s3_external_principal_grant`) — new scenario
  (`s3_external_principal_grant_cost_test.go`) driving
  `storage/cypher.S3ExternalPrincipalGrantWriter.WriteS3ExternalPrincipalGrants`
  (constructed unwrapped at `go/cmd/reducer/canonical_graph_writers.go:76`).
- `projection:s3_internet_exposure_materialization` (family `s3_bucket_posture`)
  — new scenario (`s3_internet_exposure_cost_test.go`) driving
  `storage/cypher.S3InternetExposureNodeWriter.WriteS3InternetExposureNodes`
  (constructed at `go/cmd/reducer/canonical_graph_writers.go:56`, wrapped by
  `graphowner.NewS3InternetExposureLockedWriter` at lines 88-90).
- `projection:secrets_iam_trust_chain` (family `secrets_iam`) — new scenario
  (`secrets_iam_graph_cost_test.go`) driving
  `storage/cypher.SecretsIAMGraphWriter.WriteServiceAccountNodes` at the
  writer level only (see "Governance: secrets_iam_trust_chain" below).

All six are claimed; none needed a fake/hand-counted assertion.

## Writer verification (honesty rule a)

For the five node/edge writers (EC2, RDS, S3 external principal grant, S3
internet exposure, secrets/IAM), `go/cmd/reducer/canonical_graph_writers.go`
and `go/cmd/reducer/secrets_iam_graph_wiring.go` construct each raw
`cypher.*Writer` over `neo4jExec` — the parameter `buildReducerService`
(`go/cmd/reducer/main.go:25-37`) receives, which
`go/cmd/reducer/observed_service_wiring.go` `buildObservedReducerService`
already wrapped as `instrumentedNeo4j := &sourcecypher.InstrumentedExecutor{
Inner: neo4jExecutor, Tracer: tracer, Instruments: instruments}` before
calling `buildReducerService`. Three of the five (EC2, RDS, S3 internet
exposure) are further wrapped by a `graphowner` gate
(`EC2InstanceGatedWriter`, `RDSPostureLockedWriter`,
`S3InternetExposureLockedWriter`) that serializes the SAME inner write call
under a Postgres advisory lock — confirmed by reading
`go/internal/graphowner/posture_locked_writers.go` and
`go/internal/graphowner/family_writers.go`, each gate's `Write*` method calls
the wrapped raw-writer function exactly once, unconditionally, with the full
row batch. The gate is a concurrency-safety layer, not a statement-shape
change, so driving the raw `cypher.*Writer` directly (the pilot's established
convention for `CanonicalNodeWriter`/`SemanticEntityWriter`/`EdgeWriter`)
reproduces the identical Cypher statement / instrument shape production
emits.

For `aws_cloud_runtime_drift`, `go/cmd/reducer/wiring_handlers.go:61`
constructs `reducer.PostgresAWSCloudRuntimeDriftWriter{DB: database}`, where
`database` is `buildReducerService`'s first storage parameter — the
`instrumentedDB` `buildObservedReducerService` constructs
(`go/cmd/reducer/observed_service_wiring.go:35-40`,
`StoreName: "reducer"`). `AWSCloudRuntimeDriftHandler.Handle`
(`go/internal/reducer/aws_cloud_runtime_drift.go:77-133`) calls
`h.Writer.WriteAWSCloudRuntimeDriftFindings` exactly once per intent with the
full admitted-candidate slice (line 106), confirming the writer this scenario
drives is the real per-intent production call shape.

## Governance: secrets_iam_trust_chain (honesty rule c)

`secrets_iam_trust_chain` (this domain) and the live graph-projection handler
(`DomainSecretsIAMGraphProjection`, `secrets_iam_graph_projection.go`) are
different reducer intents. The live projection handler is governance-gated:
`go/cmd/reducer/secrets_iam_graph_wiring.go` `secretsIAMGraphProjectionWriter`
returns `nil` (leaving `DomainSecretsIAMGraphProjection` unregistered) unless
`ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED` is explicitly set — ADR
#1314. This scenario calls `cypher.NewSecretsIAMGraphWriter(exec, 0)` and its
`WriteServiceAccountNodes` method directly, exactly as
`evidence-4367-iam-variable-retract.md`'s "Governance decision: secrets/IAM
writer-level exercise (ADR #1314)" section establishes for
`TestReducerSecretsIAMEdgeRetractGraphTruth`: constructing the writer type
directly never touches `cmd/reducer`'s domain registry, the intent-dispatch
loop, or the flag itself — it is mechanically identical to constructing any
other unwired `cypher.*Writer` type in a unit or live test. No flag, default,
or B-12 snapshot value was changed in this slice.

### Representativeness: one Write* family out of nine

The scenario drives `WriteServiceAccountNodes`, one of NINE `Write*` families
the projection handler can call per intent
(`go/internal/reducer/secrets_iam_graph_projection.go:28-36`: four node
families — ServiceAccount, VaultAuthRole, VaultPolicy, SecretMetadataPath —
and five edge families — UsesServiceAccount, AssumesIAMRole,
AuthenticatesVaultRole, UsesVaultPolicy, GrantsSecretRead). One family is
representative of all nine because the cost shape is structurally uniform:

- Every one of the nine families has exactly one fixed-const UNWIND Cypher
  template (`go/internal/storage/cypher/secrets_iam_graph_writer.go`, ADR
  #1314 §5/§6 — no data-driven token is ever interpolated, so there is no
  per-row template split anywhere).
- All nine `Write*` methods are one-line delegates to the SAME
  `SecretsIAMGraphWriter.writeBatched` helper
  (`secrets_iam_graph_writer.go:292-308`): the same
  `buildBatchedStatements(cypher, rows, w.batchSize)` batching, the same
  statement-metadata stamping, the same `dispatch`
  (GroupExecutor-or-sequential) routing through the instrumented executor.

The per-family-call cost invariant this scenario pins — one `WriteX` call
whose rows fit one batch produces exactly one UNWIND statement
(`eshu_dp_neo4j_batches_executed_total` +1), and N separate calls produce N —
is therefore identical across all nine families; asserting one family pins
the shared `writeBatched` shape they all inherit, and an N+1 regression in
any family's caller would trip the same instrument the same way. What one
family does NOT pin is the full per-intent multi-family fan-out (the
handler's node-then-edge sequence over up to nine families plus
`RetractScope`); budgeting that requires driving the governance-gated
projection handler itself, which ADR #1314 forbids without a target-bound
activation decision — the writer-level scope is the deliberate limit of this
claim, mirrored in the manifest comment.

## Instrument selection

The five node/edge writers have no domain-scoped `eshu_dp_*` counter of their
own (unlike `CanonicalNodeWriter.recordAtomicWrite`); like the pilot's
`SemanticEntityWriter` scenario, they are driven through
`storage/cypher.InstrumentedExecutor`, which increments
`eshu_dp_neo4j_batches_executed_total` once per UNWIND-shaped statement (a
statement whose `Parameters` carry a `"rows"` key) on `Execute` or
`ExecuteGroup`. Each of the five writers uses exactly one Cypher template with
no per-row vocabulary split (unlike `SemanticEntityWriter`'s per-label
batching), so two distinct fixture rows are sufficient — no same-key/same-label
requirement applies here.

`aws_cloud_runtime_drift`'s writer persists Postgres facts, not graph nodes,
so no `storage/cypher` instrument applies. `storage/postgres.InstrumentedDB`
records `eshu_dp_postgres_query_duration_seconds`, a duration histogram
(`Float64Histogram`, `go/internal/telemetry/instruments.go:3464-3472`), on
every `ExecContext` call — one call per admitted candidate
(`aws_cloud_runtime_drift_writer.go` loops per candidate, no batching). The
test reads the histogram's total observation count (`Count` field on each
`metricdata.HistogramDataPoint`) via a new `collectHistogramCount` helper,
analogous to `collectCounter` but for a `Sum`-shaped vs. `Histogram`-shaped
otel instrument. Each `ExecContext` call records exactly one observation, so
the observation count is a genuine per-call cost signal recorded by
production instrumentation, not a hand-counted call slice.

## N+1 control shape (aws_cloud_runtime_drift deviation)

The five node/edge writers use the pilot's standard N+1 shape: call the
writer once per fixture row instead of once for the whole batch (2 rows -> 1
batch positive, 2 batches N+1).

`aws_cloud_runtime_drift` cannot use that shape: `PostgresAWSCloudRuntimeDriftWriter`
has no UNWIND-style batching to break — it always issues exactly one
`ExecContext` per candidate, so N calls with 1 candidate each produce the
IDENTICAL observation count as 1 call with N candidates (both are `N`). This
was confirmed empirically during the RED discovery run: the positive scenario
and a "call once per candidate" variant both recorded 2 observations,
proving that shape is a no-op negative control for this writer. The genuine
regression class a per-row Postgres writer is exposed to is DUPLICATE
invocation — a retry without an idempotency check, or an
evidence-loader/candidate-dedup bug that admits the same candidate set twice —
doubling Postgres write cost for identical logical work. The N+1 control
instead calls `WriteAWSCloudRuntimeDriftFindings` TWICE with the SAME
2-candidate fixture set, producing 4 observations against a budget of 2.

## RED -> GREEN evidence

Commands run from the worktree root with `GOCACHE=$(pwd)/.gocache`:

```
cd go && go test ./internal/replay/costcounting/... \
  -run 'TestCostBudget_EC2InstanceNodeMaterialization$|TestCostBudget_RDSPostureMaterialization$|TestCostBudget_S3ExternalPrincipalGrantMaterialization$|TestCostBudget_S3InternetExposureMaterialization$|TestCostBudget_SecretsIAMTrustChain$|TestCostBudget_AWSCloudRuntimeDrift$' -v -count=1
```

1. **RED (placeholder budget, discovery run).** All six `.cost-budget.json`
   files started with `999999` placeholder budgets. The positive tests logged
   the real observed counts:
   - `eshu_dp_neo4j_batches_executed_total=1`, `statements_executed=1` for
     EC2, RDS, S3 external principal grant, S3 internet exposure, and
     secrets/IAM (all five node/edge writers, two distinct rows batching into
     one UNWIND statement each).
   - `eshu_dp_postgres_query_duration_seconds` observations `=2`,
     `statements_executed=2` for `aws_cloud_runtime_drift` (two admitted
     candidates, one `ExecContext` each).
   Running the N+1 negative controls at that point FAILED with "did NOT
   exceed budget 999999" for all six — the loose placeholder proved nothing
   yet, the expected RED state for a not-yet-tightened budget.
2. **Design correction (RED, aws_cloud_runtime_drift's per-row N+1 shape was
   a no-op).** The initial N+1 control used the standard "call once per
   candidate" shape; it recorded the SAME 2 observations as the positive
   scenario, proving the control was a no-op — `PostgresAWSCloudRuntimeDriftWriter`
   has no batching to regress out of (see "N+1 control shape" above). Fixed
   by changing the control to call the writer twice with the full 2-candidate
   set (duplicate invocation), which now genuinely proves 4 observations
   exceeds the 2-observation budget.
3. **Exact budgets set from the observed GREEN counts** (see the six
   `.cost-budget.json` files' `description` fields for the per-scenario
   accounting).
4. **GREEN — positive + N+1 against the tightened budgets:**

```
=== NAME  TestCostBudget_EC2InstanceNodeMaterialization
    eshu_dp_neo4j_batches_executed_total=1 (budget=1) statements_executed=1 (budget=1)
--- PASS: TestCostBudget_EC2InstanceNodeMaterialization (0.00s)
=== NAME  TestCostBudget_EC2InstanceNodeMaterialization_N1_ExceedsBudget
    N+1 negative control passed: eshu_dp_neo4j_batches_executed_total = 2 > budget 1 (N=2 rows, scenario=ec2-instance-node-materialization)
--- PASS: TestCostBudget_EC2InstanceNodeMaterialization_N1_ExceedsBudget (0.00s)
=== NAME  TestCostBudget_RDSPostureMaterialization
    eshu_dp_neo4j_batches_executed_total=1 (budget=1) statements_executed=1 (budget=1)
--- PASS: TestCostBudget_RDSPostureMaterialization (0.00s)
=== NAME  TestCostBudget_RDSPostureMaterialization_N1_ExceedsBudget
    N+1 negative control passed: eshu_dp_neo4j_batches_executed_total = 2 > budget 1 (N=2 rows, scenario=rds-posture-materialization)
--- PASS: TestCostBudget_RDSPostureMaterialization_N1_ExceedsBudget (0.00s)
=== NAME  TestCostBudget_S3ExternalPrincipalGrantMaterialization
    eshu_dp_neo4j_batches_executed_total=1 (budget=1) statements_executed=1 (budget=1)
--- PASS: TestCostBudget_S3ExternalPrincipalGrantMaterialization (0.00s)
=== NAME  TestCostBudget_S3ExternalPrincipalGrantMaterialization_N1_ExceedsBudget
    N+1 negative control passed: eshu_dp_neo4j_batches_executed_total = 2 > budget 1 (N=2 rows, scenario=s3-external-principal-grant-materialization)
--- PASS: TestCostBudget_S3ExternalPrincipalGrantMaterialization_N1_ExceedsBudget (0.00s)
=== NAME  TestCostBudget_S3InternetExposureMaterialization
    eshu_dp_neo4j_batches_executed_total=1 (budget=1) statements_executed=1 (budget=1)
--- PASS: TestCostBudget_S3InternetExposureMaterialization (0.00s)
=== NAME  TestCostBudget_S3InternetExposureMaterialization_N1_ExceedsBudget
    N+1 negative control passed: eshu_dp_neo4j_batches_executed_total = 2 > budget 1 (N=2 rows, scenario=s3-internet-exposure-materialization)
--- PASS: TestCostBudget_S3InternetExposureMaterialization_N1_ExceedsBudget (0.00s)
=== NAME  TestCostBudget_SecretsIAMTrustChain
    eshu_dp_neo4j_batches_executed_total=1 (budget=1) statements_executed=1 (budget=1)
--- PASS: TestCostBudget_SecretsIAMTrustChain (0.00s)
=== NAME  TestCostBudget_SecretsIAMTrustChain_N1_ExceedsBudget
    N+1 negative control passed: eshu_dp_neo4j_batches_executed_total = 2 > budget 1 (N=2 rows, scenario=secrets-iam-trust-chain)
--- PASS: TestCostBudget_SecretsIAMTrustChain_N1_ExceedsBudget (0.00s)
=== NAME  TestCostBudget_AWSCloudRuntimeDrift
    eshu_dp_postgres_query_duration_seconds_observations=2 (budget=2) statements_executed=2 (budget=2)
--- PASS: TestCostBudget_AWSCloudRuntimeDrift (0.00s)
=== NAME  TestCostBudget_AWSCloudRuntimeDrift_N1_ExceedsBudget
    N+1 negative control passed: eshu_dp_postgres_query_duration_seconds observations = 4 > budget 2 (N=2 duplicate invocations of a 2-candidate set, scenario=aws-cloud-runtime-drift)
--- PASS: TestCostBudget_AWSCloudRuntimeDrift_N1_ExceedsBudget (0.00s)
PASS
```

5. **False-green guard proof.** Temporarily set each of the six new budgets'
   primary key to `0` and reran the six positive tests: all six failed with
   "exceeds budget 0: algorithmic regression detected" (the observed values
   of `1` or `2` are nonzero, so this proves the "budget too tight trips the
   gate" direction — the paired "budget=0 means instrument isn't recording"
   guard in the test code is the same `if x == 0 { t.Fatal(...) }` shape
   already proven by the pilot's canonical-writer test). Budgets restored to
   the exact GREEN values before committing.

## Commands run (full verification)

```
cd go && gofumpt -l ./internal/replay/costcounting/*.go   # no output: already formatted
cd go && go vet ./internal/replay/costcounting/
cd go && go test ./internal/replay/costcounting/ -race -count=1
cd go && go test ./internal/replaycoverage/ ./cmd/replay-coverage-gate/ -count=1
cd go && go test ./cmd/replay-coverage-gate/ -update-dashboard -count=1
bash scripts/verify-replay-coverage-gate.sh --blocking
ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main bash scripts/verify-performance-evidence.sh
git diff --check
```

## Gate summary

`replay-coverage-report.json` (blocking run):

- `"blocking": true`
- `projection` coverage: `5/27` (18.52%) -> `11/27` (40.74%)
- `TOTAL` gaps: `23` -> `17`
- `projection:aws_cloud_runtime_drift|cost`,
  `projection:ec2_instance_node_materialization|cost`,
  `projection:rds_posture_materialization|cost`,
  `projection:s3_external_principal_grant_materialization|cost`,
  `projection:s3_internet_exposure_materialization|cost`, and
  `projection:secrets_iam_trust_chain|cost` all move from `[WARN] uncovered`
  to covered.

## No-Regression Evidence:

Test-only, spec, and doc additions; no production code paths changed (no
`cmd/reducer` wiring file, no `storage/cypher` or `storage/postgres` source
file, and no `reducer` package source file was edited — every writer and
handler this slice drives is exercised exactly as it already exists on
`main`). `go test ./internal/replay/costcounting/ -race -count=1` (18/18
pass, including the 6 pre-existing pilot-plus-canonical tests unchanged) and
`go test ./internal/replaycoverage/ ./cmd/replay-coverage-gate/ -count=1`
(gate logic unit tests) both green.
`scripts/verify-replay-coverage-gate.sh --blocking` passes with
`"blocking": true` and the six targeted gaps resolved; the remaining gaps are
unrelated pre-existing C-14 backlog, not a regression introduced here.

## No-Observability-Change:

No new `eshu_dp_*` instrument was added and no production instrumentation
call site changed. Every scenario in this slice reads PRE-EXISTING production
instruments (`eshu_dp_neo4j_batches_executed_total`,
`eshu_dp_postgres_query_duration_seconds`) that `storage/cypher.InstrumentedExecutor`
and `storage/postgres.InstrumentedDB` already record in production; this
change only adds credential-free test coverage asserting their values, per
`telemetry-coverage-discipline`'s `No-Observability-Change:` marker
convention for changes that touch no instrument definition or production
emission call site.
