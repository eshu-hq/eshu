# Holder fan-out and convergence (#9695)

This is the detailed contract behind the "Convergence: the holder fan-out"
bullet in `README.md`. The cost of the in-flight read and the signals an
operator reads are stated in the README bullet "In-flight cost"; measurements
and the defects found while proving it are in
`docs/internal/evidence/9695b-holder-fanout.md`, and the 1.5a probes in
`docs/internal/evidence/9695-advisory-retraction.md`.

The winners table keeps one copy per
canonical key, ordered by `priority_score`, then whether the row carries a
payload finding id, then `fact_id`, and another scope may hold its own copy
(KEV or EPSS enrichment, another advisory source, the operator suppression
scope). A copy only changes when its scope reruns, so a retraction, or its
reversal, used to reach the other copies on that scope's own cadence. Now, after
the writer commit and before the queue acknowledgement, a pass that changed or
retracted rows reopens the `supply_chain_impact` item of every scope that holds
an active copy of those CVEs (`fanout.go`, `writer_changed.go`, and the store in
`storage/postgres/supply_chain_impact_fanout.go`). The reopened pass
re-derives its copy, so a retracted CVE reads `advisory_retracted` in a holder's
copy once that holder's next pass runs, and a restored one returns each copy to
its pre-retraction state: `active`, or hidden for an operator's decision when
one applies. There is no precedence rule in the winners query. A holder's lag
is queue latency plus one holder pass plus one winners resweep, and a CVE with
more than 256 holders drains across claims.

## Rules

- A row is changed when it is new, revived, or its `provenance.content_digest`
  differs, and so is a finding the baseline had that this pass lacks. The
  baseline is the generation's own rows on a rerun and the scope's previous
  generation, matched by stable key, on the first pass of a new generation, so
  a rescan that derived the same findings changes nothing. The digest hashes
  the typed payload without `intent_id`, `cause`, `generation_id`, and
  `evidence_fact_ids`, and includes `suppression_state`. Rows written before
  the digest existed count as changed once.
- Holders come from one indexed read per 200 changed CVEs
  (`fact_records_supply_chain_impact_lookup_idx`), the asking scope excluded,
  the operator scope included, plus the scopes whose item at their active
  generation is claimed or running (`fact_work_items_stage_domain_status_idx`),
  read first: a pass in flight may have loaded evidence before this pass's facts
  were visible, and a new generation with no finding rows is invisible to the
  row read. In this order a holder that acknowledges between the reads has
  committed its rows.
- One hop: only the attempt a fan-out created is one hop (attempt 1; the reopen
  and the replay both reset the count to 0). That pass re-derives its copy,
  fans out nothing, and consumes the stamp. A stamp that reaches a later attempt
  was written while a genuine pass ran and failed, so the retry is genuine; an
  owed marker also makes a pass genuine. A continuation is not a hop: a stamped
  drain claim drains the stored list.
- A claim reopens at most 256 holders; the rest is stored as `fanout_remaining`
  on the committing item and drained by its next claims.
- Owed marker: when a commit changed rows, the writer sets `fanout_owed` on the
  pass's own item inside the same transaction, fenced on the lease and the
  attempt, so a pass that lost its lease cannot commit. The fan-out clears it
  when the last holder is reopened. A retry that finds the marker knows an
  earlier attempt committed and fans out for every CVE the scope names; a retry
  after a failure before the commit fans out only its own changes.
- A failed holder read, in-flight read, reopen, or state update fails the pass
  with a retryable error of class `supply_chain_impact_fanout_failed`; the
  queue retries it, then dead-letters it.
- Known limit, not introduced here: a scope that loaded its evidence before
  the source's facts were visible, derived no finding row for the CVE, and
  acknowledged before the in-flight read is not a holder; nothing reruns it
  until its own next generation. Current collectors can do this (KEV, EPSS, and
  alert scopes derive their own copies); the source scope's pass derives the
  finding from the same evidence, so the missing copy does not hide the
  finding.
