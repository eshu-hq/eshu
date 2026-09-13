# #6590: OCI registry-truth key batches are chunked at the recorded bound

## What was wrong

`go/internal/queryplan/testdata/query-source-coverage.yaml` registers the three
OCI registry-truth fetchers in `go/internal/query/impact/trace_deployment_oci.go`
as `keyed_support` / `bounded_key_batch` with `max_keys: 250`. Nothing in the
code enforced that number. The key set is every distinct image reference found
across the traced workloads, so 50 workloads with 6 images each already produce
300 keys. The fetchers sent the whole set as one `IN $digests` /
`IN $image_refs` / `IN $repository_ids` list, which made the recorded bound
false for any trace above 250 distinct images.

## Where the bound belongs

The keys come from `collectContainerImages` in the YAML parser. Capping there
would drop facts at ingest. Truncating the key list in the query would silently
lose registry truth for the images past the cap. Both hide data. The fix instead
splits the key list on the query side into statements of at most
`ociMaxKeysPerStatement` (250) keys and merges the results, so every key is
still queried exactly once and `max_keys: 250` becomes true by construction.

Each fetcher keeps its own `reader.Run` call with unchanged Cypher text; only
the bound parameter value differs per batch. `ociKeyBatches` is a pure splitter
with no graph access, so the query-source coverage registry still attributes
each callsite to the function that runs it.

## Proof

- RED (before the fix): `TestFetchOCIImageRegistryTruthBatchesDigestKeysWithinRecordedBound`
  and `TestFetchOCIImageRegistryTruthBatchesTagRefsWithinRecordedBound` feed 300
  distinct keys and failed with a statement carrying 300 keys, want <= 250.
- GREEN (after): both pass; each asserts every statement carries at most 250
  keys and every input key is queried exactly once.
- `go test ./internal/query/impact/ -count=1`: ok.
- `go test ./internal/queryplan/ -count=1`: ok after re-recording the three
  `source_sha256` values for `fetchOCIImageTagRows`, `fetchOCIImagesByDigest`
  and `fetchOCIRepositoriesByUID`. The class, key bound and result bounds are
  unchanged.
- `go vet ./internal/query/impact/`: clean.

## Live-backend measurement

No-Regression Evidence: measured on the remote host against the pinned NornicDB
build, driving the production read path in both arms. At and below the recorded
250-key bound the two arms issue identical statement and session counts and are
wall-time indistinguishable. Above the bound the batched arm issues 2x-4x the
statements and sessions and is never slower beyond run-to-run spread; on the
digest path it is reproducibly faster at n=1000 (0.0684 s -> 0.0473 s, i.e.
68.4 ms -> 47.3 ms, -28.9%; and 0.0646 s -> 0.0529 s, 64.6 ms -> 52.9 ms,
-20.5%, across two independent passes). The extra per-batch deadlines do not
bite: no single statement in either arm anywhere in the matrix exceeds 0.0343 s
(34.3 ms), 0.34% of one 10 s read deadline — and that peak is a digest n=250
*after*-arm rep, a cell where the batched code issues the identical single
statement, so it records run-to-run spread rather than a cost of batching (the
largest before-arm statement is 0.0288 s, digest n=1000). Where batching
actually splits the key list (n > 250), the worst single statement falls in 11
of the 12 before/after pass pairs, by 1.44x (digest n=300 p1, 0.0079 s ->
0.0055 s) up to 4.08x (tag n=300 p1, 0.0049 s -> 0.0012 s; tag n=1000 p1,
0.0204 s -> 0.0050 s); at n=1000 it falls 3.00x/2.92x on the digest path
(0.0288 s -> 0.0096 s and 0.0251 s -> 0.0086 s) and 4.08x/3.12x on the tag path
(0.0204 s -> 0.0050 s and 0.0203 s -> 0.0065 s). The one splitting pair that
goes the other way is digest n=600 p2, where it rises 1.90x (0.0173 s ->
0.0329 s). `absolute_target_applicable: false` — this
is a same-machine relative comparison of two arms on one backend, not a
corpus wall-time target.

