# #6296 — the Helm NornicDB pin moves to v1.2.3 by digest

`deploy/helm/eshu/values.yaml` pinned `timothyswt/nornicdb-cpu-bge` at
`v1.1.11@sha256:51b6174a…`. The operator runs **v1.2.3**. This record covers what
moving that pin does and does not establish.

## The tag is not the version

`v1.2.3` self-reports `1.2.2` internally — a running container logs
`"version":"1.2.2"`. The chart therefore pins the **digest**
`sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74`, not the
tag. Anyone bumping this again should read the version out of a running
container rather than trusting the tag name.

## Why this digest

It is the same artifact `scripts/verify-replay-tier.sh:24` already runs the R-5
replay gate against, and the same one cited in
`go/internal/query/code_relationships_nornicdb_optional_match_live_test.go:16`.
Reusing it puts the chart and the replay gate on one build instead of two.

That matters for coverage: before this change the chart's image had **no gate
coverage at all**. The B-7 golden-corpus gate and the e2e workflows all drive
`docker-compose.yaml`, which is the PR#290 source build, not the chart's image.
After it, the deployed artifact is one the replay tier already exercises.

`TestHelmNornicDBImageMatchesReplayTierGate`
(`go/internal/runtime/compose_nornicdb_image_test.go`) is what keeps that
sentence true. It compares the chart's rendered reference — repository, tag and
digest together — against `scripts/verify-replay-tier.sh`, against the
`has_nornicdb_v123_image_pin` mirror in `scripts/test-verify-replay-tier.sh`, and
against the tag the operator docs print, and it rejects a reference that carries
no digest at all. Without it, any one of those four could move alone and every
existing test would stay green while the claim quietly stopped being true.

## Before/after on the graph write and read path

Enabling bundled NornicDB puts this binary under every Cypher read and write
Eshu issues, so a version move is a runtime change and needs a number, not an
argument. Here is the number.

Both digests ran the production canonical projection writer over the committed
offline-tier cassette (`TestOfflineReplayTierGraphTruth` — cassette → real
`CanonicalNodeWriter` through the NornicDB phase-group write path → read back
and assert graph truth), then the three `sql_table` blast-radius live read tests
(`#5409`/`#6204`, the nine-branch `UNION` shape) in the same container.

Held identical across both arms: the cassette, the writer, the container
environment (copied from `scripts/verify-replay-tier.sh` — async writes off,
Heimdall, embeddings, BM25 and vector search all off; the BM25/vector warming
knobs and search-index persistence were left at their defaults, so those two
chart settings are not exercised here), a prebuilt test binary so
no Go compilation lands inside a timed window, and a **fresh container on an
empty data directory for every single run**, so neither build ever inherits
warm state from the other or from its own previous round. The two arms were
interleaved and the order alternated each round (old-first on odd rounds,
new-first on even), so drift in machine load cannot accumulate on one side.

Per-round wall-clock, seconds:

| round | order | v1.1.11 write | v1.2.3 write | v1.1.11 read | v1.2.3 read |
| --- | --- | --- | --- | --- | --- |
| 1 | old first | 5.133 | 1.226 | 0.211 | 0.236 |
| 2 | new first | 1.524 | 1.318 | 0.195 | 0.203 |
| 3 | old first | 1.639 | 1.323 | 0.341 | 0.224 |
| 4 | new first | 1.336 | 1.199 | 0.213 | 0.203 |
| 5 | old first | 1.402 | 1.188 | 0.209 | 0.205 |
| 6 | new first | 1.163 | 1.193 | 0.224 | 0.209 |
| 7 | old first | 1.218 | 1.245 | 0.188 | 0.198 |
| 8 | new first | 1.484 | 1.416 | 0.195 | 0.191 |
| 9 | old first | 1.333 | 1.176 | 0.181 | 0.197 |
| 10 | new first | 1.278 | 1.183 | 0.223 | 0.193 |
| 11 | old first | 1.300 | 1.279 | 0.187 | 0.199 |
| 12 | new first | 1.315 | 1.274 | 0.195 | 0.198 |

Medians: write `1.335 s` on v1.1.11 (`ledger:6296-replay-tier-write-wallclock-v1111`)
against `1.236 s` on v1.2.3 (`ledger:6296-replay-tier-write-wallclock-v123`);
read `0.202 s` (`ledger:6296-replay-tier-read-wallclock-v1111`) against `0.201 s`
(`ledger:6296-replay-tier-read-wallclock-v123`).

The paired per-round difference is the figure to read, because it cancels
machine load: median `-0.116 s` on the write path, new build faster, faster in
10 of 12 rounds, and the two rounds that went the other way were `+0.030 s` and
`+0.027 s` — inside the run-to-run spread of either build. With 12 paired
values the median is the mean of the 6th and 7th smallest, `(-0.137 + -0.095)
/ 2`; an earlier revision of this note reported `-0.095 s`, which is the 7th
value alone and would be the median only for an odd number of rounds. The read
path's paired median is `-0.0005 s`, which is noise; call it unchanged, not a
win.

Round 1's `5.133 s` is a load artifact, not a v1.1.11 property: the machine was
at load average 12–24 when it ran and had fallen to 1.8 by the last round. It is
left in the table rather than dropped, and the headline is a median precisely so
one such sample cannot move it. Every other v1.1.11 sample sits in `1.16–1.64 s`.

