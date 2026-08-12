# #5709 — cross-scope readiness floor

## What was broken

Some reducer domains read canonical output that a *different* ingestion scope
produces. `ci_cd_run_correlation` is one: it runs in the CI scope and needs the
container image identity that the OCI registry scope publishes, so it can say
which image a CI run built. A "scope" here is one collector's slice of a
deployment — one registry, one CI provider, one cloud account — and each scope
publishes its work in versioned batches called generations.

The first version of this change said the gap was a consumer that was already
claimed when its producer finished. That was wrong, and a review caught it.
**That window is already repaired on main.** The completion fanout
(`go/internal/storage/postgres/cross_scope_completion_fanout.go`) sets
`cross_scope_replay_required = TRUE` on any consumer sitting in `claimed` or
`running`, and the trigger installed by migration 093 rewrites that row's
`succeeded` acknowledgement back to `pending`. The consumer runs again. Nothing
was needed here.

The gap that is actually left is the **activation window**, and it is narrower
and more specific:

1. The producer's reducer row reaches `succeeded`.
2. Its scope generation is *not activated yet* — activation happens later, at
   projector acknowledgement.
3. A consumer running in between reads through
   `container_image_identity_current_support_facts_for` (migration 092c), which
   returns a row only when `scope.active_generation_id` matches the generation
   **and** that generation's `status = 'active'`.
4. So the read resolves nothing. The consumer writes a durable "no correlation"
   decision, and no later event disturbs it.

Not a retry, not a visible failure. An answer, and a wrong one.

The first version also asked the wrong question in the code. It probed whether
any producer *reducer domain* had an unfinished *work item* anywhere. In the
window above there is no unfinished producer work — the producer succeeded —
so the probe reported "ready" in exactly the case it existed to catch.

## What changed

### The readiness signal

`ProducerScopeQuiescence` (`go/internal/storage/postgres/scope_quiescence.go`)
was already committed for this, with an EXPLAIN proof
(`docs/internal/evidence/5709-quiescence-probe.md`) and a doc comment stating
this contract — and zero production callers. It now has one. It answers: for a
set of collector kinds, which scopes are *quiescent-active* — generation active,
no projector work item still pending, retrying, claimed, or running?

`CrossScopeProducerReadinessStore` maps a consumer's declared producer domains
to their producer collector kinds and reports ready only when each required kind
has a quiescent-active scope. Both mappings are read out of the code, not
guessed:

| producer domain | collector kind | why |
| --- | --- | --- |
| `container_image_identity` | `scope.CollectorOCIRegistry` (`oci_registry`) | registered by `internal/coordinator/oci_registry_scheduler.go`, projected by `internal/projector/oci_registry_canonical.go` |
| `ci_cd_run_correlation` | `scope.CollectorCICDRun` (`ci_cd_run`) | emitted by `internal/collector/cicdrun/ghactionsruntime` and `.../gitlabciruntime` |

A producer domain with no entry is skipped, not guessed. A guessed kind that a
deployment never registers would hold every consumer of that producer at "not
ready" until the elapsed bound, once per repair cycle, waiting for a scope that
was never going to appear.

The old `producerDomainsHaveOutstandingWorkQuery`, its status list, and its
EXPLAIN script are deleted.

### Readiness is sampled before the load, not after

The first version sampled readiness *after* the cross-scope load. A producer
activating in that window makes the store answer "ready" about a snapshot the
load had already taken without it, so the handler durably writes a stale empty
correlation — the same bug, through a narrower window. The reopen path cannot
save it either: it selects `succeeded` rows, and a maintenance pass racing a
still-claimed intent skips it.

