# #5709 — cross-scope readiness floor

## What was broken

The cross-scope dependency contract had a re-trigger and no floor.

When a producer reducer domain finishes, the completion fanout re-enqueues the
consumers that declare it in `crossScopeDependencyCatalog`. That part was built
and works. It cannot help a consumer that was **already claimed** when the
producer finished: that consumer's cross-scope load resolves nothing, it writes
a durable "no correlation" decision, and no later event disturbs it.

So a CI run whose container image identity had not committed yet produced a
permanent empty correlation. Not a retry, not a visible failure — an answer.

The error type for the missing half already existed in the package:
`crossScopeProducerNotReadyError`, with `CrossScopeProducerNotReadyFailureClass`
already enrolled in `nonCountingReducerRetryFailureClasses`. Nothing returned
it. The enrolment comment said so outright: *"Inert until the readiness-defer
slice wires a handler to return crossScopeProducerNotReadyError; nothing
produces this class yet."*

## What changed

`deferWhenCrossScopeProducersNotReady` returns that error when three things hold
at once: the consumer resolved nothing, it declares producers in the catalog,
and a wired readiness seam reports those producers unfinished. Every other case
returns nil, so the success path is untouched.

Wired into `CICDRunCorrelationHandler` and backed by
`postgres.CrossScopeProducerReadinessStore`, from `cmd/reducer/main.go` through
`DefaultHandlers`.

### The bound is the part that took a second pass

The first version could defer forever, which is a worse bug than the one it
fixes. This failure class is non-counting by design, so the queue freezes
`attempt_count` and never dead-letters the row. A producer scope that is
permanently absent, failed, or stuck would strand every consumer of it silently.

The bound is elapsed time since the current repair cycle began — 30 minutes,
matching the sibling `aws_cloud_runtime_drift` gate. It **cannot** be a retry
count: this class's own freeze means `attempt_count` reads the same value on
every later claim, so that comparison never fires. The sibling gate shipped that
mistake first and had to be re-proven against the real queue.

The anchor is `Intent.CycleStartedAt`, falling back to `EnqueuedAt`.
`EnqueuedAt` is immutable across a reopen and these rows are reopened routinely,
so anchoring on it alone reads as "already past the bound" on the first claim of
any reopened row — skipping the readiness lookup and committing with no grace
window at all. A zero anchor means unknown, not infinite, and keeps deferring.

## Readiness query cost

The readiness seam adds one `EXISTS` probe per deferred consumer pass against
`fact_work_items`, which is write-hot. Measured before shipping, Postgres 17,
400,000 reducer rows across five domains, index as shipped in migration 005:

| case | plan | exec | buffers |
| --- | --- | --- | --- |
| producers all finished (absence probe) | `Index Only Scan` | 0.017 ms | 3 |
| two-producer consumer, one unfinished (early exit) | `Index Only Scan` | 0.009 ms | 4 |

`stage = 'reducer'` is an equality on the leading column of
`fact_work_items_stage_domain_status_idx`, and `domain` and `status` follow it,
so all three predicates land in the index condition and the probe never touches
the heap.

The absence probe is the worst case, not the early-exit one: with every producer
succeeded there is no row to stop at. It is also the common case in steady
state, and it costs 3 buffers.

**Limits of this measurement.** Synthetic table, five domains, uniform
distribution, single connection, no concurrent writers. It shows the plan shape
and that the predicates are index-resolvable; it is not a contention
measurement. A third arm in the script (`5709-readiness-explain.sql`, ARM 2) was
mislabelled when written — the row it flipped to `pending` belongs to
`ci_cd_run_correlation`, not the domain being probed, so it re-ran the absence
case. The early-exit number above is ARM 3, which does return `rows=1`.

No-Regression Evidence: 0.009–0.017 ms per readiness probe on 400,000 rows,
`Index Only Scan`, 3–4 shared buffers, no heap access. The probe runs only on a
pass that resolved nothing and only for a domain in the catalog, so a
correlation that resolves normally does no extra work at all.

Observability Evidence: deferrals surface as the existing
`cross_scope_producer_not_ready` failure class on `fact_work_items`, which the
queue already records per row with its failure reason. The error message names
the consumer domain, scope, generation, and the bounded producer set — never a
uid, which could be a redacted identifier. No new metric or span; the class was
already enrolled and already queryable, it simply had no producer until now.

## Proof

```
cd go && go test ./internal/reducer/ ./cmd/reducer/ ./internal/storage/postgres/ -count=1
ok  github.com/eshu-hq/eshu/go/internal/reducer          3.087s
ok  github.com/eshu-hq/eshu/go/cmd/reducer               1.442s
ok  github.com/eshu-hq/eshu/go/internal/storage/postgres 5.114s
```

Ten tests cover the floor. The ones that carry the argument:

- the handler defers instead of writing, and the writer is not called on a
  deferral — proven to fail when the wiring is removed from
  `ci_cd_run_correlation.go`, so it is a guard and not decoration;
- a resolved join never consults readiness at all (order of checks, not trust);
- quiescent producers with an empty join converge on the empty answer;
- the elapsed bound converges and skips the lookup;
- a high `AttemptCount` does **not** substitute for elapsed time;
- a reopened row gets a fresh window;
- a zero anchor defers rather than reading as infinitely elapsed;
- a readiness-store error surfaces as itself, never as the non-counting class —
  a broken store hidden in that class would retry forever without surfacing;
- the registration carries the seam, so the floor cannot ship inert.

## What a reviewer should push on

**`supply_chain_impact` is a registered consumer and is not wired here.** The
catalog declares it depending on `container_image_identity` and
`ci_cd_run_correlation`. Only the CI/CD consumer got the floor in this change.
The store resolves producers from the shared catalog, so wiring the second
consumer is a handler edit and not new machinery — but it is not done, and this
change should not be read as closing the contract for every consumer.

**Registering `container_image_identity` as a producer is still blocked.**
`CrossScopeDependency` carries only `ProducerDomains []Domain`, and `Validate()`
rejects a producer that is not a registered *reducer* domain. That domain's
actual producer is the raw OCI collector, which has no reducer acknowledgement —
which is exactly why it was never in the catalog. Expressing it needs the
producer-scope-kind half of the contract that #5709 proposed and nobody built.
`aws_cloud_runtime_drift_readiness.go` names the same gap and defers it the same
way. This change does not depend on it and does not attempt it.

**The readiness signal is coarse.** It asks whether any declared producer domain
has unfinished work anywhere, not whether one specific pending row would resolve
this specific join. That is not knowable until the output exists and the join
resolves. The asymmetry is what makes it safe: a false "not ready" costs one
bounded non-counting retry, a false "ready" is the original bug.

**Dead-lettered producers do not hold consumers back.** A dead letter is
finished, badly, and waiting on it would turn one failed producer into an
indefinitely stalled correlation surface. The consumer commits its best
available answer; the reopen path re-runs it once an operator repairs the
producer. That is a deliberate call and worth disagreeing with if you see it
differently.

Refs #5709
