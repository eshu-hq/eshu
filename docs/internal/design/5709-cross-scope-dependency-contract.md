# #5709 — declared cross-scope dependency contract

Design artifact. No production behaviour changes with this document; it records
what exists today, what the issue assumed, where those differ, and the decidable
choices left for the owner.

## The defect, restated

A reducer domain that consumes the durable output of another reducer domain in a
different ingestion scope has no reliable re-trigger. Generations activate at
projector Ack, reducer intents are enqueued pre-Ack and claimed concurrently, so
a consumer intent can run before the producer's row is active, resolve nothing,
and never run again. The result is a silent zero: the query returns no rows and
nothing records that it was asked too early.

## What actually exists today — verified, not assumed

The issue reads as though none of the contract exists. That is not the current
state, and the difference changes what remains to build.

| Piece | State on `origin/main` | Evidence |
| --- | --- | --- |
| Contract on `DomainDefinition` | **Exists** — `CrossScopeDependencies []CrossScopeDependency` | `internal/reducer/registry.go` |
| Catalog of consumer → producers | **Exists**, two consumers | `crossScopeDependencyCatalog()` in `internal/reducer/cross_scope_dependencies.go` |
| Registered consumers | `ci_cd_run_correlation` → `container_image_identity`; `supply_chain_impact` → `container_image_identity`, `ci_cd_run_correlation` | `registry_additive_domains.go:184,259` |
| Readiness-defer error type | **Exists** — `CrossScopeProducerNotReadyFailureClass`, `newCrossScopeProducerNotReadyError` | `internal/reducer/cross_scope_readiness.go` |
| Readiness class enrolled non-counting | **Yes** | `reducer_queue_readiness_sql.go:41` |
| Readiness-defer **invoked** by any handler | **NO — zero call sites** | `rg newCrossScopeProducerNotReadyError` outside its own definition and tests returns nothing |
| Producer-completion fanout / re-enqueue | **NO — does not exist** | no fanout function; the only runtime consumer of `CrossScopeDependencies` is `DomainDefinition.Validate()` |

So the declaration surface is built and validated at startup, and **nothing reads
it to schedule anything**. Both enforcement points the issue proposes are absent.

### A stale claim worth fixing regardless of which option is chosen

`DomainDefinition.CrossScopeDependencies`' doc comment says:

> The producer-completion fanout derives runtime scheduling edges from the same
> catalog used to populate this declaration, so registered truth and convergence
> behavior stay in lockstep.

There is no producer-completion fanout. A reader adding a new consumer domain
today would reasonably believe registering it buys re-triggering, and it buys
nothing but a startup validation. This is the same class of defect the contract
is meant to prevent — a declaration that looks load-bearing and is inert.

## What this means for the migration order in the issue

The issue proposes: readiness-defer first (as #5699's acceptance), then
activation re-enqueue, then delete the maintenance-reopen entries.

That order still holds, with one correction: step 1 is **not** partially done.
The error type exists but is never returned, so the correctness floor is not in
place for any domain, including `container_image_identity` — which is also not
registered in the catalog at all.

## The two enforcement points, and what each is worth alone

**1. Readiness-defer (correctness floor).** When a consumer's cross-scope load
resolves nothing and a declared producer scope is pre-first-activation or has
in-flight projector work, return the typed non-counting retry error.

- Converges without any new scheduling machinery: the queue already retries, and
  the class is already enrolled non-counting so the miss cannot erode the
  attempt budget or dead-letter a still-pending intent (this is the same shape
  as the seventeen readiness classes enrolled in #5046).
- Bounded by the same trade #5046 documents: a non-counting class retries on
  backoff indefinitely rather than dead-lettering, so a producer that never
  publishes leaves the consumer deferring forever, visible as a per-class surge
  on `eshu_dp_reducer_retry_surge_total{failure_class}`.
- Does **not** fix latency: the consumer re-runs on its retry cadence, not when
  the producer becomes ready.

**2. Activation-driven re-enqueue (re-trigger).** Inside the producer's Ack
transaction, insert NEW deterministic-ID pending consumer intents pinned to the
consumer's CURRENT active generation, `ON CONFLICT DO NOTHING`.

- Fixes latency, and is the only piece that removes the reopen band-aid.
- Carries the real concurrency risk: it writes to the consumer's queue from
  inside the producer's Ack transaction, so it must not extend that transaction's
  lock envelope or introduce a lock-order inversion between producer Ack and
  consumer claim. That needs a contention proof, not an argument.
- "Pinned to the consumer's CURRENT active generation" is the load-bearing
  detail and the fix for the band-aid's latent bug: `ReopenSucceededReducerWorkItems`
  replays the SAME row under its ORIGINAL generation_id, so once the active
  generation moves, replayed output lands in a non-active generation and every
  active-generation-joined query returns zero.

They are independent. (1) alone makes the system correct-but-slow. (2) alone
makes it fast-but-still-racy, because a consumer claimed before the producer Ack
still resolves nothing with nothing to defer on. The floor should land first.

## Decisions the owner needs to make

1. **Does the readiness-defer ship without the fanout?** It is the smaller,
   safer change and it removes the silent-zero today. It leaves the maintenance
   reopen in place, so nothing gets deleted yet.
2. **Is `container_image_identity` registered as a consumer?** It is the issue's
   first named instance and #5699's acceptance, and it is absent from the
   catalog. Registration is a prerequisite for either enforcement point covering
   it.
3. **Where does the re-enqueue write from?** Inside the producer's Ack
   transaction (atomic, but widens that transaction) or from a separate
   post-Ack step (narrower lock envelope, but no longer atomic with activation
   and so needs its own convergence argument).
4. **What replaces the reopen list, and when is it deleted?** The issue says
   delete `container_image_identity`, `deployable_unit_correlation`, and
   `kubernetes_correlation_materialization`. That deletion is only safe after
   (2) covers each of them; deleting earlier trades a band-aid with a known bug
   for no coverage at all.

## What I did not do

No code changes. The issue asks for the design artifact first, and three of the
four decisions above are ownership calls rather than engineering ones — in
particular (3), which sets the lock envelope of the producer Ack path and so
falls under the repo's concurrency-proof rules rather than a maintainer's
judgement alone.

Related: #5699 (readiness-defer acceptance for `container_image_identity`),
#5703, #5423 (the reopen this replaces), epic #5422.