An earlier revision of this document argued that no live measurement was needed
"because the per-key work and statement shape are unchanged". That reasoning was
unsound and the review finding against it was correct: per-*key* work says
nothing about per-*statement* overhead. Each `reader.Run` opens a fresh session
(`newReadSession`) under its own `context.WithTimeout(defaultGraphReadTimeout)`
with `maxGraphReadAttempts = 2`, and `fetchOCIImagesByDigest` issues one
statement per entry in `ociImageLookupLabels` (3 labels), so above 250 keys the
batched path genuinely multiplies sessions, round trips and deadline ceilings.
The mechanism is real. What the measurement adds is its magnitude, and at these
key counts the answer is that it costs nothing.

### Environment

| field | value |
|---|---|
| run dir | `6590-oci-batch-perf-20260913T121305Z` |
| machine | remote Linux x86_64, 16 logical CPUs, 123 GiB RAM, load avg 1.2 at start |
| Go | go1.26.2 linux/amd64 |
| BEFORE arm | `origin/main` @ `81fc4f31771bb75d3927da872652fb4879e2289c` (detached worktree, clean) |
| AFTER arm | `claude/6590-chunk-oci-key-batches` @ `b7d5341f6b16770ec35cfa1bbec51031395c51af` (pushed head, detached worktree, clean) |
| backend image | `eshu-nornicdb-pr290:3722b483c02c` (image id `sha256:6da649670a80`) |
| backend self-report | `CALL dbms.components()` -> `NornicDB`, versions `["1.2.1"]`, edition `community` |
| backend env | `NORNICDB_NO_AUTH=true`, embeddings off, BM25 off, vector off, async writes off, `GOMEMLIMIT=48GiB` |
| ports | bolt `127.0.0.1:17690`, http `127.0.0.1:17475`, both free beforehand |
| absolute_target_applicable | false (same-machine relative comparison) |

The measured AFTER arm is the pushed head
`b7d5341f6b16770ec35cfa1bbec51031395c51af`, not a local worktree; the branch was
rebased afterwards without touching the fetchers.

### Harness

Not a re-implementation of the statements. The harness drives the **production**
path: a test in package `query` inside each arm's own worktree calls
`impact.FetchOCIImageRegistryTruth` through
`query.NewNeo4jReader(driver, "nornic")`, i.e. the real read policy — fresh
session per attempt via `newReadSession`, an independent
`context.WithTimeout(defaultGraphReadTimeout)` per `Run`,
`maxGraphReadAttempts = 2`. A thin counting wrapper sits *above* the production
reader and records per-statement shape, key count, rows and seconds; it changes
nothing below it. Sessions equal statements in the table below because every
call completed on its first attempt (no retry, no deadline) and `runReadAttempt`
opens exactly one session per attempt.

The three Cypher constants are byte-identical between the arms, so the only
difference measured is the Go loop that splits the key list:

| constant | md5 (both arms) |
|---|---|
| `ociImageByDigestCypher` | `60892d18cfcefa1c3b8b38120dbb68d6` |
| `ociRepositoryByUIDCypher` | `38d78862e9351302951b038a90633d5c` |
| `ociTagObservationByRefCypher` | `10fd3300074b30d4e97e3ad392f46f2a` |

Corpus seeded schema-first (the four property indexes and the uid constraint
`go/internal/graph/schema_tables_indexes.go` declares for these labels), then
data, with counts asserted before any timing: `ContainerImage` 24000 (4000
addressable + 20000 background), `ContainerImageIndex` 1000,
`ContainerImageDescriptor` 1000 (carries `descriptor_id`, so the `coalesce` arm
is exercised), `ContainerImageTagObservation` 24000, `OciRegistryRepository`
4000. Positive control before timing: `FetchOCIImageRegistryTruth` on two digest
refs and one tag ref returned 5 fully-populated truth rows spanning all three
image labels and both the digest and tag paths — an empty result would be
indistinguishable from a working query over an empty graph. Key windows differ
per repetition (rep `r` uses keys `[r*1000, r*1000+n)`) so no repetition is
served from the backend's parameter-value query cache, and are identical between
arms, which is what makes the row-equality check meaningful.

### Per-cell results

