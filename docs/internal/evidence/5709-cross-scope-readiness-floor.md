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
set of collector kinds, which scopes exist, and which of those are
*quiescent-active* — generation active, no projector work item still pending,
retrying, claimed, or running?

Returning the registered scopes and not only the quiescent ones matters more
than it sounds. The first version returned only the quiescent set, so an empty
answer meant either "this deployment runs no collector of that kind" or "one
exists and has not finished", and the floor read both as not-ready. A deployment
that indexes repositories whose CI publishes image digests but runs no OCI
registry collector — ordinary, since that collector needs registry credentials —
therefore deferred *every* `ci_cd_run_correlation` intent to the 30-minute bound.
The retry does not back off, because this failure class freezes `attempt_count`,
so that is roughly 60 no-op claims per row per repair cycle against the
write-hot `fact_work_items` table. Zero registered scopes of a kind now reads as
ready. The sibling AWS gate already answered absence the same way:
`HasPendingStateSnapshotEvidence` returns false — meaning ready — when no
`state_snapshot` scope exists.

Both answers come from the same query, so absence costs no extra round trip. The
plan for it was measured before it was written, including one shape that was
rejected: writing the quiescent flag as a target-list `NOT EXISTS` lets
PostgreSQL 16 de-correlate and hash the subquery, sequentially scanning
`fact_work_items` at 5.16 ms against 0.30 ms. Keeping the anti-join inside a CTE
holds the index plan at 0.34 ms. Both plans are in
`docs/internal/evidence/5709-quiescence-probe.md`.

`CrossScopeProducerReadinessStore` maps a consumer's declared producer domains
to their producer collector kinds and reports ready only when each required kind
that this deployment actually registers has a quiescent-active scope. Both
mappings are read out of the code, not guessed:

| producer domain | collector kind | why |
| --- | --- | --- |
| `container_image_identity` | `scope.CollectorOCIRegistry` (`oci_registry`) | registered by `internal/coordinator/oci_registry_scheduler.go`, projected by `internal/projector/oci_registry_canonical.go` |
| `ci_cd_run_correlation` | `scope.CollectorCICDRun` (`ci_cd_run`) | emitted by `internal/collector/cicdrun/ghactionsruntime` and `.../gitlabciruntime` |

A producer domain with no entry is skipped, not guessed.

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

## Why this is safe under concurrent reducers

The floor adds a read and a decision, and touches no coordination state. Spelled
out, because "obviously safe" is how unsafe things ship:

The probe is a plain `SELECT` with no `FOR UPDATE`, no advisory lock, and no
write. It takes no row locks, so it cannot deadlock against a claim, an
acknowledgement, or another reducer's projection, and it cannot block a writer.

It runs *outside* the claim transaction. The queue claims the work item and
commits; the handler runs afterwards, so the probe holds nothing open across it.
A slow probe delays one handler, not the claim path other workers depend on.

Nothing about lease, status, ordering, or idempotency changes. A deferral
returns an error the queue already knows how to classify; the row goes back to
retrying under its existing lease rules, in a failure class that was already
enrolled as non-counting. No new work item is enqueued, no status transition is
added, and the conflict domain (one scope generation, one domain) is untouched.

The one race worth naming is the readiness sample against a producer activating
concurrently, and it is asymmetric on purpose. Sampling *before* the load means
the signal can only be staler than the load, never fresher — see the section
above. A producer that activates between the sample and the load makes the load
read more evidence than the signal assumed, and the post-load resolved count is
what decides. The reverse ordering is the bug, and it is what the ordering test
guards.

Two consumers of the same producer probing at once see the same committed state
and reach the same answer independently; there is no shared mutable state
between them. Two passes of the *same* intent cannot overlap, because the queue
claim is exclusive.

## Readiness query cost

The floor issues one `ProducerScopeQuiescence` query per declared producer
collector kind, stopping at the first *registered* kind with no quiescent-active
scope. A kind with no registered scope is skipped and the remaining kinds are
still probed. The wired consumer declares one producer, so it costs exactly one
query.

The plan-shape proof for that query is
`docs/internal/evidence/5709-quiescence-probe.md`: the `NOT EXISTS` body is
byte-equivalent to the production reducer claim query's projector-drain fence,
it rides `fact_work_items_scope_generation_idx` with an Index Scan rather than
scanning `fact_work_items`, and it ran in 0.34 ms (median of five) on a seeded
500-scope × 50,000-work-item shape — 0.30 ms before the registered-scope column
was added.

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
deferral it used to trigger. Reporting the registered scopes alongside the
quiescent ones keeps that at one query and holds the plan: same Nested Loop Anti
Join, same Index Scan, same 795 shared buffers, 0.300 ms to 0.338 ms median on
the same seed. Plan shape for the retained query is the Index Scan
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
ok  	github.com/eshu-hq/eshu/go/internal/reducer	2.894s
ok  	github.com/eshu-hq/eshu/go/cmd/reducer	1.135s
ok  	github.com/eshu-hq/eshu/go/internal/storage/postgres	5.002s
exit=0

$ ESHU_POSTGRES_DSN=postgresql://eshu:...@localhost:15709/eshu_live \
    go test ./internal/storage/postgres -run ProducerScopeQuiescenceLive -count=1 -v
--- PASS: TestProducerScopeQuiescenceLive (4.70s)
    --- PASS: .../a_collector_kind_with_no_scope_at_all_reports_nothing_registered (0.00s)
    --- PASS: .../an_active_scope_with_live_projector_work_is_registered_but_not_quiescent (0.01s)
    --- PASS: .../a_reducer-stage_work_item_does_not_hold_the_scope_back (0.01s)
    --- PASS: .../a_scope_with_no_active_generation_is_registered_but_not_quiescent (0.00s)
