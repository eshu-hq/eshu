# In-flight flag narrowing (#10972 D4b)

How a findings pass decides which in-flight passes of other scopes to flag.
This is a section of [`HOLDER_FANOUT.md`](HOLDER_FANOUT.md), split out to keep
that file under the 500-line Markdown cap.

## Why the in-flight arm exists

A pass in flight may have loaded its evidence before the source's facts were
visible, and a scope whose new generation has no finding rows yet is invisible
to the row read. So a pass that fans out also flags the claimed or running
non-marker findings passes of other scopes (`cross_scope_replay_required`);
each flagged pass reruns once at its acknowledgement.

Before D4b the arm flagged every such pass, whatever it had loaded. On the QA
environment that was most of the reruns: a long holder pass was flagged within
seconds of its claim on every run, and its rerun was flagged again (#10972,
the re-run contract ruling, re-rank decision 3).

## The two reads

A genuine pass reads the in-flight claims twice through
`InFlightClaimReader`:

1. **Set A**, `SnapshotInFlightNonMarkerClaims`, once, right after
   `loadSupplyChainImpactEvidence` returns and before the writer runs
   (`readInFlightAtLoadEnd` in `fanout_inflight_narrow.go`).
2. **Set B**, `FindInFlightNonMarkerClaims`, at fan-out, in place of the old
   scope-only read (`addInFlightHolders`).

Each claim is a scope id and the item's `last_attempt_at`. The fan-out flags a
claim in set B only when set A holds the same scope with the same
`last_attempt_at`. A claim with no recorded `last_attempt_at` is always
flagged.

The row read is unchanged. A holder that the row read finds is reopened, and
flagged if it is in flight, whatever the two sets say.

## Why it is sound

- **Claim identity.** Every claim writes a strictly greater
  `last_attempt_at` (`reducer_queue_claim_query.go`,
  `reducer_queue_batch_query.go`). A heartbeat, an acknowledgement, and the
  fan-out statements only fence on it. Equal values in both reads mean the same
  claim. A claim that was acknowledged and claimed again, or reclaimed after a
  lost lease, has a new value and is not matched.
- **Visibility.** A claim in set B and not in set A committed after the
  snapshot of the set A read. That read runs after the source's last evidence
  read, so the holder's own load starts later and sees every fact the source
  saw. This is the rule the old order already relied on for a pass claimed
  after the in-flight read; D4b applies it from the load end.
- **Wider set A.** Set A has no active-generation arm, so an item claimed
  before its generation activated is in it. A wider set A can only keep a flag.
- **Fail closed.** If the set A read fails or times out, or the pass took no
  set A read, every claim in set B is flagged, as before D4b. A failed set A
  read never fails the pass. A failed set B read still fails it.

### Precondition: one primary

The argument needs the evidence loads, the claims, and both reads on one
Postgres primary. The reducer opens one writer pool from
`ESHU_FACT_STORE_DSN`, `ESHU_CONTENT_STORE_DSN`, or `ESHU_POSTGRES_DSN`
(`runtime/data_stores.go`, `LoadPostgresConfig`) and never reads
`ESHU_POSTGRES_READ_DSN` there. `ESHU_POSTGRES_READ_ROUTING` (`stale-audits`
or `staged-audits`) opens a second pool only for the winners Auditor
(`cmd/reducer/observed_service_wiring.go`, `winners_wiring.go`).
`TestSupplyChainImpactLoadsClaimsAndFanOutShareOneHandle` in `cmd/reducer`
builds the real service with a read replica configured and checks that the fact
loader, the authorized loader, the writer, the fan-out store, the group
projection queue, and the work queue hold one handle. A proxy in front of the
DSN that splits reads to a replica would break the argument; that is a
deployment check, not a code check.

### Reads after set A

Set A must come after every read that can add to the change set from data a
later holder load might miss. The reads that run after it:

| Read | What it reads | Why it is safe |
| --- | --- | --- |
| `resolveSuppressionWorkloads` | the graph and the scope's repositories | Resolves the source's own suppression scopes; it changes the source's rows, not a fact a holder loads. The in-flight arm never carried graph changes to holders. |
| `readSuppressionCandidates`, the suppression diff pair reads | the scope's own generations and their suppression facts | Facts of generations that were committed before the current generation activated, so before the claim. Queue state only picks the baseline. |
| `tryAdmitSupplyChainImpactWrite` | the admission row of the same scope and generation | A fence, not evidence. |
| `readStoredSupplyChainImpactFindings` | the scope's own stored rows, under the conflict-domain lock | The source's own output. It names which ids changed; the facts behind the change were loaded before set A. |
| recovery `readSupplyChainImpactCVEIDs` and group ids | the rows this transaction wrote | Own output. |
| the holder reads and set B | other scopes' rows and queue state | Fan-out targeting, after set A by design. |

## Cost

One extra indexed read per genuine pass, over the passes in flight at once
(`fact_work_items_stage_domain_status_idx`, the same plan shape as set B). No
schema change, no claim-query change, no lock. A one-hop rerun takes no set A
read: it fans out only its suppression ids, and set B then keeps every claim.

