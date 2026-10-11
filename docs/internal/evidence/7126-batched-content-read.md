# Batched content read: proof (#7126, PR 2)

The needed-content change (PR 1) reads content only for files an extractor
consumes, but it still issues one `GetFileContent` statement per file. Each
statement on the fenced reader costs a borrow, an identity fence, a replay
fence and the business query, plus 6 spans and 5 histogram records. This change
reads the consumed files of a repository with one `GetFileContents` statement
per chunk of 10 paths. It stacks on PR 1. Ingest-time extraction (PR 3), a
parse cache and request budgets are not in this change.

## What is read now

The selection, the 50-file cap, the blank rule and the non-blank statement are
as in PR 1. Only the two content reads changed:

1. The consumed files of one repository (`configArtifactReadsContent`) are
   read in chunks of 10 paths, up to 8 chunk reads in flight.
2. The resource files the kustomizations name (phase 2) are read the same way.

Repositories are still visited one after another. A repository holds at most
five chunks per read, because the cap is 50 files. `HydrateRepositoryCandidateFiles`
(controller, runtime, workflow and compose/GitHub Actions loaders, anchor
repository only) still reads one file per statement.

## Statement

```sql
SELECT repo_id, relative_path, coalesce(commit_sha, ''),
       content, content_hash, line_count, coalesce(language, ''),
       coalesce(artifact_type, '')
FROM content_files
WHERE repo_id = $1 AND relative_path = ANY($2::text[])
```

The columns and the scan are those of `GetFileContent`. The match is equality,
not `LIKE`, so a path with `%`, `_`, a backslash, a quote or a space matches
only itself. The input is deduplicated on exact value and an empty input issues
no statement.

Why a chunk and not one statement per repository: one statement detoasts its
rows on one backend, serially. On the 7.4 MB repository of the response set a
single statement was 2.4 times slower (21.6-24.3 ms against 9.0-12.7 ms)
than eight parallel single-file reads (a private harness measurement on a read
replica, not committed). Chunks of 10 keep the work parallel.

## SQL proof (private PostgreSQL 18.6, UTF8, en_US.UTF-8)

`TestGetFileContentsMatchesSingleReadsLive` stores 19 rows of the classes the
extractors meet and compares the batched result with one `GetFileContent` per
path, field by field (`reflect.DeepEqual`), for 14 path sets.

| Class | Covered |
|---|---|
| Content | ASCII, multibyte (`ü-é 日本語 🙂`), empty, whitespace only, 2 MB out of line |
| Columns | `commit_sha`, `language`, `artifact_type` set and NULL |
| Paths | space, double quote, single quote, `%`, `_`, backslash, non-ASCII, comma and braces, leading `./`, padded with spaces, upper and lower case, the word `NULL`, `100%` |
| Input sizes | 0, 1, 10 and all 19 paths; one absent path; absent among present; duplicates |
| Isolation | a row of the same path in another repository never returns |
| Pattern characters | `%`, `_`, `%.yaml`, `pct%`, `a_c.yaml`, `a%c.yaml` return only exact matches |
| NULL element | the statement run raw with `{"plain.yaml",NULL}` returns 1 row, `{NULL}` and `{}` return 0 |

`TestGetFileContentsPlanUsesAnIndexLive` explains the exact statement the
reader sends (`getFileContentsStatement`) among 12,000 rows of other
repositories, under a custom plan and under a forced generic plan, for a
repository of 8 files, 10 paths of a 50-file repository, and 50 paths of it:

```text
repo-small, custom plan, 8 paths:
  Index Scan using content_files_repo_path_pattern_idx on content_files
    Index Cond: ((repo_id = 'repo-small'::text) AND (relative_path = ANY ('{...}'::text[])))
repo-small, generic plan, 8 paths:
  Index Scan using content_files_repo_path_pattern_idx on content_files
    Index Cond: ((repo_id = $1) AND (relative_path = ANY ($2)))
repo-fifty, generic plan, 10 and 50 paths: the same node and condition
```

No plan holds a sequential scan. The test accepts the primary key as well as
the pattern index; every plan observed used the pattern index.

## Equality proof

Package `repositoryartifacts`, through the real loader
(`loadRepositoryConfigArtifactsForSources`). The oracle is the per-file reader
of PR 1 (`perFileFetch`, the same plan with `hydrateFileIndexes`). The
comparison is the SHA-256 of the marshalled `config_paths`, the outcome class
and the set of paths fetched. The PR 1 tests still compare the production
loader with full hydration, now through the batched reader.