Container restarted before every pass; two independent passes per arm (`p1`,
`p2`), 3 repetitions per cell, all three shown.

| ref kind | n | arm | statements | sessions | rows | truth hash | median wall (s) | all 3 reps (s) | slowest single statement (s) |
|---|---|---|---|---|---|---|---|---|---|
| digest | 250 | before p1 | 4 | 4 | 376 | 3bd53c15… | 0.0219 | 0.0241, 0.0219, 0.0214 | 0.0089 |
| digest | 250 | before p2 | 4 | 4 | 376 | 3bd53c15… | 0.0223 | 0.0245, 0.0223, 0.0214 | 0.0088 |
| digest | 250 | after p1 | 4 | 4 | 376 | 3bd53c15… | 0.0232 | 0.0246, 0.0232, 0.0211 | 0.0087 |
| digest | 250 | after p2 | 4 | 4 | 376 | 3bd53c15… | 0.0251 | 0.0519, 0.0251, 0.0226 | 0.0343 |
| digest | 300 | before p1 | 4 | 4 | 450 | 5e25676a… | 0.0220 | 0.0220, 0.0223, 0.0204 | 0.0079 |
| digest | 300 | before p2 | 4 | 4 | 450 | 5e25676a… | 0.0255 | 0.0218, 0.0268, 0.0255 | 0.0101 |
| digest | 300 | after p1 | 8 | 8 | 450 | 5e25676a… | 0.0128 | 0.0133, 0.0128, 0.0123 | 0.0055 |
| digest | 300 | after p2 | 8 | 8 | 450 | 5e25676a… | 0.0141 | 0.0155, 0.0141, 0.0135 | 0.0066 |
| digest | 600 | before p1 | 4 | 4 | 900 | 14274e6a… | 0.0411 | 0.0411, 0.0407, 0.0411 | 0.0153 |
| digest | 600 | before p2 | 4 | 4 | 900 | 14274e6a… | 0.0423 | 0.0487, 0.0423, 0.0409 | 0.0173 |
| digest | 600 | after p1 | 12 | 12 | 900 | 14274e6a… | 0.0398 | 0.0352, 0.0398, 0.0404 | 0.0088 |
| digest | 600 | after p2 | 12 | 12 | 900 | 14274e6a… | 0.0384 | 0.0378, 0.0384, 0.0637 | 0.0329 |
| digest | 1000 | before p1 | 4 | 4 | 1500 | 368fdd4c… | 0.0684 | 0.0684, 0.0688, 0.0639 | 0.0288 |
| digest | 1000 | before p2 | 4 | 4 | 1500 | 368fdd4c… | 0.0646 | 0.0646, 0.0646, 0.0625 | 0.0251 |
| digest | 1000 | after p1 | 16 | 16 | 1500 | 368fdd4c… | 0.0473 | 0.0555, 0.0473, 0.0472 | 0.0096 |
| digest | 1000 | after p2 | 16 | 16 | 1500 | 368fdd4c… | 0.0529 | 0.0529, 0.0535, 0.0527 | 0.0086 |
| tag | 250 | before p1 | 5 | 5 | 250 | 8191bbad… | 0.0094 | 0.0110, 0.0094, 0.0088 | 0.0059 |
| tag | 250 | before p2 | 5 | 5 | 250 | 8191bbad… | 0.0097 | 0.0099, 0.0095, 0.0097 | 0.0061 |
| tag | 250 | after p1 | 5 | 5 | 250 | 8191bbad… | 0.0088 | 0.0090, 0.0087, 0.0088 | 0.0054 |
| tag | 250 | after p2 | 5 | 5 | 250 | 8191bbad… | 0.0103 | 0.0108, 0.0102, 0.0103 | 0.0060 |
| tag | 300 | before p1 | 5 | 5 | 300 | 85667ac9… | 0.0090 | 0.0099, 0.0090, 0.0085 | 0.0049 |
| tag | 300 | before p2 | 5 | 5 | 300 | 85667ac9… | 0.0090 | 0.0097, 0.0090, 0.0083 | 0.0049 |
| tag | 300 | after p1 | 10 | 10 | 300 | 85667ac9… | 0.0068 | 0.0069, 0.0067, 0.0068 | 0.0012 |
| tag | 300 | after p2 | 10 | 10 | 300 | 85667ac9… | 0.0087 | 0.0087, 0.0096, 0.0083 | 0.0017 |
| tag | 600 | before p1 | 5 | 5 | 600 | 62219298… | 0.0229 | 0.0185, 0.0230, 0.0229 | 0.0126 |
| tag | 600 | before p2 | 5 | 5 | 600 | 62219298… | 0.0215 | 0.0206, 0.0215, 0.0215 | 0.0129 |
| tag | 600 | after p1 | 15 | 15 | 600 | 62219298… | 0.0166 | 0.0164, 0.0167, 0.0166 | 0.0051 |
| tag | 600 | after p2 | 15 | 15 | 600 | 62219298… | 0.0205 | 0.0201, 0.0205, 0.0230 | 0.0062 |
| tag | 1000 | before p1 | 5 | 5 | 1000 | 393caa07… | 0.0333 | 0.0363, 0.0327, 0.0333 | 0.0204 |
| tag | 1000 | before p2 | 5 | 5 | 1000 | 393caa07… | 0.0299 | 0.0333, 0.0299, 0.0286 | 0.0203 |
| tag | 1000 | after p1 | 20 | 20 | 1000 | 393caa07… | 0.0258 | 0.0262, 0.0258, 0.0255 | 0.0050 |
| tag | 1000 | after p2 | 20 | 20 | 1000 | 393caa07… | 0.0333 | 0.0329, 0.0333, 0.0336 | 0.0065 |