The correctness assertions are what make those timings comparable at all:
the tier graph-truth test passed in 24/24 runs (ledger:6296-replay-tier-graph-truth-passes),
and all three blast-radius reads passed in every one of the same 24 invocations. Both builds did the same work
and left the same graph, so the write-path difference is not bought by one of
them doing less. Pass counts were taken by counting `--- PASS:` lines per run,
not by reading the summary, because a `-test.run` filter that matches nothing
also exits 0.

Reproduce it:

```bash
cd go
go test -c -o /tmp/offlinetier.test ./internal/replay/offlinetier/
go test -c -o /tmp/query.test ./internal/query/
# one fresh container per run, per digest, with the verify-replay-tier.sh env:
docker run -d --name nb -p 17475:7474 -p 17688:7687 \
  -e NORNICDB_NO_AUTH=true -e NORNICDB_DATA_DIR=/data \
  -e NORNICDB_HTTP_PORT=7474 -e NORNICDB_BOLT_PORT=7687 \
  -e NORNICDB_ASYNC_WRITES_ENABLED=false -e NORNICDB_HEIMDALL_ENABLED=false \
  -e NORNICDB_EMBEDDING_ENABLED=false \
  -e NORNICDB_SEARCH_BM25_ENABLED=false -e NORNICDB_SEARCH_VECTOR_ENABLED=false \
  timothyswt/nornicdb-cpu-bge@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74
cd internal/replay/offlinetier && \
ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb \
ESHU_NEO4J_DATABASE=nornic NEO4J_DATABASE=nornic \
ESHU_NEO4J_URI=bolt://localhost:17688 NEO4J_URI=bolt://localhost:17688 \
NEO4J_USERNAME=neo4j NEO4J_PASSWORD=change-me \
/tmp/offlinetier.test -test.run '^TestOfflineReplayTierGraphTruth$' -test.count=1 -test.v; echo $?
```

Swap the digest for `sha256:51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090`
for the v1.1.11 arm, and point `/tmp/query.test` at the same endpoint with
`-test.run '^TestSQLTableBlastRadius.*Live$'` for the read arm. The A/B driver
that looped this 12 times was a throwaway shell script and is not committed;
what is committed is both test binaries' sources, so the loop is mechanical.

## What is measured, and on which build

`docs/internal/evidence/6176-grouped-semantic-retract-version-floor.md` records
the one *behaviour* measured across versions — the grouped semantic
delta-retract, 20 runs per build:

| build | reports | grouped retract |
| --- | --- | --- |
| `eshu-nornicdb-pr290:3722b483c02c` | 1.2.1 | PASS 20/20 |
| `timothyswt/nornicdb-cpu-bge:v1.2.3` | 1.2.2 | PASS 20/20 |
| `sha256:51b6174a` (the old chart pin) | 1.1.11 | **FAIL 20/20** |

So the pin moves off the only build in that table where a measured behaviour is
broken.

No-Regression Evidence: the write path is faster and the read path is unchanged
on the new digest — medians and paired per-round differences in the section
above, 12 interleaved rounds per arm on a fresh container each run, with
graph-truth assertions passing on both builds so the arms did equal work. No
Eshu code path changed: the diff is the chart's image reference, the chart
version, one new contract test, the design-doc claim a test asserts against, and
operator-facing docs that named the old version. No Cypher, query shape, writer,
worker knob, batch size, lease or concurrency setting is touched.

Observability Evidence: no metric, span, log line or status field changes. The
one operator-visible difference is the image reference the chart renders, and the
version a running container self-reports (`1.2.2`), which is already logged at
startup by the backend itself.

## What this does NOT establish

- **This is not a repo-scale throughput proof.** One lean container, one
  cassette, one writer, no Postgres and no queue. It rules out a gross
  backend-level regression on the projection write path and the blast-radius
  read shape; it says nothing about full-corpus bootstrap wall-time, reducer
  throughput under concurrent workers, or memory behaviour at scale. A
  full-corpus comparison on this digest is still unrun.
- **The `relationshipMergePropertyIdentity` capability is unmeasured on this
  build.** `deploy/helm/eshu/values.yaml` still sets it `false` and
  `templates/validate.yaml:38` still fails the render closed without it. Nothing
  in this repository states whether published v1.2.3 preserves relationship MERGE
  identity properties (orneryd/NornicDB#290), and a version comparison is not a
  measurement. The flag was deliberately not flipped.
- **The v1.1.11 workarounds stay unmeasured on 1.2.2, bar one.**
  `rg 'v1\.1\.11' go/internal/storage/cypher/` returns 154 lines across 74
  files (111 lines in the 53 non-test files), citing that version as the reason
  for a workaround. Exactly one of them — the grouped semantic retract — has a
  number on the new build. Moving the pin is not evidence that any of the rest
  can be removed; each needs its own measurement first. The same caveat now
  heads `docs/public/reference/nornicdb-pitfalls.md` and
  `docs/public/reference/nornicdb-query-pitfalls.md`, whose entries were measured
  when v1.1.11 was the chart's pin and have not been re-run on this digest.