The saving is bounded: it removes only the flags that land on a holder claimed
after the source's load ended. The QA shim put that at about 60% of attributed
flags (12 of 20, a small sample, from the 2026-11-24 QA shim on #10972). It
shortens rerun chains; it does not end them for long reruns.

## Signals

- `eshu_dp_supply_chain_impact_fanout_total`, one point per claim in set B:
  `outcome` `in_flight_kept` (in both sets), `in_flight_narrowed` (claimed after
  the load, not flagged), `in_flight_early_read_failed` (flagged because set A
  failed), `in_flight_early_read_skipped` (flagged by a pass that took no set A
  read), with the pass's `cause`.
- A failed set A read also counts `read_error` or `read_timeout` with
  `site=in_flight_early`, and writes the warn log
  `supply chain impact in-flight load-end read failed`.
- `eshu_dp_supply_chain_impact_fanout_read_duration_seconds` with
  `kind=in_flight_early`, one sample per set A read.
- The `supply chain impact holder fan-out` log line and the
  `supply_chain_impact.fanout` span carry `in_flight_early_read` (`ok`,
  `failed`, `skipped`), `in_flight_kept`, and `in_flight_narrowed`; the log line
  also carries `in_flight_early_read_seconds`.

An operator reads the narrowed share as
`in_flight_narrowed / (in_flight_narrowed + in_flight_kept)`. A rising
`in_flight_early_read_failed` means set A is timing out and the narrowing is
off.

## Tests

- Core, no database (`fanout_inflight_narrow_test.go`): only claims in both
  sets are flagged; a re-claim is not matched; a claim with no identity is
  flagged; a failed set A read flags every claim and the pass succeeds; a
  one-hop pass takes no set A read; set A is read before the writer.
- Live, real `ReducerQueue.Claim` and the real fan-out store
  (`storage/postgres/supply_chain_impact_fanout_inflight_two_read_live_test.go`,
  `supply_chain_impact_fanout_inflight_claims_live_test.go`): a holder claimed
  after the source's load is not flagged and its row version is unchanged; a
  holder claimed before is flagged; a holder that acknowledged and was claimed
  again is not flagged; a failed set A read flags the holder; a no-row holder
  in set A is flagged and converges; both claim reads time out under a locked
  table.

## Requeue budget (gate `supply-chain-requeue-budget`)

The tests above pin events. The requeue budget pins the fixpoint
(`storage/postgres/supply_chain_impact_quiescence_live_test.go`, arbiter
re-run contract tests A, B and E):

- `TestSupplyChainImpactQuiescesAfterOneChangeLive` (A-now): 30 holders of one
  CVE, 10 unrelated multi-CVE scopes, 6 of them with a genuine pass in flight
  from before the source's load, and 3 more claimed after the load ended and
  before the fan-out. One source change. The queue drains within 1 + 30 + 6
  executions, none of the 3 late passes reruns (D4b set A), and an identical
  rescan then runs the source pass only, changes no `reopened_at`, and stages
  at most one winners dirty fact mark per row of the rescanned generation.
  The rescan's first pass writes the same findings under new fact ids, and
  the dirty capture compares within one generation (`winners_keyed.go:198`),
  so each row re-points its winner once.
- A-D3, logged by the same run and asserted by the D3 work: no unrelated scope
  reruns in full, and unrelated finding rows keep their xmin. Today the
  flagged unrelated reruns reach nothing and run full (`scoped_seed.go:63-65`).
- `TestSupplyChainImpactSteadyArrivalsConvergeLive` (B): one changed source
  commit per tick for 30 ticks while an unrelated holder's full pass lasts 10
  ticks. The holder runs at most twice.
- `TestSupplyChainImpactRequeueBudgetCatchesTheStatusOnlyInFlightFlagLive` (E,
  seeded violation): a port whose set B is its own status-only read, whose set
  A read always fails (so every claim in B is flagged), and whose reopens name
  no changed id must make A-now and B each report a violation. The late passes
  are what A-now catches: the seed reruns them past the execution bound.

## Write amplification (today's contract)

A pass rewrites every finding row it derives, whether or not the row changed.
The finding upsert (`factwrite.BatchInsertVersionedQuery`,
`reducer/factwrite/batch_insert_versioned.go:130-146`) is `ON CONFLICT DO
UPDATE` guarded only by the fencing token and the scope, with no
content-equality condition. So an unchanged full rerun costs one row rewrite
(a new xmin and its WAL) per finding row. The stored payload also carries the
pass's `intent_id` and `cause`, which the content digest strips
(`writer_changed.go:41-54`), so the raw payload differs on every rewrite while
the digest does not. Only the winners dirty capture is gated by the digest,
so an unchanged rerun stages no dirty mark. This is write amplification by
design today. The planned bound is D3: a rerun the stamp cannot reach ends
inert and writes nothing. `TestSupplyChainImpactWriterRewritesEveryRowOfAnUnchangedFullRerunLive`
pins the contract: an unchanged full rerun keeps every row's content digest,
stages 0 dirty marks, and gives every row a new xmin. Change it only together
with this note.