`rows` and the truth-row hash are identical between arms for every
(ref kind, n, repetition) — all 24 cells match. That correctness agreement is
what makes the timings comparable.

### Arm-to-arm summary

| ref kind | n | before median p1/p2 (s) | after median p1/p2 (s) | after vs before (best/worst) | statements before -> after |
|---|---|---|---|---|---|
| digest | 250 | 0.0219 / 0.0223 | 0.0232 / 0.0251 | +4.9% / +13.6% | 4 -> 4 |
| digest | 300 | 0.0220 / 0.0255 | 0.0128 / 0.0141 | -46.1% / -40.8% | 4 -> 8 |
| digest | 600 | 0.0411 / 0.0423 | 0.0398 / 0.0384 | -7.9% / -4.5% | 4 -> 12 |
| digest | 1000 | 0.0684 / 0.0646 | 0.0473 / 0.0529 | -28.9% / -20.5% | 4 -> 16 |
| tag | 250 | 0.0094 / 0.0097 | 0.0088 / 0.0103 | -7.5% / +7.5% | 5 -> 5 |
| tag | 300 | 0.0090 / 0.0090 | 0.0068 / 0.0087 | -23.7% / -2.8% | 5 -> 10 |
| tag | 600 | 0.0229 / 0.0215 | 0.0166 / 0.0205 | -25.3% / -7.7% | 5 -> 15 |
| tag | 1000 | 0.0333 / 0.0299 | 0.0258 / 0.0333 | -18.4% / +5.5% | 5 -> 20 |

Every individual repetition in the whole matrix, both arms, completes in
0.0067-0.0688 s (6.7-68.8 ms); the 32 per-cell medians span 0.0068-0.0684 s
(6.8-68.4 ms).

### Per-statement cost vs IN-list size

Isolated probe on the same production reader and session-per-call policy, sizes
interleaved so no size is systematically first, key windows in range and
distinct per repetition, with a hard `rows == keys` assertion as the denominator
check:

| IN-list keys | samples | rows | median | µs/key |
|---|---|---|---|---|
| 250 | 18 | 250 | 1.466 ms | 5.86 |
| 500 | 18 | 500 | 2.798 ms | 5.60 |
| 1000 | 18 | 1000 | 4.693 ms | 4.69 |

Per-statement cost is close to linear in key count with a small fixed overhead,
slightly *sub*-linear per key. On that curve alone, splitting 1000 keys into
four 250-key statements should cost about +1.2 ms of statement time, inside the
run-to-run spread of a single cell.

### What the measurement answers

