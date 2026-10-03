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

## Reader-fleet follow-up, 2026-10-03

The fleet routing branch does not change the code-topic SQL. Its new contract is
that one four-connection snapshot set stays on a qualified physical standby,
and a whole read can retry after that member is lost. The measurements below
separate the legacy endpoint from the opt-in fleet path. Neither is deployed
ops-qa acceptance or a 100k-repository capacity result.

### Preserved-corpus legacy route

The private run `7033-f4857eef-20261003` built API images from base
`306eac09c893b5739533fcb054330b21eb99e956` and reviewed candidate
`f4857eefcd511efef9ffd60df5db592371399ac2` with the same pinned Go
1.26.6 Dockerfile and runtime. The images were
`sha256:54179746b3363f2a2f32066e4a07b819c717152ef66e261086b293bbc0c77287`
and
`sha256:c69d94f9b127e817c2e4806b453801300d201bc943ac0427dc6a51dbb0a7ce3c`.
Both used one preserved PostgreSQL 18 primary and Neo4j, with the same
connection configuration and the canonical 16-term request, limit 25.
The corpus had 2,808,209 content entities and 155,826 content files; counts,
row-version hashes, and content-table mutation counters matched before and
after. This configuration did **not** exercise physical reader-fleet routing.

Two prior quiet-host base control sets had medians 0.415602 and 0.410272 s;
their frozen upper bound was 0.446954 s. After balanced warmup, eight ABBA
rounds produced 16 valid requests per variant, with no control above that
bound. Full HTTP-body timings were:

| Source | Median | Nearest-rank p95 |
| --- | ---: | ---: |
| Base | 0.413778 s | 0.428213 s |
| Candidate | 0.414077 s | 0.435662 s |

The eight same-round candidate/base ratios had mean 1.001243 and sample SD
0.014082. All timed requests returned HTTP 200, 25 rows, 16 searched terms,
and the same complete canonical JSON SHA-256
`095bfc68e8251d24b6a684901263dd5d69b8c9c55b9203cccaed5b7dde7c3e88`.
Maximum sampled load1 was 2.45 on 16 logical CPUs, below the half-CPU gate.
The harness SHA-256 was
`256640c388df76f3902a71fd5e001d45025fe8f8de79f1125ae9034bac706e64`.
Both APIs served the endpoint but returned `/readyz` 503 on this preserved
stack, so this run is not a rollout-readiness proof. The temporary canaries,
restricted login, and credential file were removed; the database volumes
were stopped and preserved.

### Opt-in physical reader fleet

A separate isolated PostgreSQL 18.6 primary and two distinct streaming
standbys held a fixed small corpus of 400 entities and 200 files. Both exact
API binaries used the same Neo4j instance, topology, request, and aggregate
connection limits. The Neo4j graph was empty; this endpoint reads the
PostgreSQL content store, so this fixture is not graph-result proof. The first
candidate configuration retained only two idle
connections per reader although each snapshot set needs four. A same-binary
interleaved shim measured 0.101612 s median with two idle connections per
reader and 0.074521 s with four; the latter kept four open connections on
each member. One noisy shim attempt was discarded after a base-control
exceedance; the complete clean run had equal response digests and unchanged
content fingerprints. This established the idle-budget cause before the
configuration fix.

The final candidate source was
`3b49a53151bb5403c16ada31109f1a7908c48b03`, built from a clean detached
Git worktree with pinned Go 1.26.6. Its binary SHA-256 was
`687c8e67f3832f9906bfe6be3a969b0c9d2879d389b372cd66735549dedbd914`;
the base binary SHA-256 was
`4bdfaa4865f133ec859745aafdd8560c29eb18d6f15dbe9f5b98b6bfb4580819`.
With the same effective eight-open/eight-idle reader budget on both APIs,
the final candidate left its idle override unset to exercise the new default.
Eight interleaved ABBA rounds gave 16 timed requests per variant:

| Source | Median | Nearest-rank p95 |
| --- | ---: | ---: |
| Base | 0.076659 s | 0.082437 s |
| Final fleet candidate | 0.077670 s | 0.082110 s |