`checkCrossScopeProducerReadinessBeforeLoad` runs first and
`crossScopeProducerDeferral` combines its answer with the post-load count. One
`time.Now()` reading is threaded through both, so a slow load cannot push the
intent past its own bound. This mirrors
`checkAWSCloudRuntimeDriftReadinessBeforeLoad`, where a hostile review found the
same ordering bug (#5875 P1).

The reverse race is benign. A producer activating *between* the check and the
load only means the load reads fresher data than the signal assumed, which can
add evidence but never remove it.

### The floor does not apply when there was nothing to look up

`FactStore.ListActiveCICDRunCorrelationFacts` short-circuits an empty
digest/image-ref filter to no rows without querying. So a CI run that published
no container artifacts — normal for any repository whose CI never builds images
— resolves zero forever.

Under the first version that run deferred for the full 30 minutes. The retry is
30 seconds and does not back off: this deferral's own failure class freezes
`attempt_count`, so the exponential term never grows. About 60 wasted claims per
row per repair cycle, to look up nothing.

The floor now applies only when the pass actually had something to ask. A
`FactLoader` that does not implement the cross-scope seam counts the same way —
that empty result is a missing capability, not a producer that has not
activated.

### The bound

Unchanged from the first version, and still the part worth re-reading. This
failure class is non-counting by design, so the queue freezes `attempt_count`
and never dead-letters the row. Without a bound, a producer scope that is
permanently absent, failed, or stuck strands its consumers silently — worse than
the durable empty answer this floor prevents, because an empty answer is at
least visible and repairable.

The bound is elapsed time since the current repair cycle began, 30 minutes,
matching the sibling `aws_cloud_runtime_drift` gate. It **cannot** be a retry
count: the freeze means `attempt_count` reads the same value on every later
claim, so that comparison never fires. The sibling gate shipped that mistake
first and had to be re-proven against the real queue.

The anchor is `Intent.CycleStartedAt`, falling back to `EnqueuedAt`.
`EnqueuedAt` is immutable across a reopen and these rows are reopened routinely,
so anchoring on it alone reads as "already past the bound" on the first claim of
any reopened row — skipping the readiness lookup and committing with no grace
window. A zero anchor means unknown, not infinite, and keeps deferring.

### Telemetry

Each deferral now emits its own structured log line carrying `domain`,
`scope_id`, `generation_id`, `producer_domains`, `elapsed_since_cycle_start`,
and `max_wait`. Elapsed against the bound, never `attempt_count`: this class
freezes `attempt_count`, so it reads as a constant on every occurrence and
cannot tell an operator how close the intent is to converging.

Three comments claimed no handler ever produces
`cross_scope_producer_not_ready`. One does now, so they are corrected: the
telemetry-coverage row, the enrolment comment in `reducer_queue_readiness_sql.go`,
and the class doc in `cross_scope_readiness.go`.

## Readiness query cost

The floor issues one `ProducerScopeQuiescence` query per declared producer
collector kind, stopping at the first kind with no quiescent-active scope. The
wired consumer declares one producer, so it costs exactly one query.

The plan-shape proof for that query is
`docs/internal/evidence/5709-quiescence-probe.md`: the `NOT EXISTS` body is
byte-equivalent to the production reducer claim query's projector-drain fence,
it rides `fact_work_items_scope_generation_idx` with an Index Scan rather than
scanning `fact_work_items`, and it ran in 0.554 ms on a seeded 500-scope ×
50,000-work-item shape.

**What that proof is and is not.** It is a plan-shape confirmation on a synthetic
seed: one connection, no concurrent writers, no contention. It shows the
predicates are index-resolvable and the query does not degrade into a table scan.
It is **not** a scale measurement, not a worst-case measurement, and not a
contention measurement. No such measurement was taken for this change.

**A cost this change adds that the first version did not.** The first version
only probed when the load had already resolved nothing. Sampling before the load
means every `ci_cd_run_correlation` pass that has at least one digest or image
ref now runs one probe, whether or not it goes on to resolve. That is the price
of closing the ordering window, and it is not free — it is one query alongside
the several the handler already issues. It has not been measured under
production concurrency.

No-Regression Evidence: the floor adds one indexed `ProducerScopeQuiescence`
query per `ci_cd_run_correlation` pass that has something to look up, replacing
the first version's one `EXISTS` probe on `fact_work_items` that ran only on an
empty resolve. A pass with no digests and no image refs now runs *fewer*
queries than before, because the empty-filter gate skips both the probe and the
deferral it used to trigger. Plan shape for the retained query is the Index Scan
in `docs/internal/evidence/5709-quiescence-probe.md`. Baseline versus after:
`internal/reducer`, `cmd/reducer`, and `internal/storage/postgres` are green
before and after; terminal queue state is unchanged, since the floor enqueues
nothing and only converts one durable write into a bounded retry. Limits: plan
shape only, single connection, no contention arm.

Observability Evidence: deferrals carry the existing
`cross_scope_producer_not_ready` failure class on `fact_work_items`, counted by
`eshu_dp_reducer_retry_surge_total{failure_class}` and read by the golden-corpus
drain breakdown as readiness-deferred. New in this change: one structured log
line per deferral with `elapsed_since_cycle_start` against `max_wait`, which is
the only signal that says how much longer a waiting consumer has, because
`attempt_count` is frozen for this class. The error message names the consumer
domain, scope, generation, and the bounded producer set — never a uid, which
could be a redacted identifier. No new metric.

## Proof

```
$ cd go && go test ./internal/reducer/ ./cmd/reducer/ ./internal/storage/postgres/ -count=1
ok  github.com/eshu-hq/eshu/go/internal/reducer          3.656s
ok  github.com/eshu-hq/eshu/go/cmd/reducer               0.847s
ok  github.com/eshu-hq/eshu/go/internal/storage/postgres 5.120s
exit 0

$ bash scripts/verify-telemetry-coverage.sh
verify-telemetry-coverage: docs/public/observability/telemetry-coverage.md and
go/internal/telemetry/instruments.go agree, no new untracked stages
exit 0

$ bash scripts/verify-package-docs.sh
verify-package-docs: changed Go package docs present
exit 0

$ git diff --check
exit 0
```

### Which guards were proven to guard

Every negative test below was run against a deliberately broken production line
and observed to fail, then the break was reverted. A test that passes either way
guards nothing.

| break introduced | test that caught it |
| --- | --- |
| `container_image_identity` mapped to `CollectorGit` instead of `CollectorOCIRegistry` | `TestCrossScopeProducerCollectorKindsForResolvesEveryCatalogConsumer`, plus 3 more |
| store never reports not-ready (`len(quiescent) < 0`) | `TestCrossScopeProducersReadyDefersWhenNoProducerScopeIsQuiescent`, `...StopsAtTheFirstMiss` |
| readiness sampled *after* the load | `TestCICDRunCorrelationDefersDespiteProducerActivatingDuringTheLoad`, and only that one |
| nothing-to-look-up gate removed | `TestReadinessFloorDoesNotApplyWhenThereWasNothingToLookUp`, `...DoesNotDeferARunWithNoImageArtifacts`, `...DoesNotDeferWithoutTheCrossScopeLoaderSeam` |
| `elapsed_since_cycle_start` and `max_wait` dropped from the defer log | `TestCICDRunCorrelationDefersWhenIdentityProducerScopeHasNotActivated` |
| collector-kind dedup removed | `TestCrossScopeProducerCollectorKindsDeduplicatesAndSorts` |

The ordering break is the one worth noting: it failed exactly one test and no
others, which is what a targeted guard should do.

The Postgres store had no test file at all before this change. Nothing outside
it referenced it, so a wrong status set or a broken binding shipped undetected —
demonstrated by mutating the old status list and watching
`go test ./internal/storage/postgres/` stay green.

## What a reviewer should push on

**`supply_chain_impact` is a registered consumer and is still not wired.** The
catalog declares it depending on `container_image_identity` and
`ci_cd_run_correlation`. Only the CI/CD consumer has the floor. Adding a
consumer to the catalog does **not** gate it — the handler has to call the floor
helper — and the first version's comment claiming otherwise was false and is
deleted. Wiring the second consumer is a handler edit, not new machinery, but it
is not done, and this change does not close the contract for every consumer.

**Registering `container_image_identity` as a producer is still blocked.**
`CrossScopeDependency` carries only `ProducerDomains []Domain`, and `Validate()`
rejects a producer that is not a registered *reducer* domain. That domain's
actual producer is the raw OCI collector, which has no reducer acknowledgement —
exactly why it was never in the catalog. Expressing it needs the
producer-scope-kind half of the contract that #5709 proposed and nobody built.
`aws_cloud_runtime_drift_readiness.go` names the same gap and defers it the same
way. This change does not depend on it and does not attempt it.

**The collector-kind mapping is under-inclusive, on purpose.**
`container_image_identity` intents are also enqueued in `aws`, `azure`, `gcp`,
`git`, and `sbom_attestation` scopes — see
`containerImageIdentityCandidateFactKinds` in
`internal/projector/container_image_identity_intents.go`. Identity output can
therefore be published by a scope this mapping does not name, and the floor does
not wait for those. Widening it would make any mid-ingestion cloud scope
anywhere block every CI correlation, which is a worse trade. But it does mean a
digest whose identity comes from an ECR scope can still be answered early. This
is the sharpest remaining hole and it is worth disagreeing with.

**Quiescence does not prove the producer's reducer has run.** The probe checks
that a producer scope's generation is active and its *projector* work has
drained. The identity reducer writes its support rows in a later stage. So there
is a residual window — projector drained, generation activated, identity reducer
row still pending — where the floor reports ready and the join is still empty.
Narrower than the window this change closes, and not closed by it.

**"At least one quiescent scope of the kind", not "all of them".** With several
OCI registry scopes, one mid-ingestion scope does not hold the consumer back if
another is quiescent. Closing that needs the total registered-scope count, which
would be a second query with no committed plan proof, so it was not added.

**A deployment with no producer scope at all waits the full bound.** If a
repository's CI publishes image digests but no OCI registry collector is
configured, the probe returns nothing, the consumer defers, and it converges only
at the 30-minute bound — once per repair cycle. The empty-filter gate removes the
common instance of this (no digests at all), but not this one. Bounded and
visible in the defer log, but real.

**Dead-lettered producers do not hold consumers back.** A dead letter is
finished, badly, and waiting on it would turn one failed producer into an
indefinitely stalled correlation surface. Under the quiescence probe this falls
out of the design rather than being a special case: a dead-lettered *projector*
row is not in `('pending','retrying','claimed','running')`, so the scope reads as
quiescent and the consumer commits its best available answer. Worth disagreeing
with if you see it differently.

Refs #5709