1. **At n <= 250, are the arms identical in statements, sessions and wall
   time?** Yes. Digest n=250: 4 statements and 4 sessions on both arms. Tag
   n=250: 5 and 5 on both. Wall time is inside run-to-run spread in both
   directions (digest +4.9%/+13.6%, tag -7.5%/+7.5% across two passes, against a
   BEFORE-arm pass-to-pass drift of up to 16% on the same cells).
   `ociKeyBatches` returns a single batch for a key set within the bound, so the
   statement issued is literally the one the old code issued.
2. **Above 250, what does batching cost, and how does it scale from 300 to 1000
   keys?** It does not cost; it saves, and the saving does not decay as n grows.
   Digest: -46.1%/-40.8% at 300, -7.9%/-4.5% at 600, -28.9%/-20.5% at 1000,
   while statements go 4 -> 8 -> 12 -> 16. Tag: -23.7%/-2.8% at 300,
   -25.3%/-7.7% at 600, -18.4%/+5.5% at 1000, statements 5 -> 10 -> 15 -> 20.
   Tag n=1000 is the only cell where one pass showed the batched arm marginally
   slower (+5.5%), inside the BEFORE arm's own drift band.
3. **Does the per-batch 10 s deadline create a worst-case ceiling that
   matters?** No. Batching lowers the worst single statement in 11 of the 12
   before/after pass pairs where it actually splits the key list, by 1.44x to
   4.08x: from 0.0288 s to 0.0096 s (28.8 ms -> 9.6 ms, digest n=1000) and from
   0.0204 s to 0.0050 s (20.4 ms -> 5.0 ms, tag n=1000). The twelfth pair,
   digest n=600 p2, rises 1.90x (0.0173 s -> 0.0329 s, 17.3 ms -> 32.9 ms),
   still 0.33% of one deadline. No single statement in either arm anywhere in
   the matrix exceeds 0.0343 s (34.3 ms), 0.34% of one deadline, and that peak
   sits in the digest n=250 *after* arm, where batching issues the identical
   single statement. N batches do mean N independent 10 s
   deadlines and N sessions rather than one shared budget — the review's
   mechanism is exact — but the quantity it governs sits 3-4 orders of magnitude
   from the ceiling. The worst case it creates is a longer *total* budget
   (16 x 10 s rather than 4 x 10 s at digest n=1000), a liveness concern only
   against an already-pathological backend, where the unbatched arm is failing
   too, just in fewer and larger pieces.
4. **Is the cost acceptable against the alternative?** Yes. The alternative is a
   single statement that violates the `max_keys: 250` bound the query-plan
   registry records, and the measurement shows the recorded bound costs nothing
   to honour. Nothing here justifies raising or removing the 250 bound, and
   nothing justifies truncation: the truth-row hashes prove batching returns
   exactly the same answer.

### Not built on

The same 1000-key `ContainerImage` statement measured 4.7 ms in the isolated
probe but 14.9 ms (median of 6) when it ran as the single large statement inside
the BEFORE arm's cells. That gap was not chased to a cause. It does not change
the answer either way — on the isolated curve batching costs ~1 ms, on the
in-call curve batching is faster, and both leave the cost three orders of
magnitude below one read deadline. The end-to-end cell table, which is
arm-controlled and reproduced across container restarts, is the load-bearing
evidence; the isolated statement curve is supporting only.

No-Observability-Change: the fetchers emit no new or changed metrics, spans, or
logs. The existing trace-deployment handler telemetry covers the path as before.
The measurement harness added no production instrumentation; its counting
wrapper lives in the test arms only and is not part of the diff.

## Related finding, not changed here

`enrichBlastRadiusTiers` (`go/internal/query/impact/blast_radius.go`) also sends
an unchunked key list, but its key set is bounded upstream by the response
limit. Its consumer keeps the last tier row per repository and the tier read has
no `ORDER BY`, so if a repository ever carried more than one tier the result
would be nondeterministic. Today nothing writes `Tier` nodes (only the
uniqueness constraint in `go/internal/graph/schema_tables.go` exists), so the
case cannot occur yet. The first `Tier` writer should enforce one tier per
repository or the read should gain an ordering.
