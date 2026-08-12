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