| Test | What it covers |
|---|---|
| `TestBatchedHydrationMatchesPerFileOnGeneratedTrees` | the 400 seeded trees of PR 1; digest, outcome, fetched path set, files hydrated and skipped equal; batched statements fewer than per-file reads in total; no `GetFileContent` statement |
| `TestBatchedHydrationChunkScenariosMatchPerFile` | 0, 1, 10, 11, 50 and 60 candidates; a chunk of all blank rows; mixed blank rows; a vanished row in the middle chunk, the first row, and every row of a chunk; an erroring chunk among good ones; two erroring chunks; a 2 MB file in a chunk; paths with space, quotes, backslash, `%`, `_`, non-ASCII, comma and braces |
| `TestBatchedHydrationDuplicateListPathsMatchPerFile` | an anchor list with three repeated rows; statements carry distinct paths |
| `TestBatchedHydrationIssuesOneStatementPerChunkOfTen` | 0 to 120 files: one statement per chunk of at most 10, at most five per repository, no `GetFileContent` |
| `TestBatchedHydrationPhaseTwoReadsAreChunked` | 23 referenced resources read in three chunks plus one for the kustomization |
| `TestNeededContentHydration*` (PR 1) | blank classes, kustomize shapes, cap, preloaded content, vanished rows, read errors, mutants; unchanged and green |

## Failure and concurrency rules

| Rule | Proof |
|---|---|
| At most 8 chunk reads of a repository in flight; the bound is reached | `TestChunkedReadKeepsAtMostEightChunksInFlight` (200 paths, 20 chunks, peak 8, a ninth never starts while eight block) |
| Chunks launch in list order. The launch decision and the recording of a failure share one mutex, and a read frees its slot in the same critical section that records its failure, so no chunk is launched once a failure is recorded or the context is done. A chunk launched earlier runs to completion, and its statement may begin after the failure was recorded. The returned error is the lowest-numbered failing chunk's, whatever the interleaving | `TestChunkedReadReturnsTheLowestFailingChunkDeterministically` (25 runs, the higher chunk fails first) |
| A recorded failure stops the launches | `TestChunkedReadStopsDispatchingAfterAFailure`: 20 chunks, one in flight, the first fails; exactly 1 statement runs |
| A failed or cancelled pass leaves the rows unchanged | the same test, and `TestChunkedReadCancelledBetweenChunksIsNotSuccess` |
| A cancelled context issues no statement and sets no batch statistic; one cancelled in flight returns promptly, launches nothing more and leaves nothing in flight | `TestChunkedReadHonorsContextCancellation` (a store that ignores the context; exactly 0 and exactly 8 statements) |
| A stop on the context with every read successful is not success | `TestChunkedReadCancelledBetweenChunksIsNotSuccess` (exactly 1 statement of 20) |
| Rows are assigned by path, not position; an unrequested row is ignored; repeated list rows each get the row | `TestChunkedReadAssignsRowsByPathNotByPosition` |
| No data race | the package tests pass under `-race` (each read writes only its own slot under the mutex; the caller writes the rows after `wg.Wait`) |

The earlier per-file reader returned whichever failure arrived first and
returned `nil` when the context ended before any read started. The chunked
reader returns the context error in that case. A cancelled request therefore
fails the load instead of answering from a partly hydrated set. This is the
one intended behavior difference besides the one PR 1 already named.

## Guards and seeded pairs

`TestChunkedReadShapeGuardSeededPair` is the seeded-violation pair for the
statement-shape guard (`readShapeViolation`: no `GetFileContent` statement,
every chunk at most 10 paths, at least one chunk). GREEN: the production
loader passes. RED: the same load through the per-file reader is reported with
`25 GetFileContent statements`. The equality harness reuses PR 1's
seeded-extractor pair (`TestNeededContentHydrationGuardCatchesUnplannedContentRead`).

## Mutation sweep

One edit at a time to a production file, the focused tests run (the package
`-count=2`), the file restored. Script `mutate3.py` in the proof scratch
directory; log `mutate6.log`; HEAD of this PR. 32 mutants: 31 killed, 1
survived.

