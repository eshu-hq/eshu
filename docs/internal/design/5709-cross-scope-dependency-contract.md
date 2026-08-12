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

## What actually exists today — corrected after review

My first draft claimed there was no producer-completion fanout and that the only
runtime consumer of the contract was `DomainDefinition.Validate()`. **That was
wrong**, and all three reviewers caught it independently (#6028 review).

The error is worth naming because it is the same one twice over: I grepped the
struct field `CrossScopeDependencies`, found only `Validate()`, and concluded
absence. The fanout consumes `CrossScopeCompletionEdges()` — a different
accessor over the *same* `crossScopeDependencyCatalog()`. I verified a name and
concluded a fact.

| Piece | State on `origin/main` | Evidence |
| --- | --- | --- |
| Contract on `DomainDefinition` | **Exists** | `go/internal/reducer/registry.go` |
| Catalog of consumer → producers | **Exists** | `crossScopeDependencyCatalog()` |
| Registered **consumers** | `ci_cd_run_correlation`, `supply_chain_impact` | `registry_additive_domains.go:184,259` |
| Registered **producers** | `container_image_identity`, `ci_cd_run_correlation` | same catalog |
| Activation-driven re-enqueue (design point 2) | **EXISTS and is wired in production** | `CrossScopeCompletionEdges()` derives edges from the catalog; reducer ACK inserts `cross_scope_completion_events` (`reducer_queue_batch.go:254`); `CrossScopeCompletionRunner` + `NewCrossScopeCompletionStore` are wired at `cmd/reducer/main.go:453`; the golden-corpus gate asserts the ledger drains |
| Readiness-defer error type + class | **Exists**, enrolled non-counting | `cross_scope_readiness.go`, `reducer_queue_readiness_sql.go:41` |
| Any handler that RETURNS the readiness error | **`ci_cd_run_correlation`**, since the readiness floor landed. It was NONE when this table was first written, which is what the rest of this doc reasons from | `cross_scope_readiness_floor.go`; `supply_chain_impact` is in the catalog and still has not opted in |

So **design point 2 is built**, not missing. The remaining gap was narrower and
more specific than the issue or my first draft implied: only the readiness-defer
correctness floor was absent, and it was absent everywhere — no handler returned
the error for any domain. The floor now exists for `ci_cd_run_correlation`, so
that last row is the one piece of this table the readiness-floor work moved.

A second correction from review: `container_image_identity` **is** in the
catalog, as a *producer* for both registered consumers. It is not registered as
a *consumer*, which is the thing #5699 needs. My first draft said "not in the
catalog at all", which contradicted my own registered-consumers row.

## What this means for the migration order in the issue

The issue proposes: readiness-defer first (as #5699's acceptance), then
activation re-enqueue, then delete the maintenance-reopen entries.

That order needs revising. Step 2 (activation re-enqueue) is **already
delivered** by the completion fanout, so the migration is not "build both, then
delete the band-aids" — it is "add the missing floor, then re-evaluate whether
the band-aids still have a job".

Step 1 is genuinely absent: the error type exists but is never returned, so the
correctness floor is in place for no domain. `container_image_identity` is in
the catalog as a producer but not as a consumer, which is what #5699 needs.

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

**2. Activation-driven re-enqueue (re-trigger) — ALREADY BUILT.** Reducer ACK
inserts into `cross_scope_completion_events`, `CrossScopeCompletionStore.Fanout`
derives the edges from `CrossScopeCompletionEdges()`, and
`CrossScopeCompletionRunner` schedules the consumers. The golden-corpus gate
asserts the ledger drains to zero.

Since it exists, the question the issue raised about it is settled in code
rather than open in design, and the remaining interest is confirmatory:

- Whether the fanout writes pinned to the consumer's CURRENT active generation
  rather than replaying a row under its original one. That distinction is what
  makes it a real fix and not a re-run of `ReopenSucceededReducerWorkItems`'
  latent bug, where replayed output lands in a non-active generation and every
  active-generation-joined query returns zero.
- Whether its write sits inside the producer's ACK transaction, and if so
  whether that lock envelope has a contention proof. This is the one piece I
  would still want evidence for, because it is the concurrency-sensitive part
  and it is already in production.

They are independent, and the system is currently in the (2)-only state: the
fanout re-triggers consumers on producer completion, but a consumer claimed
*before* that completion still resolves nothing and has nothing to defer on, so
it records an unresolved decision rather than waiting. That is precisely the
"fast-but-still-racy" case. Adding the floor is what closes it.

## Decisions the owner needs to make

1. **Is the readiness floor still wanted, now that the fanout exists?** It is
   the only missing piece. The fanout re-triggers a consumer *after* the
   producer completes; the floor is what stops a consumer claimed *before* that
   from recording an unresolved decision. Cheap, and independent of the fanout.
2. **Is `container_image_identity` registered as a CONSUMER?** It is in the
   catalog today as a *producer* only. Registering it as a consumer is what
   #5699 needs, and is a prerequisite for either enforcement point covering it.
3. **Does the existing fanout's ACK-transaction write have a contention proof?**
   This is the question I would prioritise. The mechanism is already in
   production, so if the write widens the producer ACK lock envelope, that risk
   is live now rather than hypothetical.
4. **Can the reopen entries be deleted already?** The issue treats deletion as
   gated on building the fanout — but it is built. So the real question is
   whether the fanout plus (if added) the floor covers
   `container_image_identity`, `deployable_unit_correlation` and
   `kubernetes_correlation_materialization`, and the answer differs per domain
   because only two domains are registered consumers.

## What I did not do

No code changes. The issue asks for the design artifact first, and three of the
four decisions above are ownership calls rather than engineering ones — in
particular (3), which sets the lock envelope of the producer Ack path and so
falls under the repo's concurrency-proof rules rather than a maintainer's
judgement alone.

Related: #5699 (readiness-defer acceptance for `container_image_identity`),
#5703, #5423 (the reopen this replaces), epic #5422.
