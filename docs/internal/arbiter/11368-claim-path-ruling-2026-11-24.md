# Arbiter ruling: #11368 claim path stays proven by live Postgres tests, no Odù (2026-11-24)

Recorded by the coordinator; the arbiter does not edit files. Arbiter:
`arbiter-eshu`. Goal: #10671, lane #11368 (Part of #9209; named gap for
#11350). The arbiter ruled from the coordinator's raw observations
re-measured at Eshu origin/main `f91199d5ae` (OduResolver surface families,
Derive enumeration, manifest grep, determinism-layer order-independence,
live-test homes); the full observation set is the lane decision record
[11368-claim-path-decision.md](../evidence/11368-claim-path-decision.md).
The arbiter edited nothing and ran no promotion gates.

Ruling (the arbiter's headline, verbatim): ADOPT — claim path stays proven
by live Postgres tests, no Odù; keep #11368 as named gap linked from #11350.
Confidence: high. Rationale: Odù asserts order-independent terminal graph
truth, so any claim order over same cassette converges green and cannot fail
on ordering regression.

> 1. **Decision adopted.** Live tests are the correct home for ORDER BY /
> gate-boost / starvation / batch-vs-single selection. Verified:
> `OduResolver.Resolve` switches only on `fact_kind:` /
> `narrowed_correlation:` else `unrecognized ifa coverage surface`;
> `nonOduScenarioGuard` rejects non-`odu`; `Derive` enumerates only
> registry kinds + evidence-narrowed correlations. An Odù here needs a new
> surface family + resolver + derivation — a platform extension with its own
> design doc, not #11368 scope.
> 2. **Cassette cannot express the contract anyway.** Determinism layer
> requires byte-identical, order-independent canonical digests across
> worker counts. Claim ordering and the starvation bound are temporal /
> liveness properties under backlog; terminal graph is identical either
> way. A cassette Odù would be green whether or not ordering holds —
> proof theater, violating seed-only-green-rows.
> 3. **No manifest change.** The manifest carries zero
> `claim|queue|fanout|reducer` mentions (confirmed); that absence is
> correct under this ruling, not a gap to backfill.
>
> **Required proof (already present, keep as the contract):** the named
> live tests — `reducer_queue_batch_bounded_claim_test.go`,
> `reducer_queue_batch_claim_funnel_proof_test.go`,
> `reducer_queue_batch_candidate_rejoin_diff_test.go`,
> `projector_queue_source_fairness_test.go`, locked-arms / telemetry /
> replay tests, and the #11366 two-window regression — all confirmed
> present on disk. Coordinator posts the decision text on #11368 and links
> it from #11350 as the named gap.
>
> **What I will not do and why:** I will not order a `claim_path:` Odù or
> platform extension now — it would add a surface family whose gate cannot
> fail on the regression it claims to pin, trading accuracy for coverage
> optics. Revisit only if Ifá gains a temporal / liveness assertion layer;
> then the manifest row belongs to that platform change.
>
> **Unverified (NOT_CHECKED):** owner agreement and the GitHub posting /
> link — coordinator-owned per the decision record; the arbiter ruled only
> on the design question from source evidence.

Coordinator note: the GitHub posting is evidenced — decision text posted on
[eshu-hq/eshu#11368](https://github.com/eshu-hq/eshu/issues/11368) and linked
from [eshu-hq/eshu#11350](https://github.com/eshu-hq/eshu/issues/11350) on
2026-11-24; #11368 stays open until the owner acks.
