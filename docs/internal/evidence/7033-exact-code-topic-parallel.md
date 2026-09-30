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
The production patch is unchanged by the subsequent base-only rebase, as
verified with `git range-diff`; these image and timing claims remain bound to
the pre-rebase source SHAs, not the rebased branch SHA.

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

## Deployed ops-qa acceptance

NOT_CHECKED for the candidate on the deployed topology. The last recorded
ops-qa baseline median for this 16-term endpoint was 1.632480 s; no matched
candidate measurement exists there. The live PostgreSQL pod exceeded the
previously agreed memory gate during attempted canary preflight. Do not claim
the deployed `<1 s` budget or open the #7033 PR on this record alone. Add a
matched interleaved endpoint measurement, full-response parity, storage
fingerprint, resource pressure, and exact cleanup result before publication.
