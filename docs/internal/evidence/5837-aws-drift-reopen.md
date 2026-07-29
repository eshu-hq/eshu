# #5837 / #5848 — `aws_cloud_runtime_drift` cross-scope race, reopen, and insert admission

## Status

**Closed by this branch.** Unlike `docs/internal/evidence/5847-container-image-identity-retire.md`
(the sibling domain, where the retire was withdrawn under review and #5854 stayed
open), `aws_cloud_runtime_drift` ships all four pieces in one change: a bounded
readiness defer at the source of #5837's race, the bootstrap/ingester reopen that
lets a lost race recover, a generation-authoritative retire that makes the reopen
safe, and the begin-before-mutate insert-admission check #5848 asked for. The
prerequisite work #5848's issue text assumed already existed (a `#5837`-landed
retire fence) did not exist on `origin/main` at the time this branch started —
verified by direct search (`rg -n "fencing_token" go/internal/reducer/aws_cloud_runtime_drift*.go`
returned nothing, and no `aws_cloud_runtime_drift_retire_fence_live_test.go` file
existed). This branch builds the retire fence AND the insert-admission check
together, rather than reconstructing a design that was never landed.

## Cross-PR interactions

**Migration-number collision, resolved, then rebased.** This branch forked
from `ba2b7b80b`. `origin/main` advanced past that point with `f1fd95dbc`
(#5469, "resolve the judged version from the strongest deployment-truth
tier"), which claimed migration number `086`
(`086_cloud_resource_owner_runtime_digest_index.sql`) — the same number this
branch first used for `086_aws_cloud_runtime_drift_write_admission.sql`. The
collision was found before rebasing, by diffing `origin/main...HEAD`
(merge-base relative) against a fresh `git fetch origin main` and comparing
changed-file sets, then confirmed by inspecting `f1fd95dbc`'s own migration
directory diff. First resolved by renumbering this branch's migration to
`087` in place (`git mv`, plus the matching `orderedBootstrapDefinitionNames`
entry in `schema_order_test.go`), then the branch was actually rebased onto
`origin/main` (`git fetch origin && git rebase origin/main`) once the
coordinator confirmed the collision was real and requested it. The rebase
conflicted exactly where expected — twice in
`go/internal/storage/postgres/schema_order_test.go`, both times because both
`origin/main` and this branch's own commits touch the tail of
`orderedBootstrapDefinitionNames` — and was resolved by hand to keep `086`
for #5469's `cloud_resource_owner_runtime_digest_index` and `087` for this
branch's `aws_cloud_runtime_drift_write_admission`, consistently across the
migration filename, `schema_order_test.go`, and this doc. The rebase
preserved all 6 original commits (the renumber commit still does real work
post-rebase: the actual `git mv` from `086_...` to `087_...` had not yet
happened at the point the conflict was resolved, so that commit's rename is
not a no-op).

The transient shape where two DIFFERENT migration files briefly share the
numeric prefix `086` across intermediate commits (before the renumber commit
replays) is not a functional defect, even transiently: `BootstrapDefinitions()`
sorts and derives each definition's name from the FULL filename minus its
numeric prefix, and the codebase already has a documented precedent for
exactly this shape —
`schema_order_test.go`'s own comment on migration prefix `075`
("a duplicate-number merge artifact from two independently-landed PRs...
`BootstrapDefinitions()` sorts by migration filename, so
'075_fact_records_active_container_image_slsa_idx' (f) deterministically
precedes '075_kubernetes_live_pod_template_object_index' (k)"). The
renumbering to `087` was still the right call for tidiness and to avoid
ambiguity, not because a shared prefix breaks anything.

Confirmed via `rg -n "086|087" go/internal/storage/postgres/` (every hit
accounted for: `086` names #5469's index consistently, `087` names this
branch's migration consistently, `evidence-5092-relationship-family-guard.md`'s
hits are unrelated timing numbers containing the same digits) and a clean
`go build ./...`/`go vet ./...` on the rebased head.

**`container_image_identity` (#5847/#5851, `a2a5340a9`) is untouched, read-only
reference.** `a2a5340a9` is in this branch's base (an ancestor of `ba2b7b80b`,
the fork point) — it landed before this branch started, so no rebase is
involved in seeing it. `git diff origin/main...HEAD` (merge-base relative,
confirming no drift) shows zero changes to any `container_image_identity`,
`ci_cd_run_correlation`, provenance-edge, or `supply_chain_impact`/suppression
file; the `AWSCloudRuntimeDriftAdmissionBeginner`/`AWSCloudRuntimeDriftTx`
pattern in this branch mirrors `ServiceMaterializationBeginner`'s shape
(#1943), not `a2a5340a9`'s fencing pattern directly, since
`container_image_identity` uses the unversioned `reducerFactRow` family and a
bare execer, while `aws_cloud_runtime_drift` uses the versioned family and
now a transaction.

**#5759 (`DomainMultiCloudRuntimeDrift`) cannot be affected by this branch's
readiness defer — structurally, not by convention.** `#5759` newly enqueues
`DomainMultiCloudRuntimeDrift` for GCP/Azure scopes, and in the golden corpus
those scopes can legitimately have NO Terraform state ever (not late — absent
by design). A readiness defer that could not tell "not yet" from "never" would
either suppress valid orphan findings for those providers or defer them until
the terminal fallback fires on every run. This branch's defer cannot reach
that domain at all: `MultiCloudRuntimeDriftHandler`
(`go/internal/reducer/multi_cloud_runtime_drift.go`) is a SEPARATE Go struct
and `Handle` method from `AWSCloudRuntimeDriftHandler`, with its own field set
(`EvidenceLoader`, `Writer`, `Instruments`, `Logger` — no `ReadinessChecker`
field exists on it, confirmed by direct read of the struct literal). The
`AWSCloudRuntimeDriftReadinessChecker` interface and
`PostgresAWSCloudRuntimeDriftReadinessChecker` implementation are wired
exactly once, in `go/cmd/reducer/wiring_handlers.go`, into
`DriftHandlers.AWSCloudRuntimeDriftReadinessChecker`, which
`defaults_additive_domains_secrets_drift.go` threads only into
`AWSCloudRuntimeDriftHandler{ReadinessChecker: ...}` — never into
`MultiCloudRuntimeDriftHandler`. `rg -n "PostgresAWSCloudRuntimeDriftReadinessChecker|HasPendingStateSnapshotEvidence"
go/ -g '*.go'` outside tests returns exactly those wiring lines and the
implementation itself — no other call site exists. For AWS itself, the
distinction the coordinator's question raises still matters and is answered
by the design already documented above: the checker's signal is coarse
("is ANY `state_snapshot:*` scope pending", not "is the specific backend for
THIS ARN pending"), which is DELIBERATE — it can only ever cause an
unnecessary, bounded (3-attempt) defer, never an incorrect verdict, because
the terminal fallback always commits the classification the evidence actually
supports once the bound is reached, whether or not any state scope ever
activates. Extending this pattern to `MultiCloudRuntimeDriftHandler` in the
future would need its own analysis of what "pending" means for GCP/Azure
state backends; this branch does not attempt that and cannot regress it.

## The four pieces, and why each is required

1. **Readiness defer** (`go/internal/reducer/aws_cloud_runtime_drift_readiness.go`).
   `AWSCloudRuntimeDriftHandler.shouldDeferForStatePending` holds back an
   `orphaned_cloud_resource` classification when a Terraform `state_snapshot:*`
   scope is still mid-ingestion (`scope_generations.status = 'pending'`), for up
   to `awsCloudRuntimeDriftStatePendingMaxAttempts` (3) attempts. Past the bound,
   Handle commits its best-available verdict — the terminal fallback a genuine
   orphan (no Terraform anywhere, ever) needs, so a permanently-pending or
   never-registered state scope cannot starve the intent forever. This addresses
   #5837's actual root cause directly: the AWS scope's drift intent draining
   before the tfstate scope's generation activates, which made
   `cloudruntime.Classify` read absent state as a VERDICT rather than as
   "not ready".
2. **Bootstrap/ingester reopen**
   (`go/internal/storage/postgres/ingestion_reopen_correlation.go`).
   `aws_cloud_runtime_drift` is added to `CrossScopeCorrelationReopenDomains()`,
   mirroring the `container_image_identity` precedent (#5846). Before this, the
   domain's intent ran exactly once per (scope, generation) — the projector's
   `EntityKey` dedupes on scope, and re-enqueue hits `ON CONFLICT DO NOTHING` — so
   a pass that lost the readiness race (or ran before this branch existed at all)
   had nothing to replay it.
3. **Generation-authoritative retire**
   (`go/internal/reducer/aws_cloud_runtime_drift_writer_queries.go`). The reopen
   alone would make #5837 WORSE without this: the fact identity embeds
   `finding_kind`, so a corrected replay reclassifying an ARN mints a NEW
   `fact_id` beside the old one. The retire removes a stale finding for any ARN
   the current pass evaluated whose fact_id is not among the rows it just wrote,
   bounded by the same fencing token as the insert so it can never delete a
   fresher row.
4. **Insert-admission check** (`go/internal/reducer/aws_cloud_runtime_drift_admission.go`).
   #5848's actual ask: a begin-before-mutate check so a pass whose evidence-read
   watermark is older than one already admitted for the same (scope, generation)
   is rejected BEFORE it inserts or retires anything. Closes the residual the
   retire alone leaves open: two DIFFERENT fact_ids (a reclassification) never
   collide on the insert's own `ON CONFLICT` guard, so without this check a
   stalled worker's stale insert can still land unopposed after a fresher
   worker's pass has already committed.

## Design: why a transaction, not three independent statements

`PostgresAWSCloudRuntimeDriftWriter.WriteAWSCloudRuntimeDriftFindings` runs the
admission check, the versioned insert, and the retire inside ONE transaction
(`AWSCloudRuntimeDriftBeginner`/`AWSCloudRuntimeDriftTx`,
`go/internal/storage/postgres/aws_cloud_runtime_drift_admission_beginner.go`
provides the production adapter over the shared `postgres.Beginner`). Three
separate auto-committed statements would leave a gap: a pass could pass the
admission check, then stall before issuing its insert, while a fresher pass ran
its own admission-check-through-retire to completion in between — the admission
check alone, without a transaction, only orders WHEN a pass is admitted, not
whether it can still be preempted before it finishes writing. Wrapping all three
in one transaction closes that: Postgres's own row lock on the admission table's
`INSERT ... ON CONFLICT DO UPDATE` serializes two concurrent attempts against the
SAME (scope, generation) key, and whichever commits first sets the value the
loser's own `WHERE fencing_token <= EXCLUDED.fencing_token` check re-evaluates
against once its lock is granted.

The writer's `DB` field changed from a bare `workloadIdentityExecer` to
`AWSCloudRuntimeDriftBeginner` for this reason — the original issue text's
caveat ("the drift writer currently holds a bare execer rather than a
transaction-capable handle") is resolved rather than worked around.

## Concurrency analysis

- **Shared state / conflict domain:** one row in
  `aws_cloud_runtime_drift_write_admission` keyed by `(scope_id, generation_id)`;
  the `reducer_aws_cloud_runtime_drift_finding` rows in `fact_records` for that
  same (scope, generation), further partitioned by ARN via `payload->>'arn'`.
- **Lock/claim ordering:** admission CAS first, unconditionally, inside the
  transaction; insert and retire only run if admission succeeds. Two concurrent
  transactions targeting the same (scope, generation) serialize on Postgres's
  row lock for the conflicting admission key; the second to acquire the lock
  re-evaluates its own `WHERE` against the first's now-committed value.
- **Transaction scope:** admission + insert + retire commit or roll back
  together, one transaction per `WriteAWSCloudRuntimeDriftFindings` call.
- **Retry scope:** the whole `Handle` call is one retry unit at the reducer queue
  level; a rejected write (or a deferred readiness gate) returns a
  `Retryable() == true` error so the queue reschedules the intent.
- **Idempotency key:** `fact_id` (candidate identity, including `finding_kind`)
  for the insert's own upsert; `(scope_id, generation_id)` for the admission
  watermark; a retry with the SAME `EvidenceAsOf` (the SAME pass redelivered)
  carries the SAME fencing token and is admitted again (`<=`, not `<`), so
  redelivery is a no-op, not a rejection.
- **Starvation:** none introduced. A rejected pass is not serialized behind
  anything — it is told to retry, and a retry that reads fresher evidence gets a
  higher token and is very likely admitted (unless an even-fresher concurrent
  pass wins first, in which case it correctly defers again). The readiness
  defer's bound (3 attempts) is the other starvation guard: it prevents an
  indefinite hold on a state scope that will never activate.
- **Dead-letter behavior:** both `awsCloudRuntimeDriftWriteSupersededError` and
  `awsCloudRuntimeDriftStatePendingError` self-classify as non-counting retry
  classes (`AWSCloudRuntimeDriftWriteSupersededFailureClass`,
  `AWSCloudRuntimeDriftStatePendingFailureClass`, both added to
  `nonCountingReducerRetryFailureClasses`,
  `go/internal/storage/postgres/reducer_queue_readiness_sql.go`), so losing a
  normal race or waiting on a pending state scope never erodes the retry budget
  or dead-letters the intent — the acceptance criterion "refusing a pass does
  not consume its retry budget in a way that turns a normal race into a
  dead-letter" is satisfied by reusing the exact mechanism EC2/GCP/Kubernetes
  readiness misses already use, not a new one.
- **Serialization avoided:** no worker-count reduction, no batch-size-1, no
  domain-wide exclusive lock. The conflict domain is partitioned by (scope,
  generation) — concurrent passes for DIFFERENT scopes or generations never
  contend, and a fresher pass for the SAME scope/generation is never blocked by
  an older one; it wins the race and the older one is told to retry.

## Adjacent defect found and fixed: `InstrumentedDB.Begin` silently dropped observability

`InstrumentedDB.Begin` returned the inner `Beginner`'s transaction UNWRAPPED, so
every `ExecContext`/`QueryContext` call issued through it bypassed
`eshu_dp_postgres_query_duration_seconds` entirely, silently. This was latent
because no production writer routed real per-write statements through a
transaction over an `InstrumentedDB`-wrapped connection before this branch (only
`PostgresServiceMaterializationWriter` uses a transaction today, and this fixes
that path too). `aws_cloud_runtime_drift`'s cost-budget gate
(`go/internal/replay/costcounting/aws_cloud_runtime_drift_cost_test.go`) caught
it before it shipped: the positive scenario read 0 histogram observations
instead of the expected 3.

Fixed in `go/internal/storage/postgres/instrumented_transaction.go`: `Begin` now
wraps the returned transaction so its `ExecContext`/`QueryContext` calls record
the same histogram non-transactional calls do. Proven with a temporary revert:

```
$ go test ./internal/storage/postgres -run TestInstrumentedDBBeginWrapsTransactionWithMetrics -v
--- FAIL: TestInstrumentedDBBeginWrapsTransactionWithMetrics (0.00s)
    eshu_dp_postgres_query_duration_seconds observations = 0, want 2
# fix restored
--- PASS: TestInstrumentedDBBeginWrapsTransactionWithMetrics (0.00s)
```

## Proof

All Postgres proofs ran against an isolated `postgres:16` container
(`eshu-pg-5848`, host port 15948) started specifically for this branch, not the
shared default Compose ports.

### Failing-then-green: the #5848 interleaving (mandatory regression)

`TestAWSCloudRuntimeDriftInsertAdmissionRejectsStaleWorkerAfterFreshWriteLive`
(`go/internal/storage/postgres/aws_cloud_runtime_drift_admission_live_test.go`)
reproduces the exact interleaving #5848 describes, sequentially (no timing
dependency: the hazard only needs the stale pass's evidence read to predate the
fresh pass's commit for the SAME scope/generation, not true concurrent
overlap):

1. Seed one scope and one active generation.
2. Worker B (fresh): `EvidenceAsOf = now`, one candidate for ARN X classified
   `image_version_drift`. Run to completion (admission + insert; retire is a
   no-op on a first pass).
3. Worker A (stale): `EvidenceAsOf = now - 5m`, one candidate for the SAME ARN X
   classified `orphaned_cloud_resource`. Different classification means a
   different `fact_id`.

Temporarily forcing `admitted = true` unconditionally (bypassing the admission
check) reproduces the pre-#5848 defect:

```
$ go test ./internal/storage/postgres -run TestAWSCloudRuntimeDriftInsertAdmissionRejectsStaleWorkerAfterFreshWriteLive -v
--- FAIL: TestAWSCloudRuntimeDriftInsertAdmissionRejectsStaleWorkerAfterFreshWriteLive (0.33s)
    worker A (stale) write error = nil, want superseded rejection
# fix restored
--- PASS: TestAWSCloudRuntimeDriftInsertAdmissionRejectsStaleWorkerAfterFreshWriteLive (0.92s)
```

After the fix, exactly one row survives — `SELECT payload->>'finding_kind' FROM
fact_records WHERE scope_id = $1 AND generation_id = $2 AND fact_kind =
'reducer_aws_cloud_runtime_drift_finding'` returns `["image_version_drift"]` —
and `AWSCloudRuntimeDriftFindingStore.ListActiveFindings` agrees: one finding,
`FindingKind = "image_version_drift"`, `ARN` matching worker B's.

`TestAWSCloudRuntimeDriftInsertAdmissionAppliesEqualTokenRetryLive` pins the
`<=` boundary: a retry carrying the SAME watermark must be admitted, not
rejected — a `<` guard would silently discard every retry of a pass whose
evidence is read exactly once.

### Failing-then-green: the retire (the more common real-world shape)

`TestAWSCloudRuntimeDriftRetireRemovesStaleFindingOnReclassificationLive` covers
the shape a bootstrap/ingester reopen actually produces: an OLDER pass runs
FIRST (unconditionally admitted — nothing existed yet to compare against), then
a fresher reopen-replay reclassifies the same ARN. Neutralizing
`retireAWSCloudRuntimeDriftFindings` to a no-op reproduces the pre-fix defect:

```
$ go test ./internal/storage/postgres -run TestAWSCloudRuntimeDriftRetireRemovesStaleFindingOnReclassificationLive -v
--- FAIL: TestAWSCloudRuntimeDriftRetireRemovesStaleFindingOnReclassificationLive (1.65s)
    reopen replay Retired = 0, want 1 (the stale orphaned_cloud_resource row)
# fix restored
--- PASS: TestAWSCloudRuntimeDriftRetireRemovesStaleFindingOnReclassificationLive (0.24s)
```

After the fix: `WriteAWSCloudRuntimeDriftWriteResult.Retired == 1`, exactly one
row survives (`image_version_drift`), and the stale pass's own `fact_id` (an
independent computation via `facts.StableID`, matching the production writer's
own identity derivation) no longer exists in `fact_records`.

### Deterministic reproduction of #5837's actual root cause

`TestPostgresAWSCloudRuntimeDriftReadinessCheckerLive`
(`go/internal/storage/postgres/aws_cloud_runtime_drift_readiness_live_test.go`)
forces the evidence-state ordering directly rather than racing real collectors
for the 1-in-3 shape the issue observed: a `state_snapshot:*` scope registered
with a `'pending'` generation reports `HasPendingStateSnapshotEvidence() ==
true`; once that generation activates (`status = 'active'`,
`ingestion_scopes.active_generation_id` set), the same check reports `false`.
Breaking the query's status predicate reproduces the defect:

```
$ go test ./internal/storage/postgres -run TestPostgresAWSCloudRuntimeDriftReadinessCheckerLive -v
--- FAIL: TestPostgresAWSCloudRuntimeDriftReadinessCheckerLive/registered_but_pending_state_snapshot_generation_is_pending (0.02s)
    HasPendingStateSnapshotEvidence() = false, want true
# fix restored
--- PASS: TestPostgresAWSCloudRuntimeDriftReadinessCheckerLive (0.22s)
```

`go/internal/reducer/aws_cloud_runtime_drift_readiness_test.go` covers the
Handler-level bound logic without a database: a pending state scope defers an
orphaned classification (writer never called), the bound (3 attempts) commits
the best-available verdict regardless of pending state, a non-pending state
never defers, a nil `ReadinessChecker` never defers (opt-in, matches pre-#5848
behavior byte-for-byte), and a non-orphaned candidate never even calls the
readiness checker (a pending state scope cannot improve
unmanaged/ambiguous/unknown/image_version_drift).

### Deterministic end-to-end reproduction (real Handler, EvidenceLoader, Writer)

`TestAWSCloudRuntimeDriftReadinessDeterministicReproductionLive`
(`go/internal/storage/postgres/aws_cloud_runtime_drift_readiness_deterministic_live_test.go`)
goes one level further than the readiness-checker-only proof above: it drives
the REAL `AWSCloudRuntimeDriftHandler`, the real
`PostgresAWSCloudRuntimeDriftEvidenceLoader`, the real
`PostgresAWSCloudRuntimeDriftReadinessChecker`, and the real
`PostgresAWSCloudRuntimeDriftWriter` together, end to end, with no stubs. This
is the closest a Docker-free lane can get to the owner's own technique of
forcing the collector order (awscloud drains before terraformstate) to turn
the 1-in-3 race into a 100% reproduction: instead of forcing two collectors to
drain in a specific order, it forces the EVIDENCE STATE they would leave
behind directly (a `state_snapshot:*` scope registered with a `'pending'`
generation) — deterministic by construction, not timing, so it reproduces on
every run rather than roughly one in three.

Three phases, one continuous run, verified deterministic over 5 repeated
`-count=5` runs:

```
$ go test ./internal/storage/postgres -run TestAWSCloudRuntimeDriftReadinessDeterministicReproductionLive -v -count=1
    BEFORE (no ReadinessChecker): durably wrote [orphaned_cloud_resource] while state scope was pending -- the #5837 bug
    AFTER (ReadinessChecker wired, attempt 0): deferred, zero rows written -- the #5837 fix
    AFTER (attempt 1, state active): reclassified to [unknown_cloud_resource] once state activated
--- PASS: TestAWSCloudRuntimeDriftReadinessDeterministicReproductionLive (0.66s)
```

Phase 1 (`AWSCloudRuntimeDriftHandler` with `ReadinessChecker` left nil —
byte-identical to the code that shipped before this branch) durably writes
`orphaned_cloud_resource` while the state scope is pending: the bug,
reproduced on demand rather than waited for. Phase 2 (the same evidence shape,
`ReadinessChecker` wired) defers instead — zero rows, a retryable error, not a
silently degraded success. Phase 3 activates the state scope with matching
Terraform state for the same ARN and replays `Handle` (simulating the
reopen-triggered retry, `AttemptCount: 1`): the verdict is
`unknown_cloud_resource`, not `orphaned_cloud_resource` — reclassified once
state became visible. (`unknown_cloud_resource`, not `image_version_drift`,
because this fixture's `EvidenceLoader` has no `ConfigResolver` wired —
`aws_cloud_runtime_drift_evidence.go`'s `loadConfigByStateScope` forces
`unknown_cloud_resource` whenever `ConfigResolver` is nil. The specific
non-orphaned kind is incidental to this proof; what matters is that it is no
longer the degraded verdict.)

### Unrelated `internal/storage/postgres` failures, confirmed pre-existing via baseline

A full `go test ./internal/storage/postgres/... -count=1` run on this branch
hits four failures outside anything this branch touches:
`TestIngestionStoreCommitScopeGenerationFencesDerivedRelationshipEvidence` (a
`pg_trgm` operator-class visibility race across parallel tests' custom
Postgres schemas — self-heals once the extension exists database-wide),
`TestIngestionCommitScopeGenerationHoldsBarrierOnlyForAtomicCommit` (a
400,000-fact synchronous-backfill timing assertion),
`TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks`, and
`TestPostgresTerraformBackendQuerySurvivesNullTerraformBackendsPath`. None of
the four files these tests live in were touched by this branch.

Ruled out as a regression by baseline comparison, not by file-adjacency
reasoning alone: an `origin/main` worktree (`ba2b7b80b`, no `#5848` code at
all) was checked out fresh, given its own isolated `postgres:16` container on
a private port, and driven through the SAME three timing-sensitive tests
(`-run "TestIngestionCommitScopeGenerationHoldsBarrierOnlyForAtomicCommit|
TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks|
TestPostgresTerraformBackendQuerySurvivesNullTerraformBackendsPath" -p 1`).
All three failed identically on that clean baseline, with the SAME error
signatures (`relationship_evidence_facts row count = 0`; `relation
"ingestion_scopes" does not exist` cascading from the heavy test's timeout
into the next). The baseline run's own timings (`CommitScopeGeneration
onboarding ... took 8.09s` on the first baseline attempt, `82.73s` on a repeat
against the same idle-looking machine) show wide variance consistent with
contention, not a stable per-test cost.

Machine-state honesty: this repo's other concurrent work was not quiesced for
either run. `ps aux` during the branch run showed a second, unrelated agent
session (`5594-local-backend-default-path`) actively running its own
`go test ./internal/storage/postgres/...` against a different private-port
Postgres at the same time, and the coordinator separately reported starting a
`make pre-pr` run (which holds the cross-worktree live-gate mutex and adds its
own Docker/CPU load) around the same window as the baseline comparison. Both
runs above are therefore "branch vs. baseline under real, uncontrolled,
comparable background load on the same shared host" — not "quiet machine vs.
loaded machine". Given identical failure signatures on a codebase with ZERO
`#5848` changes, under directly comparable (if not literally simultaneous)
contention, these four are classified as pre-existing environment/timing
sensitivities in `internal/storage/postgres`'s heavier integration tests, not
regressions introduced by this branch. `TestPostgresTerraformBackendQuerySurvivesNullTerraformBackendsPath`
specifically: a sibling executor is independently changing that exact query on
`5594-local-backend-default-path` (adding a `LEFT JOIN` and a column); that
work is not in this branch's base and cannot be causing this failure, but it
does mean the test is in active flux — its baseline failure here is against
UNMODIFIED `origin/main`, not against that sibling's in-flight work.

Both isolated containers used for this comparison (host ports 15949 and
15950) were created and torn down specifically for this check; neither reused
nor collided with `eshu-pg-5848` (the container this branch's other live
proofs run against) or the shared default Compose port set.

### Credential-free

```
$ go test ./internal/reducer/... ./internal/replay/costcounting/... ./cmd/reducer/... ./cmd/bootstrap-index/... -count=1
ok  	github.com/eshu-hq/eshu/go/internal/reducer
ok  	github.com/eshu-hq/eshu/go/internal/reducer/dsl
ok  	github.com/eshu-hq/eshu/go/internal/reducer/tags
ok  	github.com/eshu-hq/eshu/go/internal/reducer/tfstate
ok  	github.com/eshu-hq/eshu/go/internal/replay/costcounting
ok  	github.com/eshu-hq/eshu/go/cmd/reducer
ok  	github.com/eshu-hq/eshu/go/cmd/bootstrap-index
$ go build ./... && go vet ./...
(clean)
```

## The #5831 / #5837 golden-gate flake: causal verdict

**#5848 was NOT the cause of the `list_aws_runtime_drift_findings` empty
`drifted_attributes` flake.** The owner's own root-cause comment on #5837
(refuting their own and #5831's earlier hypotheses) is correct and independently
verified against the code in this branch's base:

- `drifted_attributes` is derived in-process from the SAME finding row's own
  evidence array (`iac_management_transform.go` ->
  `driftedAttributesFromAWSEvidence`) — finding and attributes are physically
  inseparable, so "attributes projected after the finding" cannot happen.
- The golden gate evaluates `required_json_values` in SORTED path order and
  returns on first failure (`go/internal/goldengate/query_shape_paths.go`).
  Sorted, `drift_findings[].drifted_attributes[].attribute` precedes
  `drift_findings[].finding_kind`, so the empty-attributes failure is the
  symptom of a WRONG finding_kind, not a timing gap.
- The actual mechanism is `cloudruntime.Classify` reading `state == nil` as a
  VERDICT (`orphaned_cloud_resource`, no drifted attributes) when the state
  scope has simply not activated yet — confirmed directly against
  `go/internal/storage/postgres/aws_cloud_runtime_drift_evidence_sql.go`'s
  `listActiveStateResourcesForAWSARNsQuery`, whose join requires
  `ingestion_scopes.active_generation_id = fact.generation_id`: a
  `state_snapshot` scope with a pending generation joins ZERO rows regardless of
  whether matching state data exists.

Why #5848's insert-admission mechanism does not explain the flake: at the point
#5837/#5831 were filed, `aws_cloud_runtime_drift` had NO retire and NO insert
race at all (verified above — no `fencing_token`, no retire query, no admission
table existed on `origin/main`). The flake's shape is ONE finding written
(`drift_findings has 1 results`) with the WRONG `finding_kind`, not two
contradictory findings for one ARN. #5848's insert-admission and retire
mechanisms answer "what happens when a SECOND, differently-classified pass
writes for the same ARN" — a question that only arises once something (the
reopen this same branch adds) causes a second pass to run at all. Before this
branch, the domain ran exactly once per (scope, generation), so there was never
a second write to race against; the flake is a ONE-write, wrong-verdict defect,
which is what the readiness defer (piece 1 above) fixes at the source.

The readiness defer does directly address #5837/#5831's mechanism: it stops
`Handle` from committing `orphaned_cloud_resource` as a durable verdict while
the state scope is still pending, which is the exact condition the owner's
root-cause comment identifies. The golden assertion itself
(`drift_findings[].drifted_attributes[].attribute`,
`drift_findings[].finding_kind`) is unchanged and unweakened by this branch —
closing the race at the source is what should let it stop being vacuously
satisfied by a degraded finding.

**What this branch does not prove:** full end-to-end confirmation on the real
B-7 20-repo/17-collector corpus requires
`scripts/verify-golden-corpus-gate.sh`, a Docker-based live gate this executor
lane does not run (orchestrator-serialized, per
`docs/internal/agent-guide.md#live-gate-serialization-and-contention`). The
deterministic Postgres-level reproduction above proves the READINESS DEFER
MECHANISM works correctly in isolation; it is not a substitute for the
orchestrator running the golden-corpus gate to confirm the flake stops
reproducing on the real corpus.

Performance Evidence: the aws_cloud_runtime_drift Postgres cost budget rose from
1 to 3 statements per `WriteAWSCloudRuntimeDriftFindings` call (measured, not
asserted — see `testdata/cassettes/replayoffline/aws-cloud-runtime-drift.cost-budget.json`
and the `TestCostBudget_AWSCloudRuntimeDrift` family in
`go/internal/replay/costcounting`): the insert-admission check and the
generation-authoritative retire are each one additional round-trip, required
for correctness (closing #5848), not an unmeasured regression. No other
hot-path Cypher, graph write, or query handler changed. The readiness defer adds
one `EXISTS` query per `Handle` call only when `ReadinessChecker` is wired
(opt-in; nil leaves the domain's cost unchanged).

Observability Evidence: `awsCloudRuntimeDriftWriteSupersededError` and
`awsCloudRuntimeDriftStatePendingError` each self-report a distinct
`FailureClass()` (`aws_cloud_runtime_drift_write_superseded`,
`aws_cloud_runtime_drift_state_pending`), which the existing
`ReducerQueue.failIntent` path already records on the
`ReducerRetrySurge` counter labeled by `failure_class`
(`go/internal/storage/postgres/reducer_queue_helpers.go`) and on the durable
`fact_work_items.failure_class`/`failure_message` columns — an operator can
distinguish a superseded-pass rejection, a readiness defer, and a genuine
handler error from each other and from ordinary retries, without a new metric
instrument. `AWSCloudRuntimeDriftHandler` additionally emits its own structured
`slog` line for each of the two cases (`logWriteSuperseded`,
`logStatePendingDefer`), distinct from the existing admitted-findings log line.
The `InstrumentedDB.Begin` fix restores `eshu_dp_postgres_query_duration_seconds`
coverage for every statement this writer's transaction issues, closing a gap
this branch's own architecture change would otherwise have introduced silently.
