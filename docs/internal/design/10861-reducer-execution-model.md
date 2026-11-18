# Reducer execution model: six design decisions (#10861)

Status: **Proposed**. The arbiter ruled on 2026-11-18
([ruling](../arbiter/10861-reducer-execution-model-ruling-2026-11-18.md)):
D1, D2, D4 directions accepted; D3, D5, D6 rejected as written and replaced
by the text below; no numeric limit is accepted until the #10862 baseline
exists (criterion 3). Until Status becomes Accepted this record approves no
code, migration, contract change, deployment, or QA enable.

Refs #10861. Part of #10860 (Phase 0, step 1; phases 2 and 3 wait on
acceptance). Size target (set by the owner 2026-11-17, #10862): ten times one
organization. Every limit below is a proposed starting number; confirm each
against the #10862 baseline before it is accepted.

Supersedes Part E of
[the July 2026 performance and scale review](../architecture-review-2026-07-performance-scale.md#part-e--performance-ha-failure-modes-scale),
whose options added capacity around how the reducer works today.

Where each decision applies in the flow: saving a fact (decision 2) starts
waiting work (decision 4); work finds its input (decision 1) and processes the
change (decision 3); results land in the graph or a result table (decision 5);
facts are divided by scope hash (decision 6).

## Decision 1: how reducer work finds its input

Recommended: save lookup values when a fact is saved, and read those. Rationale:
the reducer's own code does the computing while the database's time goes into
finding what to compute on; one part of the busiest load already works this way
and its time halved (6,488 ms before, 2,936 ms after on 20,000 facts,
`evidence-5088-relationship-reference-keys.md`). Keeping per-search indexes on
fact content filled the database, and loading whole snapshots moves far more
data per work item.

- Rule: a load that runs more than once a second may not match patterns or
  search text inside fact content.
- Proposed limit: 95% of load queries finish within 100 ms on the baseline
  data set.
- Where the database still does the choosing (e.g. picking the winning
  advisory), it reads only the entries marked as changed.
- Proof: the before-and-after query plan on a realistic copy, and a comparison
  showing the same facts come back. Tracked in #10875.
- Reject if: saving the lookup values costs more database time than the
  searches they replace.

## Decision 2: who computes the lookup values

Recommended: the core computes them when it saves the fact. Rationale:
collectors are moving to their own repositories with no database access
(`eshu-ecosystem-repository-migration.md`); per-collector rules would need every
re-collect on a rule change, and reducer-side computation keeps the first run
slow. The three lookup tables that exist today
(`relationship_reference_candidate_keys`, `package_manifest_consumption_keys`,
`package_registry_identity_keys`) already work this way via `refresh...`
functions in the save path, each rebuildable on rule change
(`consumption.Rebuilder` is the rebuild pattern).

- Rule: a lookup value is computed from one fact alone, by the core, in the
  same step that saves the fact. Each lookup table has a rebuild path.
- Proposed limit: lookup values add no more than 10% to batch save time on the
  baseline data set.
- Proof: time to save a batch with and without the new values. Tracked in
  #10875.
- Reject if: a value needs more than one fact to compute. That is reducer work,
  not a lookup value.

## Decision 3: when work must process only what changed

Recommended: process changes only, with a periodic full run as an audit.
Rationale: five kinds of work use 95% of reducer working time
(`supply_chain_impact` 51%, `aws_cloud_runtime_drift` 22%,
`workload_materialization` 8%, `container_image_identity` 8%,
`eshu_search_document` 6% over 14 h of `eshu_dp_reducer_handler_duration_seconds`);
each redoes a whole repository or the fleet per item. Ten other kinds of work
already narrow to what changed. No-audit change-only would never correct a
missed change.

- Rule: work must process changes only when any of these holds over the
  #10862 baseline window: average run over 5 s; at least 5% of total handler
  time; or input not bounded by the scopes that changed. Work restarts only
  when its input changed (#8283 is the restart-on-change pattern;
  `crossScopeCorrelationReopenDomains` is the restart-without-change case to
  remove).
- Proposed limit: a full audit run at most once a day per kind of work,
  inside a stated time budget for that kind; corrections above zero are
  reported per run, and corrections on two consecutive audits page the owner
  as a delta-correctness defect.
- Proof: on the golden corpus, the same graph and result tables as a full run,
  with matching, non-matching, and unclear cases. Tracked in #10878.
- Reject if: a kind of work cannot name what changed. It may then keep full
  runs only under a per-domain ceiling recorded here before #10878 starts on
  it (seconds per run and runs per day at the baseline size, with a review
  date). No ceiling recorded, no exemption.

## Decision 4: how work waits when it is not ready

Recommended: work that is not ready is set aside, and the save that provides
its input starts it. Rationale: a third of runs (7,965 of 24,982) stop for
missing input and return on a timer, and waits can count as failures.
`activateWithConsumerWake` (#7597, with lost-start proofs in
`go/internal/storage/postgres/AGENTS.md`) already starts waiting work in the
saving step for one family; `cross_scope_completion_events` with
`CrossScopeCompletionRunner` covers the case where the finishing work cannot
know its dependents.

- Rule: waiting work is set aside with a note of what it waits for, in a
  separate table so handing out work never ranks it. A wait never counts as a
  failed try. A slow timer remains only as an audit for missed starts.
- Proposed limits: under 5% of runs put back (32% today); the audit starts a
  missed item within 10 minutes.
- Proof: the same result whichever arrives first, the waiting work or its
  input; plus, per #10860's queue rules, a competing-workers proof on the
  built program against the largest realistic queue (#10862 replay) showing
  no lost wake, no double claim, and save-transaction latency unchanged
  within the baseline tolerance. Row equality alone is not enough. Tracked
  in #10876 and #10877.
- Reject if: starting work in the saving step makes saves wait on each other.

## Decision 5: what the graph is the source of truth for

Recommended: the graph answers questions that follow relationships; result
tables answer everything else. Rationale: about a fifth of the 162 read
capabilities need the graph (34 graph, 13 mixed); most others read reducer
results saved back into the fact table. Putting everything in the graph grows a
4.9 GB store with no clustering on an edition with no running backup, and lists
and counts are slower there. Dropping the graph reloads the already-full
database. `7331-neo4j-primary-graph-backend-decision.md` chose the graph
database without saying which store answers what; this record says it.

- Rule: a result goes into the graph only when a declared read traverses
  relationships with depth or fan-out not fixed at write time. Fixed-depth
  joins, lists, counts, and filters go into a table made for their reads.
  Both rebuild from facts; no family becomes graph-only until #10883 has
  timed a full rebuild at 9,000+ scopes.
- Proposed limit: every one of the 162 capabilities declares its answering
  store or stores in the capability catalog (mixed is allowed when named;
  the 6 unsorted are sorted first), and a check fails when a read uses a
  store it did not declare.
- Proof: identical API and MCP answers before and after a family of results
  moves. Tracked in #10880, with graph rebuild time in #10883.
- Reject if: a family needs both relationship-following and filtered lists over
  the same data. That family is decided on its own.

## Decision 6: how facts and the queue are divided

Recommended: divide `fact_records` by `HASH(scope_id)` into a fixed number
of parts (the `eshu_search_index_terms` precedent), after reads move off the
fact table. The queue (`fact_work_items`) is not divided by this decision;
#10881 must bring its own claim-fairness proof or leave the queue in one
piece. Rationale: `fact_records` is 415 GB in one piece with 152 indexes
(64 parts would mean about 9,700), and old data is removed row by row; the
search index is already divided this way into 64 parts. Dividing by date
would scatter every single-repository load; a database per customer is
beyond the owner's target, which is not a hosted service.

- Rule: reads move off the fact table first. The conflict key that
  identifies a fact must include `scope_id`, with a proof that global
  `fact_id` dedup semantics do not change. Cross-scope reads keep their own
  path, and the largest single scope's share of one part is measured and
  recorded (hash parts do not split one scope).
- Proposed limit: removing a retired snapshot — delete plus vacuum back to
  steady state — costs in proportion to that snapshot on the divided copy,
  measured against the undivided copy on the same data.
- Proof: removal measured on a test copy both ways on the same data. Tracked
  in #10881.
- Reject if: the busiest loads get slower on the divided copy.

## What this does not decide

- The phase 1 fixes. They are needed under every option above.
- A message broker or a second database. The measurements do not support
  either.
- Any change to an index that serves API or MCP reads.

## Records this decision respects

- `1286-postgres-ownership-inventory.md` (ownership),
  `1289-queue-substrate-evaluation-gate.md` (the database stays the queue;
  saving a fact still queues its work in the same step),
  and "What NOT to change without an ADR" in
  `go/internal/storage/postgres/AGENTS.md` (queue-table changes need this
  record).
- `docs/internal/design/eshu-ecosystem-repository-migration.md` (collectors get
  no database, queue, or reducer handles).

## Acceptance mapping (#10861)

- Size target set (#10862, 2026-11-17): done, recorded above.
- Arbiter ruling per decision, quoted in the issue: done 2026-11-18 —
  D1/D2/D4 accepted (limits still Proposed), D3/D5/D6 replaced as above;
  see [ruling](../arbiter/10861-reducer-execution-model-ruling-2026-11-18.md).
  Status stays Proposed until criterion 3.
- Limits confirmed or replaced against the #10862 baseline: pending; fill in
  per decision.
- Proving experiment plus reject result per decision: recorded above.
- This file: this record.
- Part E marked replaced: pointer at the top of Part E.
- Phase 2 and 3 children citing their decision: #10875-#10880 cite #10861;
  #10881, #10882, #10883 do not yet.
