# #7033 exact code-topic parallel read

## Claim boundary

The built four-session code-topic read was faster than a built single-query
baseline on one preserved Neo4j/PostgreSQL test corpus. This record does not
establish the deployed ops-qa `<1 s` endpoint budget. A comparable deployed
or isolated read-replica endpoint measurement remains pending; do not infer
that the dedicated test instance represents current ops-qa load.

The query-shape theory and earlier relative-path index rollout are recorded in
[the preceding #7033 evidence](7033-unscoped-topic-relative-path-index.md).

## Earlier built API comparison on the dedicated test instance

The baseline API image was
`sha256:b5ce6d52b6a9e2f0456dfa6c73612ddebef8da9ad7a02070efe5ee5b0bd6a136`
from source `182a311246ff81dafa9fb35204bf003186a3636e`. The candidate image
was `sha256:6e62984e082e9bab8ab01d95869fa0c7bf5371b55fefd901573999055fd2fd3a`
from the independently reviewed source
`917922a0831cf6991e3dbe1dc528b0d7683edb47`. Both served the same
preserved Neo4j/PostgreSQL stack. The candidate started healthy with both
PostgreSQL DSNs carrying `default_transaction_read_only=on`; bootstrap and
OIDC session refresh were disabled. No index build was active. The retained
A/B artifacts do not include a startup-marker receipt.

The request was `POST /api/v0/code/topics/investigate`, limit 25, offset 0,
with this `topic` phrase (no explicit `terms` field):

```text
config content deployment environment file function handler module package path repository resource service source system workspace
```

After two warmups per variant, eight timed pairs alternated A/B then B/A.
The HTTP wall-time boundary included the full response body. The paired
times below are in seconds and in request order:

| Pair | Baseline A | Candidate B |
| ---: | ---: | ---: |
| 1 | 0.812228 | 0.437103 |
| 2 | 0.821174 | 0.432220 |
| 3 | 0.824270 | 0.422975 |
| 4 | 0.774759 | 0.419177 |
| 5 | 0.779787 | 0.409452 |
| 6 | 0.766343 | 0.423054 |
| 7 | 0.765251 | 0.412535 |
| 8 | 0.780511 | 0.423092 |

The baseline median was **0.780149 s** and the candidate median was
**0.423014 s**. All 16 timed requests returned HTTP 200 and 25 results with
`candidate_pool_truncated=true`. Every full canonical JSON response had the
same SHA-256 digest,
`095bfc68e8251d24b6a684901263dd5d69b8c9c55b9203cccaed5b7dde7c3e88`.
This verifies this capped request's observed output, not general capped-pool
equivalence. The contract tests cover cancellation, rollback, pool limits,
and exact assembly separately.

The content-table fingerprint was identical before and after all requests.
Its fields are `relname|reltuples|relpages|n_live_tup|n_dead_tup|n_tup_ins|n_tup_upd|n_tup_del|pg_relation_size|max(content_files.indexed_at)`:

```text
content_entities|2809457|171400|2809457|0|2808209|0|0|1404108800|2026-09-29 10:31:51.825835+00
content_files|149277|14721|155817|0|155826|0|0|125476864|2026-09-29 10:31:51.825835+00
```

The retained A/B artifact ends at the post-request fingerprint; it does not
record candidate cleanup or post-cleanup health. The measured comparison uses
the same corpus and backend instance with unchanged content-table state, not
a deployed ops-qa before/after.

## Exact-source ABBA comparison on the dedicated test instance

A later run compared baseline source
`4592e8c090b0308819e78d282e271684701a753b` and candidate source
`147971b860e0fd1e6df3e39697be715f52bd865e` using immutable API images
`sha256:58c1d7154cee12fdca879fc2735910ed059e88e21f1461d1fbfd2268d5e53db0`
and `sha256:ec923eb1c98bd48d5ffa6d3819c40dc8f839b938fde31a837608c52f77fbbe7f`.
The request and preserved Neo4j/PostgreSQL corpus matched the earlier run.
An earlier base-only rebase preserved the query patch, but the subsequent
guarded-reader integration changed its production connection path. These
image and timing claims remain bound to the pre-integration source SHAs, not
the current candidate branch.

After warmup and bounded baseline-control samples, eight interleaved ABBA
rounds yielded 16 timed requests per variant. The baseline median/p95 was
**0.768627/0.797310 s** and the candidate median/p95 was
**0.415212/0.432096 s** (nearest-rank p95). The control bound was 0.906697 s;
all timed baseline requests remained below it. All 32 timed requests returned
HTTP 200 and 25 results with `candidate_pool_truncated=true`. Their full
canonical JSON had one digest,
`095bfc68e8251d24b6a684901263dd5d69b8c9c55b9203cccaed5b7dde7c3e88`.
The six-row content/queue fingerprint before and after had the same SHA-256,
`a4c010e8bcb2e5d915b5d1b8b9125546f6d98e816030a69a0b5f68ef9ca75a73`.
No peer or load invalidation was recorded. The exact runner had SHA-256
`213bf67f897c1861bde133850897afcad319c184e13bc1302639e0c72d345a8c`;
the retained artifact is `evidence-run-paused-2327` under the private remote
validation run. This is an isolated, quiet-stack result, not a deployed result.

The owned resolver was paused only for the bounded run and subsequently
restored healthy. Temporary API containers and volumes were removed; the
preserved database volumes were retained. Earlier attempts with a path error
and an out-of-bound control sample were rejected rather than counted.

## Historical pre-permit fixed-corpus endpoint comparison, 2026-10-02

The accepted private artifact is `7033-exact-20261002-ab-1459`. An
API-only Dockerfile built baseline `abc6d3c7b4cac6ebe05907c047741f1dbefc86f6`
and pre-review-fix candidate `9b96896d595fecd700d8e4bdbc32435dee2cad1b`
with the same pinned Go toolchain and runtime image
`sha256:b5ce6d52b6a9e2f0456dfa6c73612ddebef8da9ad7a02070efe5ee5b0bd6a136`.
The resulting API images were
`sha256:376237e32cf542e3512224919c1af1560a9096066a8cf3bd09495ce69cf3d3c6`
and
`sha256:f711f1ac094619a9f344f9f277412d2ae19206a84c41d853b808aceab76e4b29`.
Both used the same preserved PostgreSQL 18/Neo4j stack, identical connection
settings, an eight-connection total pool with four readers, and isolated
loopback-only API containers. Source and the recorded single-host/four-reader
configuration imply the candidate's four-partition shared-snapshot path for
this 16-term request; no trace export directly captured the execution-mode
attribute. This is an exact-source relative result for those two SHAs on a
test stack, not an ops-qa run or a measurement of the later permit/fallback
changes.

Two warmups per variant preceded two eight-request baseline control sets.
Their medians were 0.775120 and 0.772025 s; the frozen maximum-sample
control bound was 0.878257 s. Eight alternating ABBA rounds then produced
16 timed full-body HTTP requests per variant:

| Source | Median | Nearest-rank p95 |
| --- | ---: | ---: |
| Baseline | 0.765572 s | 0.778206 s |
| Candidate | 0.464944 s | 0.473124 s |

The median saving was **0.300628 s (39.27%)**. All 52 warmup, control,
and ABBA requests returned HTTP 200 with `count=25`,
`candidate_pool_truncated=true`, and one full canonical JSON SHA-256
`095bfc68e8251d24b6a684901263dd5d69b8c9c55b9203cccaed5b7dde7c3e88`.
The content-entity row-version, content-file row-version, and content-index
catalog/state SHA-256s were identical before and after the timed window:

```text
entities 65bfccb02802b68710b1945ddba8505ed58ffd3cc7042ac83df0ac9c1476ebf6
files    c7740414bf9b0ee0827b361c5b3a645dfa47368346c667720ed6f65e55ed3cb2
indexes  58b4e9db18a383c1c33845db9a7b48f2418c5b998066fab1e02b1079fcec66b3
```

The maximum sampled host load was 2.97 on 16 logical CPUs, below the
half-CPU invalidation threshold. Candidate API memory reached 35.71 MiB
versus 15.65 MiB for baseline in the sampled series; both had a 2 GiB
limit. This is a roughly 20 MiB sampled increase, not a no-memory-cost
claim. No resource sampler invalidation occurred.

The historical medians above must not be attributed to the later guarded-reader
source; its separate exact-source comparison follows below.

Both API `/healthz` probes passed. Both `/readyz` probes returned the
same HTTP 503 solely because the preserved test database lacks the receipt
for migration 155, a story-support index on `fact_records` unrelated to
the measured content tables. A temporary SELECT/EXECUTE-only role denied
one best-effort bootstrap audit INSERT (SQLSTATE 42501) per container before
timing; no bootstrap audit row persisted, and no later store error appeared.
The containers, role, and temporary images were removed; PostgreSQL and
Neo4j remained healthy. Thus this result proves neither rollout readiness
nor audit integrity, and it does not establish the deployed ops-qa
`<1 s` budget.

## Current guarded-reader fixed-corpus comparison, 2026-10-02

The accepted private run `7033-exact-current-5d44-20261002b` compared
baseline `575ef287bfe13785639a8e4548593ccb92cf3c27` with the exact
reviewed PR source `5d44dda539ad5e2e136aa25e738aaca4910d4e50`. Both
API images were built from those Git commits with the same Dockerfile
(`sha256:031d70e15d536537b4d6be8f135af85bd0ebe2931b4837a1b8758e989130cc60`)
and runtime image
`sha256:b5ce6d52b6a9e2f0456dfa6c73612ddebef8da9ad7a02070efe5ee5b0bd6a136`.
They used the same preserved PostgreSQL 18/Neo4j stack and loopback-only
API containers, each limited to four CPUs and 2 GiB memory, with the same
eight-connection total/four-reader PostgreSQL pool settings. The canonical
16-term request, limit 25, offset 0, was unchanged. This is a dedicated
test-instance comparison, not deployed ops-qa acceptance.

Two warmups per variant preceded two eight-request baseline control sets.
Their medians were 0.781043 and 0.768598 s; the frozen control bound was
0.878429 s. Eight alternating ABBA rounds then produced 16 timed full-body
HTTP requests per variant:

| Source | Median | Nearest-rank p95 |
| --- | ---: | ---: |
| Baseline | 0.797406 s | 0.861517 s |
| Current candidate | 0.484409 s | 0.507398 s |

The ratio-of-medians saving was **0.312997 s (39.25%)** on this fixed corpus.
The eight same-round ABBA saving ratios were 39.4064%, 39.3548%, 40.3558%,
41.0330%, 40.4396%, 39.2943%, 39.5571%, and 38.3997%; their mean was
39.7301% with a sample SD of 0.8305 percentage points. All 52
warmup, control, and ABBA requests returned HTTP 200 with `count=25`,
`candidate_pool_truncated=true`, and the same full canonical JSON SHA-256,
`095bfc68e8251d24b6a684901263dd5d69b8c9c55b9203cccaed5b7dde7c3e88`.
The content-entity row-version, content-file row-version, and index
catalog/state fingerprints matched before and after; their respective
SHA-256s were `65bfccb02802b68710b1945ddba8505ed58ffd3cc7042ac83df0ac9c1476ebf6`,
`c7740414bf9b0ee0827b361c5b3a645dfa47368346c667720ed6f65e55ed3cb2`,
and `58b4e9db18a383c1c33845db9a7b48f2418c5b998066fab1e02b1079fcec66b3`.
No baseline request exceeded the control bound. Maximum sampled host load
was 3.5 on 16 logical CPUs, below the half-CPU invalidation threshold.
Sampled API memory peaked at 15.06 MiB baseline and 34.06 MiB candidate,
a 19.00 MiB increase within each container's 2 GiB limit.

Both canaries had the same known missing-migration-155 `/readyz` response and
one denied best-effort bootstrap audit insert each, with no persisted audit
row or later store error. The restricted temporary role, canaries, images,
source worktrees, and credential file were removed after the successful run;
the preserved databases remained healthy. The result satisfies the
current-source relative merge measurement, but **does not** establish the
deployed ops-qa `<1 s` budget or rollout readiness.

## Deployed ops-qa endpoint-only diagnostic, 2026-10-02

Performance Evidence: an isolated two-container Pod compared baseline
`f5400edbe56e67cfa31b9f7b8a5e033da5a25372` with candidate
`f932f95056163bc42aa3a895f88451776b051783` on the same pinned ops-qa
PostgreSQL read replica and Neo4j backend. Each API container had a 1 CPU /
2 GiB limit. The request was the canonical 16-term topic phrase above with
limit 25 and offset 0. Both APIs returned HTTP 200 and the same complete
response digest on all eight timed requests. The user-local run artifact is
`7033-ab-20261002t010135z-2be77501-measure2`.

Only two of eight planned ABBA rounds completed. The four baseline times were
`1.188434, 1.255622, 1.165841, 1.171272` seconds (descriptive median
`1.179853 s`); the four candidate times were `0.745463, 0.811079,
0.725290, 0.740511` seconds (descriptive median `0.742987 s`). The primary
PostgreSQL CPU then exceeded the run's 70% admission gate, so the harness
stopped before round three. These incomplete medians are diagnostic, not a
before/after acceptance comparison or a `<1 s` result.

Both `/readyz` calls returned the known missing-migration-153 HTTP 503, so
rollout readiness was not established. Replica replay advanced across every
timed call. The sampled content-table statistics fingerprint changed after
round one, then stayed equal, but those estimated statistics and the capped
response digest do not prove an immutable corpus or index state. The Pod was
deleted with its UID precondition and verified absent; the temporary Secret
and SELECT/EXECUTE-only role were removed and verified absent on PostgreSQL
primary and replica. No Service selected the Pod, and no schema DDL ran.

Observability Evidence: the candidate sets `code_topic.execution_mode` and
parallel-read connection/fallback span attributes in code; this aborted run
did not capture a trace export or a completed resource series. No deployed
operator-signal claim follows from these samples.

## Reader-host safety proof

The guarded reader advertises shared snapshot sets only for a single native
PostgreSQL reader host. A multi-host reader retains the single-statement path:
PostgreSQL exported snapshots are server-local, while the normal reader pool
can select a different candidate for each connection. A disposable PostgreSQL
18.6 experiment imported an exported snapshot on its origin server and failed
with `snapshot ... does not exist` on a second server. The multi-host capability
regression was red before the guard and green after it; the ordinary reader
host-selection policy was unchanged. This is a correctness guard, not a
multi-host parallel performance claim.

## Review-fix guarded-reader correctness proof, 2026-10-02

At source `dffc84d2700c01e53781e391a3867027e5671476`, the opt-in
`TestInvestigateCodeTopicGuardedParallelPostgresLive` passed against a
temporary, isolated PostgreSQL 18.6 single-primary container (image
`sha256:d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2`).
The test ran the production guarded reader with four reader permits, compared
unscoped, grant/language, null-language, repository, and paginated responses
with the serial SQL, then held one read-only reader while the four-reader
reservation expired. The pressure subtest passed in 2.06 s: the result
matched the serial path, three partial readers and one fallback reader were
observed, and the pool returned to zero in-use connections after release.
The full test exited 0 in 4.25 s. Its generated proof database was absent
before teardown; the temporary container and storage were removed and
verified absent. This tiny disposable fixture proves fallback correctness and
cleanup, not representative latency, replica replay, or the deployed budget.
The subsequent clean rebase onto `b872673df44c546dbaea7b7491ac4ae91af3ae13`
kept the cumulative patch ID
`bff2709b7f3ac9033218ebea8bdc1a76de3c7747` unchanged, with no changed-path
overlap; the local live test was not rerun on the rebased commit.

## Deployed ops-qa acceptance

NOT_CHECKED for the current guarded-reader candidate on the deployed
topology. The current-source fixed-corpus comparison above is accepted as
the relative merge measurement, not deployed endpoint acceptance. The current
source includes migration 155. Ops-qa's migration ledger was last observed at
152 on 2026-10-02; this record does not establish a later ledger state. An
earlier exact-source attempt on 2026-10-02 aborted during
storage-digest/cleanup validation and yielded no accepted endpoint timing
evidence. The owner approved using the fixed-corpus remote comparison for
the merge measurement, with the deployed
ops-qa `<1 s` check tracked separately in #7516. Do not claim the deployed
budget from this remote result.
