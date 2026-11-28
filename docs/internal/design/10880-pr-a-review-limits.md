# PR-A review limits for the winners payload move (#10880)

Refs #10880. Companion to
[the first-result-family design record](10880-first-result-family-move.md).
It records three limits an independent review of PR-A (slices 0-2b(i)) found
and what the code does about each. None of them changes a slice decision.

## The staged audit cannot see an evidence-only rewrite

The writer-backed audit (`AuditWinners`) compares the full winner row,
including `payload`, so it sees a payload change under an unchanged
`winner_fact_id`. The staged audit does not:

- `DiffCandidates` compares only `winner_fact_id`, `finding_id`,
  `priority_score`, `source_count`, and the source suppression state. It
  pins every wide field, `payload` included, by `winner_fact_id`.
- That pin assumes the same winning fact carries the same payload. An
  evidence-only rewrite breaks the assumption: the content digest is equal,
  the upsert rewrites `fact_records.payload`, and `winner_fact_id` does not
  change. The staged diff sees nothing.
- The staged stored-winners read therefore carries neither `payload` nor
  `source_confidence`. Reading them would detoast every winner each tick and
  still not close the gap.

In staged-audit mode PR-A originally guarded evidence-only rewrites through
the writer's dirty-fact capture secondary compare (`#10880` ruling decision 4).
The `#11598` Option C arbiter ruling
([`11598-epss-remark-storm-ruling-2026-11-28.md`](../arbiter/11598-epss-remark-storm-ruling-2026-11-28.md))
amended Decision 4: `dirtyIdentifiersToCapture` no longer secondary-compares
`evidence_fact_ids`, and `AuditWinners` masks `evidence_fact_ids` and
`provenance.selected_epss_score_date` on both sides of its diff. Both staged
and full audit now accept staleness on those provenance fields in
`canonical_winners` until the next semantic change, while `supports.payload`
stays fresh for Explain.

## Migration 332 retries only lock timeouts

The 332 header says the backfill's wait on a racing resweep is "bounded by the
runner's lock_timeout and retry". The runner retries only SQLSTATE 55P03
(lock not available). A SQLSTATE 40P01 deadlock between the single-statement
backfill and a concurrent keyed tick, possible while old reducers run during a
rolling deploy, fails bootstrap instead. Replay is safe: the backfill is one
implicit transaction and its `payload = '{}'` gate fills nothing twice. A
resweep's wait on a row lock is bounded by the reducer's own timeouts, not the
runner's. The header text stays as shipped because the migration checksum
manifest pins its bytes.

## Rollout window before the first v2 full resweep

Until the first contract-v2 full resweep commits, v1 keyed ticks insert new
keys at the `'{}'` payload and `'unknown'` confidence defaults. With
`ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ` on, those rows serve empty payloads
until that full resweep runs. The keyed claim re-reads the contract under the
resweep advisory lock (`winners_keyed.go`), so no v1 write lands after the v2
full resweep. The gate defaults off, and its description says to enable it only
after the reducer maintainer has populated the winners table.