ok  	github.com/eshu-hq/eshu/go/internal/storage/postgres	7.253s

$ bash scripts/verify-telemetry-coverage.sh
verify-telemetry-coverage: docs/public/observability/telemetry-coverage.md and
go/internal/telemetry/instruments.go agree, no new untracked stages
telemetry_exit=0

$ bash scripts/verify-package-docs.sh
verify-package-docs: changed Go package docs present
pkgdocs_exit=0

$ git diff --check
diffcheck_exit=0
```

### The golden-corpus gate (B-7)

B-7 is the run that indexes a fixed 30-repository corpus through the real
pipeline and diffs the resulting graph and query answers against a committed
snapshot. `ci_cd_run_correlation` output is part of what it asserts, and
`testdata/golden/e2e-baseline.json` calls this chain the convergence long pole.

```
summary: 554 pass, 0 required-fail, 0 advisory-warn

=== PASS: B-7 golden corpus gate green (elapsed 127s, budget ceiling 1800s) ===
```

**Read that as a no-regression result, not as proof the floor works.** Every
drain checkpoint reported `fact_work_items_residual: residual=0`, and the gate
prints its `live= / readiness-deferred= / dead_letter= / failed=` breakdown only
when residual rows exist (`drains.go`, `len(rows) == 0` short-circuit). So the
**readiness-deferred count is 0**: no row sat deferred at any checkpoint, and no
deferral log line appears in the run.

That is the expected outcome, not a surprise. Quiescence fences only the
`projector` stage, so in a gate run the OCI registry scope reads quiescent-active
before `ci_cd_run_correlation` executes and the floor never fires. What B-7
proves here is that arming the floor did not change the corpus answers, the drain
shape, or the timing (`phase_first_drain: observed=64.0s, baseline=75.0s`).

What proves the floor actually defers is
`TestBuildReducerServiceWiresCrossScopeProducerReadiness`, which runs the real
`buildReducerService` wiring against a producer scope that has not finished and
observes the `cross_scope_producer_not_ready` failure class come back out.

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
| absent producer kind treated as not-ready (guard deleted) | `TestCrossScopeProducersReadyIsReadyWhenNoScopeOfTheProducerKindExists`, `...SkipsOnlyTheAbsentKind` |
| projector-stage fence changed to `reducer` in the shipped SQL | `TestProducerScopeQuiescenceLive`, two subtests, against real Postgres |
| `CrossScopeProducerReadiness` line deleted from `buildReducerService` | `TestBuildReducerServiceWiresCrossScopeProducerReadiness` (nil error) |
| `CrossScopeReadinessLogger` line deleted from `buildReducerService` | the same test (missing defer log) |

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
not wait for those. A digest whose identity comes from an ECR scope is still
answered early: #5709 is narrowed on that path, not closed.

An earlier version of this document defended the narrow map by saying a wider
one would let any mid-ingestion cloud scope block every CI correlation. That
argument was wrong, and the wrongness is worth stating because it would mislead
whoever tries to widen it. The store asks for **at least one** quiescent scope
per kind, not all of them, so adding `git` — one scope per repository, hundreds
of them, one almost always quiescent — would block almost nothing.

The actual reasons the map stays at two entries: only those two mappings are
grounded in code (each cites the scheduler that registers the scope and the
projector that publishes it), and every kind added becomes a condition every
consumer of that producer must satisfy on every pass, plus one more probe query.
Grow it with evidence that the missing kind really publishes what a consumer
reads, not by pattern-matching names.

**Quiescence does not prove the producer's reducer has run.** The probe checks
that a producer scope's generation is active and its *projector* work has
drained. That is a proxy for the read the consumer actually performs, not the
same predicate. `container_image_identity_current_support_facts_for` (migration
092c) requires three things: `scope.active_generation_id` matching the identity
domain's state row, `generation.status = 'active'`, and that state row carrying
an `active_set_id`. The probe evaluates none of them — it checks only that
`active_generation_id` is set and projector work has drained. So a producer that
has activated and drained but whose identity reducer has not yet written its
support set reads as ready and joins to nothing. Narrower than the window this
change closes, and not closed by it.

**A third residual window: the gap before projector items exist.**
`fact_work_items` carries only `projector` and `reducer` stages. Between a new
generation being scheduled and its projector items being enqueued, the scope has
no live projector row, so the probe reads it as quiescent — off its *previous*
activation. A consumer landing in that gap is told the producer is finished when
its next batch has not started. Same shape as the window above: bounded, real,
and not addressed here.

**"At least one quiescent scope of the kind", not "all of them".** With several
OCI registry scopes, one mid-ingestion scope does not hold the consumer back if
another is quiescent. The registered-scope count is now available in the same
query, so switching to all-of-them would no longer cost a second round trip —
but it would be a different and much stricter contract, and nothing measured
says it is the right one. Left as is, deliberately.

**Dead-lettered producers do not hold consumers back.** A dead letter is
finished, badly, and waiting on it would turn one failed producer into an
indefinitely stalled correlation surface. Under the quiescence probe this falls
out of the design rather than being a special case: a dead-lettered *projector*
row is not in `('pending','retrying','claimed','running')`, so the scope reads as
quiescent and the consumer commits its best available answer. Worth disagreeing
with if you see it differently.

Refs #5709