The eight same-round candidate/base ratios had mean 1.018290 and sample SD
0.040904. All 32 requests returned HTTP 200 with the same 25-row canonical
response SHA-256
`2c83c9c3e89719bb1a7d94d141c31a5643991ddac06dd4a5387c50a5f8f98133`;
the content fingerprint was unchanged. The frozen base
control bound was 0.083419 s with zero exceedances, and maximum sampled
load1 was 0.96 on 12 logical CPUs. Both qualified members served separate
requests (12 and 11 successful attempts), kept four open connections each,
and had zero in-use connections or reservations at the end. The retained
timing artifact SHA-256 was
`77c2ae708b02f3eccf15ea202da93f50eb25ba2571610788352951ec7fc5c4e0`.
Focused live tests covered same-member snapshots, contention, topology
rejection, and a refused-dial member-loss retry. A real proxy-loss endpoint
test and representative fleet-scale latency remain **NOT_CHECKED**.

### Current deployed acceptance boundary

On ops-qa's newly rolled API image `sha-ebce60b` (2026-10-03), three
read-only canonical 16-term requests returned HTTP 200 in 1.323721,
1.189914, and 1.155378 s (median 1.189914 s). Their within-run response
digest matched. Tempo traces for those requests reported
`code_topic.execution_mode=single_statement` and
`code_topic.parallel_fallback_reason=snapshot_set_unavailable`; the three
PostgreSQL query spans took 1.170486, 1.023285, and 0.989622 s.
The current one-reader Service route cannot guarantee server-local snapshot
sets as more readers are added. These live measurements show the `<1 s`
target remains unmet; they are not a controlled before/after comparison with
the separate preserved corpus or a candidate deployed fleet result. #7033
remains open.

PR boatsgroup/iac-eks-eshu#221 then pinned the ops-qa API and MCP to image
`sha-306eac0`; ArgoCD reported Synced/Healthy, and the API, MCP, writer, and
single reader were Ready. Three further read-only requests on that now-deployed
API returned HTTP 200 in 1.241450, 1.122866, and 1.127621 s (median
1.127621 s), again with one within-run response digest. The image and time
window changed, so these values are a fresh current-state check, not an
interleaved before/after speedup. Its trace routing mode was **NOT_CHECKED**;
there is still one deployed physical reader and no fleet endpoint measurement.

### Established mid-read member loss, 2026-10-03

A separate one-primary PostgreSQL 18.6 shim established the failure mode before
the follow-up edit. Terminating the backend after the first streamed row of a
read-only repeatable-read query made pgx return `*pgconn.PgError` SQLSTATE
`57P01` from `rows.Err()`; the old fleet classifier did not mark it as a lost
member. An abrupt container kill instead returned `io.ErrUnexpectedEOF`,
which the old classifier already handled. This shim did not prove peer retry
or fleet reservation cleanup.

The follow-up at source `579eda56b` added a narrow fleet-only classifier for
`57P01` and `57P02`, with negative cases for cancellation, ordinary SQL,
authentication, and capacity errors. On an owned PostgreSQL 18.6 primary and
two distinct streaming standbys, the full 16-term code-topic test terminated
one standby backend during an established snapshot. It observed the underlying
`57P01` and loss marker, exactly two whole-snapshot attempts on distinct
member addresses, and a complete retry result equal to the uninterrupted
baseline. SQL pools ended with `InUse=0`, and both member reservation gauges
were zero. The test was green for three consecutive normal runs (5.222 s,
exit 0) and one race run (3.126 s after cached build, exit 0). A response
digest was **NOT_CAPTURED**; equivalence here is the test's complete row-slice
comparison, not a cross-run hash.

The test requires explicit disposable-database and backend-termination opt-ins,
three direct DSNs, and three running Docker containers with the exact task
label. It checks DSN IPs against those containers before creating its proof
database and verifies the backend's proof-database name before termination.
A planted missing-ownership violation failed before database creation. The
owned containers, volumes, and network were removed after the run. This is a
physical failure/retry proof, not an interleaved latency comparison, a deployed
ops-qa fleet result, or evidence that #7033 meets the `<1 s` budget.
