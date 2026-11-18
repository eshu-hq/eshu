# Arbiter ruling: reducer execution model decisions D1–D6 (2026-11-18)

Recorded by the coordinator; the arbiter does not edit files. Arbiter:
`arbiter-eshu`. Issue: #10861 (epic #10860, Phase 0). Design:
[10861-reducer-execution-model.md](../design/10861-reducer-execution-model.md)
at `b266455ca7`. The arbiter verified the design record, `gh issue view` for
#10861, #10860, #10862, #10875–#10878, #10880–#10883, evidence-5088
(6,487.757 ms → 2,936.490 ms, exact set match), and every code pointer below.
#10862 baseline criteria are all unchecked — there is no baseline dataset
yet, which governs every limit verdict.

**Ruling summary: accept D1, D2, D4 directions; reject D3, D5, D6 as written
(replacement text below); no numeric limit is accepted until the #10862
baseline exists, so the record stays Proposed.**

1. **D1 — ACCEPT direction and rule; limit not accepted.** Evidence-5088
   halved the busiest predicate with identical result sets, but the
   after-number is 2,936 ms — 29× the proposed 100 ms p95 — so that limit is
   aspirational. #10875 must rank loads by share of total DB time, not
   frequency alone. 100 ms p95 stays Proposed pending #10862 + #10875.
2. **D2 — ACCEPT; limit not accepted.** Verified in the save path
   (`ingestion.go:321,329`, `relationship_reference_keys.go:79`,
   `package_manifest_consumption_keys.go:168`,
   `package_registry_identity_keys.go:55`). Gap: no rebuild/backfill
   equivalent found for `relationship_reference_candidate_keys`; #10875 must
   close that or stop citing that table as pattern-complete, and every new
   lookup table ships its rebuild in the same PR. The 10% limit stays
   Proposed (no measured denominator).
3. **D3 — REJECT as written.** `workload_materialization` averages 1.3 s/run
   (#10878), under the 5 s trigger, yet is 8% of handler time; "reads across
   the fleet" is undefined; the reject-if escape states no cost limit, so
   `supply_chain_impact` (51%) could exempt itself unbounded; a daily audit
   has no cost bound at 10× and no alarm threshold. Replacement text below.
4. **D4 — ACCEPT direction and rule; proof insufficient.** Both mechanisms
   exist (`activateWithConsumerWake` at `relationship_store_resolved.go:367`;
   `CrossScopeCompletionRunner` at `go/cmd/reducer/main.go:553`). A separate
   waiting table is a `fact_work_items` schema/claim change barred without
   an ADR by `go/internal/storage/postgres/AGENTS.md:713-717`; this record
   counts as that ADR only if #10876 names the migration and claim-query
   changes. #10876 must pair "% put back" with fact-to-readable freshness
   from #10862. Replacement proof below.
5. **D5 — REJECT rule boundary and limit as written.** The census refutes
   the limit: 34 graph + 13 mixed + 6 unsorted of 162, so "one answering
   store" is already false. "More than one step" is the wrong boundary.
   "Both rebuild from facts" is unproven (#10883: graph rebuild measured
   only on 67 repositories). Replacement text below.
6. **D6 — REJECT as written.** The cited precedent is `PARTITION BY HASH
   (scope_id)` MODULUS 64, not "by repository"; non-repository scopes have
   no repository key; the queue half is undecided; hash-by-scope still
   removes a snapshot by row DELETE, so the limit needs the measured
   delete+vacuum comparison; every save upserts `ON CONFLICT (fact_id)` and
   a partitioned unique index must include the partition key, so dedup
   semantics are unproven; hash parts cannot split one giant scope
   (vulnerability intelligence held 6,363 of 8,628 queued items, #10882).
   Replacement text below.

## Replacement text (applied to the design record by the coordinator)

D3 — Rule / Proposed limit / Reject if:

> - Rule: work must process changes only when any of these holds over the
>   #10862 baseline window: average run over 5 s; at least 5% of total
>   handler time; or input not bounded by the scopes that changed. Work
>   restarts only when its input changed (#8283 is the restart-on-change
>   pattern; `crossScopeCorrelationReopenDomains` is the
>   restart-without-change case to remove).
> - Proposed limit: a full audit run at most once a day per kind of work,
>   inside a stated time budget for that kind; corrections above zero are
>   reported per run, and corrections on two consecutive audits page the
>   owner as a delta-correctness defect.
> - Reject if: a kind of work cannot name what changed. It may then keep
>   full runs only under a per-domain ceiling recorded here before #10878
>   starts on it (seconds per run and runs per day at the baseline size,
>   with a review date). No ceiling recorded, no exemption.

D4 — Proof:

> - Proof: the same result whichever arrives first, the waiting work or its
>   input; plus, per #10860's queue rules, a competing-workers proof on the
>   built program against the largest realistic queue (#10862 replay)
>   showing no lost wake, no double claim, and save-transaction latency
>   unchanged within the baseline tolerance. Row equality alone is not
>   enough. Tracked in #10876 and #10877.

D5 — Rule / Proposed limit:

> - Rule: a result goes into the graph only when a declared read traverses
>   relationships with depth or fan-out not fixed at write time.
>   Fixed-depth joins, lists, counts, and filters go into a table made for
>   their reads. Both rebuild from facts; no family becomes graph-only
>   until #10883 has timed a full rebuild at 9,000+ scopes.
> - Proposed limit: every one of the 162 capabilities declares its
>   answering store or stores in the capability catalog (mixed is allowed
>   when named; the 6 unsorted are sorted first), and a check fails when a
>   read uses a store it did not declare.

D6 — Recommended / Rule / Proposed limit:

> Recommended: divide `fact_records` by `HASH(scope_id)` into a fixed
> number of parts (the `eshu_search_index_terms` precedent), after reads
> move off the fact table. The queue (`fact_work_items`) is not divided by
> this decision; #10881 must bring its own claim-fairness proof or leave
> the queue in one piece.
> - Rule: reads move off the fact table first. The conflict key that
>   identifies a fact must include `scope_id`, with a proof that global
>   `fact_id` dedup semantics do not change. Cross-scope reads keep their
>   own path, and the largest single scope's share of one part is measured
>   and recorded (hash parts do not split one scope).
> - Proposed limit: removing a retired snapshot — delete plus vacuum back
>   to steady state — costs in proportion to that snapshot on the divided
>   copy, measured against the undivided copy on the same data.

## Required proof before Status can become Accepted

1. #10862 baseline recorded and registered; each of the six limits then
   confirmed or replaced with numbers (#10861 criterion 3).
2. D3/D5/D6 replacement text merged into the record; #10881, #10882, #10883
   edited to cite the #10861 decision they depend on.
3. Phase gating: #10875/#10876 may start on accepted D1/D2/D4; #10878 waits
   for the D3 replacement; phase 3 waits for D5/D6 replacements. No
   queue-table migration before the D4 proof shape is in #10876.

The arbiter will not accept any numeric limit today (no baseline exists),
and will not bless serialization, worker reduction, a broker, or a second
database — none is needed and #10860's measurements do not support them.
QA production readings are taken from the issues' 2026-11-17 read-only
measurements; the arbiter verified code pointers and evidence-5088, not
live QA.