| Mutant | Result | Killed by |
|---|---|---|
| chunk end off by one | killed | `...CountBatchedBytes`, generated trees |
| chunk step off by one | killed | `...LowestFailingChunk...`, `...CountBatchedBytes` |
| chunk size 11 | killed | `...KeepTheLargestInFlight...`, shape guard |
| last chunk dropped | killed | `...CountBatchedBytes`, generated trees |
| wrong repository passed | killed | `...PreloadedContentIsKept`, `...ReferenceOutsideCapIsNotRead` |
| chunks launched in reverse | killed | `...StopsDispatching...`, `...LowestFailingChunk...` |
| highest failing chunk wins | killed | `...LowestFailingChunk...` |
| chunk error swallowed in the read | killed | `...StopsDispatching...`, `...ReadErrorsFailTheRepository` |
| failure return swallowed | killed | `...StopsDispatching...`, `...ReadErrorsFailTheRepository` |
| unbounded fan-out | killed | `...KeepsAtMostEightChunksInFlight`, `...CancelledBetweenChunks...` |
| serialized, one in flight | killed | `...KeepsAtMostEightChunksInFlight` |
| reader gets a background context | killed | `...HonorsContextCancellation` |
| launch decision ignores a done context | killed | `...CancelledBetweenChunks...`, `...HonorsContextCancellation` |
| launch decision ignores a recorded failure | killed | `...StopsDispatchingAfterAFailure` |
| launch decision ignores both | killed | `...StopsDispatching...`, `...HonorsContextCancellation` |
| dispatcher woken before the failure is recorded | survived | see below |
| context stop reported as success | killed | `...CancelledBetweenChunks...`, `...HonorsContextCancellation` |
| row given to the first list position only | killed | `...AssignsRowsByPathNotByPosition` |
| duplicate paths sent twice | killed | `...AssignsRowsByPathNotByPosition` |
| statement count not recorded | killed | `...StopsDispatching...`, `...CountBatchedBytes` |
| in-flight maximum overwritten, not maxed | killed | `...KeepTheLargestInFlightAcrossRepositories` |
| in-flight maximum not recorded | killed | `...KeepTheLargestInFlightAcrossRepositories`, `...KeepsAtMostEight...` |
| chunk size recorded without a statement | killed | `...HonorsContextCancellation` (already cancelled) |
| batched bytes not counted | killed | `...CountBatchedBytes` |
| batched statement not counted | killed | `...CountBatchedBytes` |
| phase 1 read per file | killed | panic of the batch-only store; `...MatchesPerFile...` |
| phase 2 read per file | killed | `...PhaseTwoReadsAreChunked` |
| SQL `LIKE ANY` | killed | `...MatchesSingleReadsLive` |
| SQL ignores the repository | killed | `...MatchesSingleReadsLive` |
| stage line drops `batch_statements` | killed | `...StageReportsReadCounts` |
| stage line drops `batch_chunks_in_flight_max` | killed | `...StageReportsReadCounts` |
| stage line drops `hydrated_bytes` | killed | `...StageReportsReadCounts` (exact byte total) |

The reader mutants of the first sweep (query error swallowed, scan error
partial rows, `rows.Err` ignored, input not deduplicated, statement for an
empty input) were killed there by the reader unit tests and the code they
cover did not change; they were not rerun.

An earlier sweep of this PR reported the removal of the dispatcher's loop-head
`ctx.Err()` check as an equivalent mutant. It was not. A done context could
hand a chunk to a ready worker, and a store that ignores the context returned
success after issuing statements (review verdict F2: the earlier subtest failed
84 times in 200 runs against it). The dispatcher was rewritten so that the
launch decision is a single check under the mutex. Its mutant is killed on
every run: `TestChunkedReadHonorsContextCancellation/already_cancelled` failed
50 of 50 runs at `-count=50`.

The one survivor splits the critical section of a finishing read: it wakes the
dispatcher and releases the mutex, then takes the mutex again to record the
failure. It is killable only if the dispatcher wins the mutex in that gap.
That is rare: `TestChunkedReadStopsDispatchingAfterAFailure` did not fail in
2,000 runs against it, and the independent review saw one failure in 1,000
runs under `-race`. It is a survivor of a test that cannot control the
scheduler. The correct code records the outcome and frees the slot in one
critical section by construction, which is the property the mutant breaks; it
is not claimed as covered by a test.

## Performance, laptop bench

Real fenced reader and `ContentReader`, hydration and
`BuildRepositoryConfigArtifacts` against a private native PostgreSQL 18.6
primary (port 36713) and hot standby (port 36714), no Docker, `fsync=off`,
reader with one fleet member (the replay fence runs for real). CPU is
`getrusage` user plus system around the story, `GOMAXPROCS=2`, nine rounds per
data set with a discarded warm-up, arms reversed every other round so the first
mover alternates, one OTel SDK tracer with `AlwaysSample` and a counting
exporter, a real meter provider. The harness is PR 1's, with the shipped
loader's reader switchable between PR 1's per-file read and this change's.

