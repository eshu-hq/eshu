# #5789 — per-digest bound on the supply-chain cloud-runtime probe

## The bug, quantified

The probe promotes a finding to `deployment_truth_tier=runtime_confirmed` when a
current, authorized `CloudResource` runs the finding's subject digest. It bounded
the owner-ledger read with ONE total-row cap (200) across every digest on the
page, applied after `ORDER BY (digest, arn, uid)`.

A total cap does not share. Measured on a skewed corpus — one digest running on
30,000 resources, twenty others on 100 each, 21 digests requested:

```
                     plan                            digests   rows    exec
ARM A  Index Scan, one scan + global LIMIT 200          1/21     200   0.142 ms
ARM B  Nested Loop + per-digest Limit (loops=21)       21/21     210   0.286 ms
```

Which digest wins is deterministic but arbitrary — the lexicographically first:

```
 digest                                                                  | count
 sha256:0000000000000000000000000000000000000000000000000000000000000000 |   200
(1 row)
```

**Twenty of twenty-one findings received no runtime evidence at all.** Not a
truncated answer, a missing one — and invisible, because a finding with no
runtime evidence is indistinguishable from a finding whose image runs nowhere.

## The fix

`buildCloudResourceRuntimeDigestQuery` drives a `CROSS JOIN LATERAL` from the
distinct digest set, so each digest gets its own bounded, ordered index scan
capped at `supplyChainCloudRuntimeProbePerDigestMaxResults` (10).

Both arms stay on `graph_node_owner_cloud_resource_runtime_digest_idx` — no seq
scan, no fallback. Total work is bounded and deterministic at
`len(digests) x 10`, itself capped by `supplyChainCloudRuntimeProbeMaxDigests`.

Ten per digest is sufficient for the decision being made: `runtime_confirmed`
needs only that at least one current, authorized resource runs the digest, so a
bounded sample answers it.

No-Regression Evidence: ARM A 0.142 ms -> ARM B 0.286 ms on the skewed corpus
above (Postgres 17, `EXPLAIN (ANALYZE, BUFFERS)`). Both index-backed; the
increase is 21 bounded index scans replacing one, on a sub-millisecond query,
and it buys correct answers for 20 findings that previously got none.

Observability Evidence: no new metric or span. The probe's existing
`eshu.subject_digest_count` / `eshu.runtime_confirmed_digest_count` span
attributes become meaningful rather than misleading — before this change the
confirmed count could only ever reflect one digest per page on a skewed corpus.

## Proof

`TestCloudResourceRuntimeDigestPerDigestBoundPreventsStarvationLive` seeds one
digest on 600 resources plus twenty on 20 each, calls the production
`CurrentAuthorizedCloudResourcesByDigest`, and requires every requested digest to
be represented, no digest to exceed the bound, and two identical calls to return
identical ordered rows.

Against the pre-fix global cap it fails exactly as the bug predicts:

```
20 of 21 digests got NO runtime evidence (hot digest returned 200 rows)
```

## Why eligibility runs BEFORE the bound (codex review)

The first draft bounded rows and filtered after. That is what the original code
did, and it is wrong for the same reason at a smaller scale: a digest whose
first N `(arn, uid)` rows are stale, tombstoned, or outside the caller's grants
returns nothing even though a later row is current and authorized.

Measured on the pathological shape — one digest on 50,000 resources where only
the LAST 50 (by arn order) are authorized for a scoped caller:

```
ARM 2  bound candidates (200), then filter    0.449 ms   rows=0    <- WRONG
ARM 1  eligibility inside, bound eligible     93.193 ms  rows=50   <- correct
       (Index Scan, Rows Removed by Filter: 49950)
```

**The fast shape returns the wrong answer.** It reports a genuinely running
vulnerable image as not running, which is the failure this whole issue is about,
reached from the other direction.

So eligibility moved inside the LATERAL, before its LIMIT. The cost is real and
bounded: the scan runs until it finds K eligible rows, so the worst case is one
full index-range scan of a single digest. 93 ms on 50,000 rows sits well inside
this store's own `cloudResourceListInteractiveSLO` of 2 seconds, and the shape
that produces it — a hot digest where nearly every resource is unauthorized for
the caller — is the rare one.

A hybrid (inner candidate bound, outer eligible bound) was considered and
rejected: it reintroduces ARM 2's wrong answer for exactly the rows the inner
bound cuts. Getting both would need eligibility in the index, which does not
exist.

No-Regression Evidence: the healthy shape is unchanged — when eligible rows are
not concentrated at the end, the scan stops as soon as it has K, which is the
0.286 ms measured above. The 93 ms figure is the deliberate worst case, stated
rather than discovered later.

## Side effect on scoped callers, deliberate

With eligibility inside the bound, a scoped caller now receives a full budget of
rows it can actually see, rather than the survivors of a budget spent mostly on
rows it cannot. On the existing hot-candidate fixture that changes the scoped
result from ~75 rows to the full bound. That is more evidence, not different
evidence, and it is the direct consequence of counting the right thing.
