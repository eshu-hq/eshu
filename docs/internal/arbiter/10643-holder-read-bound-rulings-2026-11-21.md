# Arbiter rulings: bounding the supply-chain impact holder read (#10643)

Date: 2026-11-21. Refs #10643, #11147, epic #10860. Three rulings from a
separate-context deepest-tier arbiter, recorded here in substance by the
lane that implemented them. Evidence:
[10643-holder-read-bound.md](../evidence/10643-holder-read-bound.md).

## 11147a decision 2

The QA census behind the ruling: 231 `supply_chain_impact_fanout_failed`
dead letters and 729 more retrying. Every failure was a statement timeout
(SQLSTATE 57014) in a fan-out read, most of them in the holder read, at 14 to
36 per hour. 211 of the dead carried `fanout_owed`, meaning the writer had
committed and the holders were never told. One `EXPLAIN (ANALYZE, BUFFERS)`
of the shipped holder read for CVE-2025-27789 fetched 83,591 heap rows to
return 263 pairs: 34,542 buffers read and 7.7 s of I/O against the 10 s
guard.

- The holder-read timeouts are fixed first. They block the dead-letter
  reopen of #11147, because a reopened owed holder runs the same read.
- The class stays counting. A non-counting class would loop 10 s statements
  against the database.
- The fix is a bounded holder read, and it is a theory until measured. Two
  candidates are shimmed first at representative mass (at least 85k rows
  per id): keyset paging of a singleton hot id, and a partial index carrying
  `(cve_id, scope_id, generation_id)`.
- Bar: per statement, p95 under 2 s, max under 5 s, zero 57014. If both
  candidates pass, take the one with no new index.
- Will not do: raise the 10 s guard; lower worker counts, the holder cap, or
  the chunk size as the answer; make the fan-out class non-counting.

The coordinator accepted fact_id key-range statements as the keyset-paging
candidate: bounded pages by key, no new index, equal to the shipped read.
Ordered keyset pages were measured and rejected on total work. The covering
index stays disproven on mechanism.

## 10643b: the member arm

The shim showed the key ranges pass the bar for the cve_id and advisory_id
arms. The GIN member arm failed it cold in a 4-way burst (max 15.329 s).

1. The key ranges ship as their own PR, `Refs #10643`, not closing it. The
   member arm runs as one statement inside the same REPEATABLE READ
   snapshot. The PR body and the evidence note state the 15.329 s member
   number and that #10643 stays open.
2. The member arm gets its own second PR. The cause: the writer stores the
   whole alias component in `advisory_group_members`, so a hot id's member
   arm re-reads rows its cve arm already returned, and the lossy `jsonb_ops`
   recheck detoasts each one. The proposed fix indexes only the members that
   are not the row's own `cve_id` or `advisory_id` (foreign members), so
   cve ∪ advisory ∪ foreign equals the shipped union on one snapshot. The
   #10408 re-key holder is a foreign row by definition, so it stays
   reachable.
3. Skipping the member arm when it looks redundant is rejected: it needs the
   answer the arm exists to give.
4. Reading the member arm on a second snapshot is rejected. The flip proof
   shows a per-statement snapshot loses a lone holder.
5. The old member index and the holder-mass probe stay unchanged in these
   PRs.
6. Order: the key-range PR, then the member-arm PR (which closes #10643 once
   the post-deploy bar holds on QA), then #11147, then #11159.

M20, a read-only QA measurement, found 13,845 to 49,133 foreign rows for the
GHSA alias of each member-bearing hot id. That ruled out indexing only the
non-own members as one unranged statement.

## 10643c: the member-arm design

- The member arm becomes one GIN index on a versioned IMMUTABLE function that
  returns a token, bucket plus ':' plus member, for each member that is not
  the row's own cve_id or advisory_id. Each of the 16 key-range statements
  gets a third UNION arm that asks for its bucket's tokens, and the separate
  member statement goes away. The non-hot path, the old member index, and the
  mass probe stay unchanged.
- The bucket is `strpos('0123456789abcdef', right(fact_id, 1))`: built-in,
  documented, IMMUTABLE string functions, total with 17 values. Statement k
  asks for bucket k, and statement 1 also asks for bucket 0, so the arms
  partition the foreign rows. md5 is out (FIPS, the `fips-md5-sql-sweep`
  gate). `hashtext` is out because nothing promises its value survives a
  major upgrade, which would drop holders silently. sha256 needs the STABLE
  `convert_to`.
- The key-range change ships alone. The member-arm change stays a theory
  until exactness, token agreement for the new bucket, plan and buffer cost,
  and a 50,000-row insert cost are measured on a quiet host, within one final
  bounded window.

Will not do, both rulings: raise the 10 s guard; lower parallelism from 4;
cap or skip the member arm; read members on a second snapshot; close #10643
on the key-range PR; authorize the dead-letter replay.