Host load1 across the quiet-host runs of this section, sampled every 5 s: 4.0
to 9.9 (below the 12 limit; PR 1's runs were at 15-32). The bench anchor of PR 1
failed (shipped path scaled to the pod's statement count measured 0.54 of the
pod's CPU), so only arm-against-arm differences hold; none of these numbers is
a pod figure.

Which code each table measured. The dispatcher was rewritten after the first
bench (launch under the mutex, a goroutine per chunk). The table below is at
the fixed head. It was taken on a busy host: load1 sampled at 21.6 to 32.7,
above the 12 limit, so its CPU figures are inflated by contention and hold
arm-against-arm only. Statements, spans, bytes and digests are unaffected by
load. The tables after it were measured with the earlier dispatcher (a worker
pool fed through a channel) on a quiet host. CPU at the fixed head on a quiet
host is NOT_CHECKED.

Fixed head, busy host (load1 21.6-32.7):

| Data | Arm | Statements | CPU-s median (min-max) | Rounds N below FN |
|---|---|---|---|---|
| Original mix | A | 8,300 | 4.487 (4.273-4.835) | |
| | FN | 3,618 | 4.085 (3.635-4.590) | |
| | N | 1,140 | 3.970 (3.554-4.083) | 8 of 9 (-0.115 CPU-s, -2.8 percent) |
| Observed mix | A | 8,300 | 3.749 (3.027-3.819) | |
| | FN | 4,325 | 3.213 (2.794-3.437) | |
| | N | 1,166 | 3.014 (2.364-3.117) | 9 of 9 (-0.199 CPU-s, -6.2 percent) |

N against A at the fixed head: -0.517 CPU-s (-11.5 percent) on the original
mix and -0.735 CPU-s (-19.6 percent) on the observed mix. The CPU gain of N
over FN is smaller than with the earlier dispatcher on a quiet host. Its sign
holds in 17 of 18 rounds. The spans for N are 6,842 and 6,998.

Arms. A: the pre-change pass (every candidate read in full, one statement per
file). FN: PR 1's loader (needed content, one statement per file). N: this
change's loader (needed content, chunks of 10).

Earlier dispatcher, quiet host (load1 4.0-9.9):

| Data | Arm | Statements (list + get + batch + blank) | Bytes | CPU-s median (min-max) | Digest |
|---|---|---|---|---|---|
| Original mix, 8,000 files, 82.7 MB (pass 1) | A | 300 + 8,000 + 0 + 0 = 8,300 | 82,712,468 | 3.273 (3.154-3.478) | `c4cd9f6b` |
| | FN | 300 + 3,042 + 0 + 276 = 3,618 | 29,589,516 | 2.960 (2.851-3.119) | `c4cd9f6b` |
| | N | 300 + 0 + 564 + 276 = 1,140 | 29,589,516 | 2.753 (2.667-2.822) | `c4cd9f6b` |
| Original mix (pass 2) | A | 8,300 | 82,712,468 | 3.143 (3.094-4.083) | `c4cd9f6b` |
| | FN | 3,618 | 29,589,516 | 2.854 (2.798-3.755) | `c4cd9f6b` |
| | N | 1,140 | 29,589,516 | 2.638 (2.608-3.549) | `c4cd9f6b` |
| Observed mix, 8,000 files, 57.4 MB | A | 300 + 8,000 + 0 + 0 = 8,300 | 57,410,054 | 2.353 (2.222-3.041) | `06b952e9` |
| | FN | 300 + 3,753 + 0 + 272 = 4,325 | 15,845,536 | 1.960 (1.938-2.659) | `06b952e9` |
| | N | 300 + 0 + 594 + 272 = 1,166 | 15,845,536 | 1.749 (1.676-2.399) | `06b952e9` |
| Heaviest repository alone, 50 files, 11.3 MB | A | 1 + 50 = 51 | 11,349,042 | 0.439 (0.435-0.455) | `765e7796` |
| | FN | 1 + 28 + 0 + 1 = 30 | 5,658,197 | 0.450 (0.447-0.461) | `765e7796` |
| | N | 1 + 0 + 4 + 1 = 6 | 5,658,197 | 0.453 (0.447-0.458) | `765e7796` |

Differences on medians (earlier dispatcher, quiet host):

| Data | N against FN (PR 1) | N against A (pre-change) | Rounds N below FN |
|---|---|---|---|
| Original mix, pass 1 | -0.207 CPU-s (-7.0 percent) | -0.520 CPU-s (-15.9 percent) | 9 of 9 |
| Original mix, pass 2 | -0.216 CPU-s (-7.6 percent) | -0.505 CPU-s (-16.1 percent) | 9 of 9 |
| Observed mix | -0.211 CPU-s (-10.8 percent) | -0.604 CPU-s (-25.7 percent) | 8 of 9 (one round +0.277, the maximum 2.399 of N) |
| Heaviest repository | +0.003 CPU-s (+0.7 percent, noise) | +0.014 CPU-s (+3.2 percent, noise) | 6 of 9 |

Reader attempts equal statements (1,140 against 3,618 and 8,300), spans fall
from 49,802 (A) and 21,710 (FN) to 6,842 (N) on the original mix, and
`pg_stat_statements` on the standby shows the same counts for the batch,
identity, replay and non-blank statements. The remaining CPU is parse, which
this change does not touch. The digest is equal across all 27 runs of each data set,
and the original-mix digest equals PR 1's (`c4cd9f6b`).

The heaviest repository (13.7 percent of the original mix's bytes) shows no
CPU or wall regression on loopback: median wall 0.46 s for FN and N, 0.43 s for
A. Loopback hides network transfer and the standby's detoast of a larger
result; neither was measured.

## Evidence markers

Performance Evidence: baseline is PR 1's loader (arm FN, one `GetFileContent`
statement per consumed file) and the pre-change pass (arm A) on a private
native PostgreSQL 18.6 primary plus hot standby, the real fenced reader and
`ContentReader`, 300 related repositories and 8,000 hydrated files. After, with
the shipped loader (arm N): 1,140 statements against 3,618 (FN) and 8,300 (A)
on the original mix, 1,166 against 4,325 and 8,300 on the observed mix, spans
6,842 against 21,710 (FN), and the same `config_paths` digest in all 27 runs of
each data set. CPU at the fixed head (launch under the mutex), nine ABBA rounds
on a busy host at load1 21.6-32.7, arm-against-arm only: median 3.970 against
4.085 CPU-s (FN, -2.8 percent, N lower in 8 of 9 rounds) and 4.487 (A) on the
original mix, 3.014 against 3.213 (FN, -6.2 percent, 9 of 9) and 3.749 (A) on
the observed mix. With the earlier dispatcher (a worker pool) on a quiet host at
load1 4.0-9.9: 2.753 against 2.960 CPU-s (FN, -7.0 percent) and 3.273 (A) on the
original mix, 1.749 against 1.960 and 2.353 on the observed mix (-10.8 and -25.7
percent against A). CPU at the fixed head on a quiet host is NOT_CHECKED. The
statement is an index scan on `content_files_repo_path_pattern_idx` with no
sequential scan, for a small repository and a 50-file repository, under a custom
and a generic plan. The change is safe because the batched reader returns
exactly the rows N single reads return (live proof), assigns them by path,
fails the repository on any chunk failure with a deterministic error, keeps at
most 8 chunk reads in flight, launches no chunk once a failure is recorded or
the context is done, and stops on a cancelled context.

Observability Evidence: the `deployment_artifacts` stage line of
`repository_query.stage_completed` carries `batch_statements`,
`batch_chunk_size` and `batch_chunks_in_flight_max` next to the PR 1 fields, and
`statements` counts a batched read as one statement, so statements before and
after are comparable. The context route's `content_infrastructure_overview`
line carries the same fields, and a failed load logs them on
`repository_query.deployment_artifacts_degraded`. Each chunk is one
`postgres.query` span with `db.operation=get_file_contents` and one fenced
reader statement. No metric instrument was added: the counts are log fields, as
in PR 1. The HTTP API, OpenAPI and MCP surfaces are unchanged (no wire change).

## Not in this PR

Ingest-time extraction (PR 3), a parse cache, request budgets, a batch for the
anchor repository's controller, runtime, workflow and compose loaders, a
cross-repository batch, and any change to worker counts. Pod CPU and the
QA-environment stage seconds of the response set are NOT_CHECKED. CPU at the fixed head
on a quiet host (load1 under 9) is NOT_CHECKED: the only CPU measured at the
fixed head is the busy-host rerun. Server-side detoast
on the standby for a large chunk, API heap for a chunk of large files, and
concurrent stories against the 30-connection reader pool are NOT_CHECKED.
