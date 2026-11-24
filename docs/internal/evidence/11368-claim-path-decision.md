# #11368 decision record: the reducer claim path cannot be an Odù — live tests stay the home

Part of #9209. Refs #11350, #10972, #11366.

## Recommendation

Record the owner-agreed decision on #11368: **the claim path
(ordering incl. gate boost and starvation bound, batch-vs-single
selection) is proven by live Postgres tests, not by an Odù.**
Keep #11368's gap named and linked from #11350 per its "no silent
gaps" acceptance criterion. Do not extend
`specs/ifa-coverage-manifest.v1.yaml` for this surface.

## Why a cassette cannot pin claim ordering

An Odù proves terminal graph truth, never dequeue order:

- The manifest admits exactly two surface families —
  `fact_kind:` and `narrowed_correlation:` — and `OduResolver.Resolve`
  (`go/internal/ifa/coverage.go`) returns `unrecognized ifa coverage
  surface` for anything else. `nonOduScenarioGuard` additionally
  rejects any non-`odu` scenario. A `claim_path:` surface would need a
  new surface family in `Derive`/`RunCoverage` plus a new resolver
  rule: a platform extension, not an Odù following the authoring
  checklist (`docs/public/guides/add-an-odu.md` step 5 allows only
  the two existing families).
- `Derive` (`go/internal/ifa/expectations.go`) enumerates expectations
  only from the fact-kind registry and the B-12 evidence-narrowed
  correlations. Claim ORDER BY, the gate boost
  (`gate_waited_generations` vs `checkpoints.last_generation_id` in
  `go/internal/storage/postgres/reducer_queue_batch_query.go`), the
  starvation bound (one boosted slot per claim, #11366), and
  batch-vs-single selection change *which pending row dequeues first
  under backlog*. The final projected graph is identical either way.
- The determinism layer asserts that identity on purpose: the
  canonicalized graph is content-addressed and order-independent, and
  the matrix requires byte-identical digests across worker counts
  (`scripts/verify-ifa-determinism.sh`). An ordering regression
  passes every Odù layer by design — the cassette pins inputs, and
  any claim order over the same inputs converges to the same graph.
- The starvation bound is a liveness property over time (who waits
  how long under backlog). Odù layers assert terminal state, never
  temporal order, so no cassette expectation can express it.

A cassette-backed Odù for this surface would therefore be green
whether or not the ordering contract holds: proof theater, and a
violation of the seed-only-green-rows rule.

## Where the surface is proven today

Live Postgres tests pin the exact behaviors #11368 names:

- `reducer_queue_batch_bounded_claim_test.go` — pins the ORDER BY /
  LIMIT shape of both claim arms (unbounded LIMIT fails the guard).
- `reducer_queue_batch_claim_funnel_proof_test.go`
  (`TestClaimBatchFunnelReconcilesLive`) — hand-derived funnel
  census: admitted batch plus closed-reason drops reconcile exactly
  to the candidate count, at two batch limits.
- `reducer_queue_batch_candidate_rejoin_diff_test.go` — pins the
  search-doc starvation ordering shape (every priority-0 candidate
  sorts ahead of priority-1) and candidate/rejoin equivalence.
- `projector_queue_source_fairness_test.go` and the
  `reducer_queue_batch_locked_arms` / telemetry / 10862–10863 replay
  tests — fairness and batch-vs-single selection under contention.
- #11366's own acceptance criterion (two-window failing-first
  regression: drain the newer, assert the older still sorts boosted)
  is a live-queue test by construction — a cassette has no windows,
  no generations, and no backlog to drain.

The manifest carries zero `claim|queue|fanout|reducer` mentions
(`rg -i` over the file returns nothing); that absence is correct
under this decision, not a gap to backfill.

## What would change this decision

A new `claim_path:` (or equivalent) surface family in
`Derive`/`RunCoverage`/`OduResolver` with its own resolver rule and
proof gate, i.e. an Ifá platform extension with a design doc — out
of scope for #11368's either/or. If that platform work ever lands,
the manifest row belongs to it, and this note becomes the pointer.

## Suggested text for #11368 (owner to agree / coordinator to post)

> Decision (date, owner): the claim path stays proven by live
> Postgres tests; no Odù. Rationale: Odù layers assert terminal,
> order-independent graph truth, so any claim order over the same
> cassette converges green — a claim-path Odù could not fail when
> the ordering contract breaks. The ordering contract lives in the
> batch-claim live tests named in
> `docs/internal/evidence/11368-claim-path-decision.md`. This issue
> stays open/linked from #11350 as the named gap: if Ifá ever gains
> a claim-path surface family, the manifest row belongs to that
> platform change.

## Outstanding

- `arbiter-eshu` verdict on this design decision: not obtained —
  no arbiter invocation route exists in this leaf session.
  Coordinator to escalate with the raw observations above.
- Owner agreement + posting the decision text on #11368 and the
  #11350 link: coordinator/owner owned.
