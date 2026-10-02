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

## Deployed ops-qa acceptance

NOT_CHECKED for the final rebased guarded-reader candidate on the deployed
topology. The patch was rebased onto `42d2ff84ebf56d75cf323e39d7aa4081da23ab00`
without changing its stable patch ID, but the binaries above predate that
base. Ops-qa's migration ledger still ended at 152 on 2026-10-02 while the
rebased source includes 154. A new exact-source run needs fully ready APIs,
a quiet completed interleaved comparison, full-response parity, content- and
index-specific storage proof, resource and operator-signal evidence, and
verified teardown. Do not claim the deployed `<1 s` budget or open the #7033
PR on this record alone.
