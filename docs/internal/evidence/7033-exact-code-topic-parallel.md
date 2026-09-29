# #7033 exact code-topic parallel read

## Claim boundary

The built four-session code-topic read is faster than the deployed single-query
route on one preserved Neo4j/PostgreSQL test corpus. This record does not yet
establish the deployed ops-qa `<1 s` endpoint budget. The ops-qa comparison is
pending completion of its package-consumption backfill and a quiet-load gate.

The query-shape theory and earlier relative-path index rollout are recorded in
[the preceding #7033 evidence](7033-unscoped-topic-relative-path-index.md).

## Built API comparison on the dedicated test instance

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

## Deployed ops-qa acceptance

NOT_CHECKED for candidate `917922a0831cf6991e3dbe1dc528b0d7683edb47`.
Do not claim the `<1 s` endpoint budget or open the #7033 PR on this record
alone. Add the approved isolated-canary interleaved measurement, full-response
parity, storage fingerprint, resource pressure, and exact cleanup result here
before publication.
