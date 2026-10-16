# Derive repository config artifacts when content is written (#7126)

Status: Accepted with changes (arbiter ruling and re-rulings 1, 2 and 3,
2026-10-10, and the S3 and cap rulings, 2026-10-11).
Shims S0 and S2 pass. S3 arms 1, 2, 4 and 6 pass. Arms 1, 2 and 6 ran in a
run whose in-run load1 maximum was 9.88 against the 9.0 bound, for 25 s, from
foreign processes and the shim's own seeding. The arbiter accepted them with
that deviation noted (evidence appendix, section 6). The arm 3 and arm 5
reruns are pending. The arm 1 confirmation (5 rounds, default sizes) rides
with the arm 5 rerun, and a reversal there reopens arm 1. S1 and S4 have not
run. S6 ran and raised E to 12,000 (section 3.3). The cap ruling supersedes
F3: the 50-candidate cap is lifted, not deferred (section 3.3, #7951). This
record does not approve the schema, the migration number or any rollout. Each
of those waits for the shims in section 8. Refs #7126.

**Decision: compute the `config_paths` rows of each repository once, after the
content writer commits that repository's content, and store them in Postgres
without the repository name. The story and context routes read the rows of
all related repositories with one indexed statement, apply the graph name at
read time, and run the query-time extractor only for a repository whose
stored rows are missing or stale.**

The derive is best-effort. A derive failure never fails the content Write. A
stale or missing row costs latency, never accuracy, because the live extractor
answers for that repository. The derive protocol, the fence and the
interleavings are in the
[protocol companion](7126-ingest-time-config-artifacts-protocol.md). The
measurements are in the
[evidence appendix](7126-ingest-time-config-artifacts-evidence.md).

## 1. Context

In the QA environment, one repository story (R1) spends 6.63-6.95 s in the
`deployment_artifacts` stage on four calls. The route takes 6.92-7.97 s. The
stage recomputes, on every request, the config artifacts of every related
repository from file content in `content_files`.

| Fact | Value | Source |
|---|---|---|
| Postgres reader statements per R1 story | 8,255-8,258 (R2: 34) | issue comment 6097810983 |
| API CPU per R1 story | 6.70-6.88 CPU-s at a 1 CPU limit | issue comment 6098498452 |
| Neo4j related-repository UNION and other graph reads | 0.010 s warm, 0.143 s first call, at most 2% of the stage | issue comment 6097810983 |
| `config_paths` rows in the R1 body | 2,986 rows from 141 source repositories | issue comment 6097810983 |
| Hydrated files of the 141 repositories | 4,632 files, 52.2 MB. 45 repositories at the 50-file cap | issue comment 6098312440 |
| Parse share of CPU on the local bench | 76-79%, mostly HCL regexes | issue comment 6098969523 |

The stage is CPU-bound in the API process. The cost is per related repository:
one file list, up to 50 content reads, and one parse. N is at least 141. The
trace prefix suggests about 300, but that figure is an extrapolation.

The output is a pure function of one repository's stored files and its name.
The name enters only through two read-side steps (section 3.3). So the work
can move to the moment the files change, and the read can become one
statement for the whole related set.

### What PR1, PR1b and PR2 already change

PR3 keeps these changes and does not redo them. On 2026-10-10 all three are
local branches with clean or pending reviews. None is merged, and none is
published as a PR.

| PR | Branch | Change on the read path | Measured effect |
|---|---|---|---|
| PR1 | `fix/7126-needed-content-hydration` | Reads content only for files an extractor reads, asks `NonBlankRepoFilePaths` for the rest, adds read stats to the stage log, adds the `deployment_artifacts_read_degraded` reason | laptop bench CPU -6.8% / -9.1% (overlapping ranges) |
| PR1b | `perf/7126-hcl-required-literal-gate` (stacked on PR1) | Skips an HCL regex scan when a file lacks the scan's required literal | 25x-34x on plain files. The whole-set saving is a projection (5.1x-5.4x less regex work) |
| PR2 | `perf/7126-batched-content-read` (sibling of PR1b on PR1, in progress) | A batched, chunked content read per repository | laptop bench -20% CPU (arm D) |

The measured effects come from the PR1 and PR1b review verdicts and the CPU
bench. Those are local kit notes, not in the repository.

An earlier estimate put PR1 plus PR2 at 2.5-4.5 s for the stage in the QA environment.
That is a theory until it is measured after the roll.

## 2. Options

```mermaid
flowchart LR
  ingester["ingester: sync, discover, parse, emit facts"] --> projector["source-local projector: ContentWriter.Write"]
  projector --> content[("content_files")]
  projector -->|"B: derive after commit"| rows[("config_artifact_paths")]
  projector -->|"reducer intents"| reducer["reducer"]
  reducer -->|"C: derive per intent"| rows
  reducer -->|"walker: repair, backfill"| rows
  rows --> api["API story and context routes"]
  content -->|"live fallback"| api
```

| Option | Cost | Verdict |
|---|---|---|
| A. Ingester parse stage emits the rows as facts | A new fact kind and contract. A delta generation sees only changed files, but the candidate selection and the kustomize walk need the whole stored file set. An extractor change needs a re-parse of every repository. | Rejected. The input differs from what the query reads. |
| B. Content-writer derive in the source-local projector | One bounded list, read and parse per repository content Write. Needs a content-version fence, because retention and manual SQL change `content_files` without a derive. | Accepted with changes. Same input as the query path, no added lag. |
| C. New reducer domain per repository generation | A new intent, queue domain and lease contract. Freshness lags the reducer backlog. | Not chosen for the write path. The reducer walker repairs and backfills. |
| A separate worker after projection | A new runtime stage, the same lag as C, no owner today. | Rejected. |

Option B follows `infra_resource_entities` (#6793) for the place, the
backfill marker and the reducer reconcile. It departs from #6793 in three
places, for reasons given in section 3.1 and in the protocol companion: the
derive cannot fail the Write, the parse runs outside the lock, and the fence
is a content version that every session bumps, not a dirty mark set only by
sessions that do not derive.

## 3. Decision

### 3.1 Where extraction runs, and failure semantics

1. `ContentWriter.Write` derives the written repository as its last step,
   after every content statement commits. `ingester`, `projector` and
   `bootstrap-index` all write content through this method, so all three
   derive. The derive reads the primary, so it has no replay fence.
2. The derive is a whole-repository derive. It lists, selects every
   candidate of the list window (allowance -1, section 3.3) and reads content
   as the live path does, with the same SQL (section 3.3, "Same SQL").
3. A trigger on `content_files` bumps a per-repository content version
   once per transaction, for every session. The bump commits with the
   content. The derive reads the version `v0` before it lists the files.
4. List, read and parse run with no lock and no open transaction. A short
   write transaction then takes the repository advisory lock and writes only
   if the version or the extractor version advances. Otherwise the derive is
   superseded. Readers trust the rows only when the stored version equals the
   current version.
5. A derive error never fails the Write. The derive recovers a panic and runs
   under a deadline (default 10 s, **Proposed** name
   `ESHU_CONFIG_ARTIFACT_DERIVE_TIMEOUT`). On any failure it counts the
   outcome, records `failure_count` and `last_error_class` on the state row,
   logs at ERROR with the repository id, and returns nil. If the content
   moved, the stored version stays behind, so readers fall back.
6. The reducer owns repair. Its walker runs the same derive protocol for
   reconcile, backfill and extractor version changes.
7. A startup check guards two invariants. Each deriving binary and the API
   run `SHOW server_encoding` once (re-ruling 3). On a value other than
   `UTF8` they skip every derive with outcome `encoding_unsupported` and
   report the read model as `not_installed`. The S3 ruling adds a second
   check: five fence triggers (protocol companion, section 2; arb-8155-f1)
   must exist in `pg_trigger`, the four `content_files_version_fence_*` on
   `content_files` and `config_artifact_fence_pending_commit`. If one is
   missing, they skip every derive with outcome `fence_missing` and
   report `not_installed` with reason `version_fence_missing`. A dropped or
   disabled trigger then fails loud and does not serve stale rows as
   trusted. The detection is ruled. The mechanism is **Proposed**: the check
   requires `tgenabled IN ('O','A')`, so a disabled (`D`) or replica-only
   (`R`) trigger counts as missing. The Write proceeds, and reads stay live.

This departs from #6793. `deriveInfraInventory` fails the Write, which is
right for a SQL-only copy. The config derive runs Go regexes and `yaml.v3`
over arbitrary content. A deterministic parse bug or a pathological file would
make the projector retry the generation until it dead-letters. That would lose
graph and content truth to protect a read model. The content rows are already
committed when the derive starts, so a failed derive cannot roll them back.

### 3.2 What is stored

Three tables hold the read model: a content version per repository, a state
row per repository, and the name-free rows. A backfill marker and a walker
cursor follow migration 109. The protocol companion has the columns. In
summary:

- A state row with a current `extractor_version` and `row_count = 0` means
  "derived, no artifacts". A missing state row, or one with no successful
  derive, means "not derived". The reader must not confuse the two.
- Rows store the path of the build with no name (`raw_path`) verbatim, and
  a `name_scoped` flag set at the construction site. The flag is a function
  of `evidence_kind`: S0 found 16 kinds, 10 scoped and 6 unscoped, none in
  both. A guard test reads both call-site families from source and pins one
  value per kind.
- Row key (G8). S2 showed that a key of four TEXT columns fails with SQLSTATE
  54000 when a path reaches 2,610 incompressible bytes. A compressible path of
  100,000 bytes still passes. Re-ruling 2: key on `(repo_id, row_digest)`,
  where `row_digest` is a stored generated SHA-256 column over the
  length-prefixed path, relative path and evidence kind, defined once in the
  schema. The four columns stay as data. A `row_too_wide` fallback and a
  length precheck are rejected. `convert_to` is STABLE, so PostgreSQL 18.6
  rejects it in a generated column. Re-ruling 3 adopted an IMMUTABLE wrapper,
  `config_artifact_utf8`, which is true per database because
  `server_encoding` cannot change. The protocol companion (section 1) has the
  definition and the rejected alternatives.
- The migration sets `autovacuum_vacuum_scale_factor` at most 0.02 on the
  rows table (re-ruling 2), because S2 measured +40% on the read after a
  rewrite with no vacuum (2.47 ms against 1.76 ms after a vacuum).
- The state row records the `content_version` the derive read, the
  `extractor_version`, and `candidate_count`, `candidates_read`,
  `files_listed` and `list_truncated` for every derive.
- The dirty set is a join of state and versions, not a table.
- Size: R1 shows about 21 rows per repository. For the 815 repositories
  the QA environment listed on 2026-10-10, that gives roughly 17,000 rows. This is an
  extrapolation. The count 815 comes from the local kit note
  `7126-story-span-probe-20261010.md`, which is not in the repository.

Retention: the rows mirror latest-wins `content_files`, not a generation.
`Write` tombstones and the full-snapshot reap run before the derive, so the
derive covers them. Generation retention deletes `content_files` rows, which
fires the trigger, so retention needs no new code path, and the bump cannot
be skipped (the live list has no generation filter; arb-8155-c1). S3 chose
the `OLD TABLE` DELETE shape, measured on the real `prune_content_files` phase
(section 8). A fallback design exists (protocol companion, section 2). It is
**Proposed**, applies only if the arm 3 rerun fails both shapes, and requires
an F2 amendment by ruling.

### 3.3 Accuracy

The stored answer for a repository must equal the live answer for that
repository from the same `content_files` state. PR3 is parity-first: the live
path at PR-cap-1's head is the oracle, so PR3 changes no extraction semantics.

- **Same function.** The live path and the derive call one per-repository
  function in a neutral package.
- **Same SQL.** The list, single read, non-blank check and batched read of
  `content_files` each have one SQL constant and one row mapper. The live
  reader and the derive both import them. A copy would drift in `ORDER BY`,
  the `artifact_type` mapping or the blank predicate, and break S4 silently.
- **Pure input.** The extractors read no clock, environment or network. They
  drop remote references without reading them. Kustomize resolves references
  only inside that repository's file index. No cross-repository input exists.
- **Name at read time.** The derive stores rows without the name. At read
  time the API sets `source_repo` to the graph `related.name` and strips the
  name prefix from the `name_scoped` rows only. Those are the HCL and compose
  rows built through `appendConfigArtifactRow`. Kustomize and Ansible rows
  never pass through it. S0 passed on 684,948 row observations with 0
  differing rows. A renamed repository therefore needs no re-derive.
- **Strip only.** `normalizeRepoScopedPath` cleans the path and then strips
  the prefix. The clean (`CleanRepositoryRelativePath`) is not idempotent
  when the cleaned result has leading or trailing whitespace: a second clean
  changed 8,943 of 200,000 random strings (0 over the S0 corpus). PR3a splits
  the function into a clean step and a strip step. The derive stores the
  cleaned path. The read step only strips and must not clean again.
- **One merge, one dedup.** The live loader keeps one dedup map across the
  anchor and all related sources. The stored path does the same: the rows of
  every repository (anchor live, stored, live fallback) merge in source
  order, then one dedup on `(path, source_repo, relative_path,
  evidence_kind)` and one sort. Two related repositories that share a graph
  name, or an anchor that also appears in the related list, then collapse
  exactly as they do live. The two loader-level merge cases are outside
  S0's scope. S4 and the PR3c tests cover both.
- **Caps (F3, amended by the cap ruling).** The silent 50-candidate cap is a
  defect of the live path, tracked by #7951. PR-cap-1 lifts it before PR3b-2.
  Each related repository keeps a floor F = 50 hydrated candidates. A
  per-request extra budget E = 12,000 candidates is spent in `RepoID` order,
  after the access-filtered sources are sorted by `RepoID`. E is provisional
  until the deployed read. S6 set it: 1.5 x R1's measured extra of 7,774
  candidates is 11,661, above the proposed 10,000 (appendix section 10). Every
  related-repository file list, and the service route's own anchor list, reads
  limit+1 and clips at 5,000. A bound that bites adds a reason to the response
  and the stage line, with counts exact within the list window:
  `deployment_artifacts_candidates_budget_reached` (the budget reason),
  `deployment_artifacts_related_file_list_truncated_at_5000` (the window
  reason) or `deployment_artifacts_anchor_file_list_truncated_at_5000` (the
  anchor reason, service route only). The live path at PR-cap-1's head is the
  S4 oracle, with allowance -1. The derive hydrates the whole list window
  (allowance -1) and stores `candidate_count`, `candidates_read` and
  `list_truncated`. The stored read (PR3c) derives the window reason from
  `list_truncated`, and never emits the budget reason for a stored repository.
  Evidence appendix, sections 9 and 10.
- **Anchor.** The anchor repository stays live. Its list is read with
  limit+1 and a truncated flag, and its pre-read compose and GitHub Actions
  files do not count toward the 50-candidate cap. Its window therefore
  differs from a related-repository window.
- **Order.** PR3a adds `evidence_kind` as the fourth sort key in both paths
  and counts the ties it reorders. `sort.Slice` is not stable, so today's tie
  order depends on input order.
- **Extractor version.** A constant `extractor_version` sits next to the
  extractors. A guard test pins a digest of extractor output over the fixture
  corpus and fails on a changed digest without a bump. The guard needs a
  seeded RED/GREEN pair.
- **Access.** The stored read takes the access-filtered set of related
  repository ids as its parameter. It never reads all related rows before the
  grant filter (#5167 W3 P0). PR3c plants an out-of-grant related repository
  with stored rows and asserts that it is absent from the body.
- **Fallback.** A repository with no state, no version row, an old content
  version, an old extractor version, or no successful derive is served live
  for that repository only, and counted. The reader uses an inner join (the
  join type is **Proposed**), so a missing version row means a live fallback
  with reason `missing`.
  Nothing is dropped, so a fallback cannot hide a wrong result.

### 3.4 Rollout

1. PR3a merges: no behavior change except the tie order.
2. PR3b deploys (1a, then 1b, then 2): the migrations create the tables, the
   triggers and one seeded version row per repository, writers derive, and
   the walker backfills. The read mode
   stays `live`. **Proposed** variable name: `ESHU_CONFIG_ARTIFACT_READ_MODE`,
   with the values `live`, `shadow` and `stored`.
3. The QA environment runs `shadow`: each request computes both answers, compares them per
   repository, logs a mismatch, and serves the live answer.
4. Cut over to `stored` when all of these hold (**Proposed** gates):
   - The backfill marker exists.
   - The dirty join is empty outside active Writes.
   - Shadow mode shows zero mismatches over the #7098 read sweep, R1 and R2.
   - An offline differential of every repository is `0/0`.
   - S1's projector bar holds.
5. Rollback: set the mode to `live` and roll the API. No data change needed.

An `extractor_version` bump makes every stored row stale at once. Every
related repository then falls back live until the walker re-derives it. The
window is about (S1 per-repository derive time) x 815 / K. Each version bump
states this number in its PR. A bump is a performance event, not a silent one.

### 3.5 Telemetry

All names in this section are **Proposed**, except what a ruling named: the
G9 stage log fields, from re-ruling 3 the log `postgres.server_encoding`, the
derive outcome `encoding_unsupported` and the `not_installed` status with a
reason, from the S3 ruling the derive outcome `fence_missing` and the
reason `version_fence_missing`, and from the cap ruling the three reasons of
section 3.3, the counts and the counter below.

| Signal | Name | Answers |
|---|---|---|
| Counter | `eshu_dp_config_artifact_reads_total{source,reason}` | related repositories served stored or live, and why (`missing`, `stale_version`, `stale_extractor`, `failed`, `mode_live`) |
| Counter | `eshu_dp_config_artifact_candidates_total{outcome}` | `hydrated`, `unhydrated`, `list_truncated`: when a bound bites. No repository label |
| Counter | `eshu_dp_config_artifact_shadow_compare_total{outcome}` | `match`, `mismatch`, `stored_missing` |
| Counter, histogram | `eshu_dp_config_artifact_derives_total{trigger,outcome}`, `eshu_dp_config_artifact_derive_duration_seconds{trigger}` | write, backfill and repair derives. `ok`, `superseded`, `error`, `panic`, `timeout`, `encoding_unsupported`, `fence_missing` |
| Counter | `eshu_dp_config_artifact_reconcile_total{outcome}` | `match`, `repaired`, `superseded`, `error`, `skipped` |
| Gauge | `eshu_dp_config_artifact_stale_repos{reason}` | how far the read model is behind, from the dirty join |
| Span | one `config_artifacts.load` span per request. One derive span per Write | replaces up to about 49,600 per-statement spans of an R1 story |
| Log | `postgres.server_encoding` at INFO (ERROR when not UTF8, re-ruling 3), `config_artifacts.derive.failed` at ERROR, `config_artifacts.shadow.mismatch`, `config_artifacts.reconcile.drift`, content-writer stage `derive_config_artifacts` | repository id, outcome, row and candidate counts |
| Status | `/admin/status` field `config_artifacts`: `not_installed` (with a reason, such as a non-UTF8 database or `version_fence_missing`), `backfilling`, `ready`, plus stale and skipped repository counts | whether stored reads are trusted |

The stage log line `deployment_artifacts` lacks N and statement counts today.
PR3c adds `read_mode`, `related_repository_count`, `stored_repository_count`,
`live_repository_count`, `live_reasons` and `stored_row_count`. No metric
carries a repository id label. At 3 AM a slow story shows as a high
`source="live"` share, and its `reason` label names the cause.

## 4. Non-goals

- No change to the 5,000-file list window. PR-cap-1 lifts the 50-candidate
  cap (section 3.3). The PR3 PRs change no bound.
- No change to the controller, runtime, workflow or CloudFormation loaders
  in PR3. They read only the anchor repository. PR-cap-2 (independent) lifts
  their silent 50 cap.
- No API response shape change, except the tie order in section 3.3.
- No cache in the API process, and no `content_hash` parse cache.
- No change to the Neo4j UNION, to other stages, or to NornicDB.
- No fact kind, collector or parser change.
- The live extractor stays. It serves the anchor and every fallback.

## 5. Consequences

Good:

1. The related-repository part of the stage costs one statement for the whole
   set, so it no longer grows with N.
2. The live path stays the reference, so a stale or missing row costs latency,
   not accuracy.
3. A rename needs no re-derive, because rows are name-free.
4. The state rows record the candidate counts, so stored mode discloses the
   5,000-window reason.

Bad:

1. Every repository content Write does extra work. S1 sets the stop bar.
2. A second copy of derived truth exists, with a fence, a walker and a
   backfill to operate.
3. An extractor version bump sends every related repository to the live path
   until the walker catches up.
4. A repository whose derive always fails or times out adds up to 10 s to each
   of its content Writes, because every Write derives. The walker skips it
   after 5 failures until its next Write or an extractor change (protocol
   companion, section 4). Its reads stay live while its rows are stale.
5. PR-cap-1 raises the live cost: up to 50 x N + E candidates per request
   (the floors plus the budget). The estimate for R1 is about +0.4 CPU-s
   (theory); the scratch shim measured +0.487 CPU-s (appendix section 10).

Follow-ups, to be filed as separate issues by the coordinator:

1. PR-cap-2 (the anchor-only loaders L7-L10). Tracking issue: #7951. It
   stays open until PR-cap-2 merges. The S6 census has run (section 3.3).
2. The cold-route stages outside PR3 (section 6).

## 6. Acceptance in the QA environment after the roll

Measure the same R1 and R2 with the same method as the 2026-10-10 probes:
serial story requests, `/metrics` deltas, stage log lines, cAdvisor CPU deltas.

The QA environment was reported to run `1ac63e871a`. The cap ruling named
`794b56a3fb`, the roll that was pending then. Both hold PR1 only
(`1be621eb11`). Neither holds PR1b, the ADR or PR2 (an ancestry check,
appendix section 10). So the plan is two reads. Read 1 on the
environment's image is the PR1 figure. Read 2 is the PR1+PR1b+PR2 figure, with
body equality, on one pinned SHA in `[6b761d2145, 64f6600d98]`: main
`6b761d2145` (the #7962 merge). That SHA excludes PR3a (it changes tie order),
PR-cap-1 and PR3b-1a. Pin it. Do not roll the latest main.

| Measure | Before | Target in `stored` mode (theory until measured on the roll) |
|---|---|---|
| `deployment_artifacts` stage, warm calls | 6.63-6.75 s | at most 0.25 s |
| Route time, warm calls | 6.92-7.04 s | at most 1.0 s |
| API CPU per R1 story | 6.70-6.88 CPU-s | at most 0.7 CPU-s |
| Postgres reader statements per R1 story | 8,255-8,258 | at most 60, and the stage count does not grow with N |
| `config_paths` | 2,986 rows, 141 source repositories (read 2 body) | the same multiset in `live` and `stored` on the same storage state, at PR-cap-1's head, where the live body is a superset of read 2's. Other body fields byte-equal |

The statement target comes from R2: its whole story used 34 reader
statements. R1 adds the stored read and the anchor's bounded reads to that
shape. This is an estimate, not a measurement.

Remaining contributors in the stage, as a budget (theory until measured):

| Contributor | Expected | Basis |
|---|---|---|
| Neo4j UNION and the other graph reads | 0.010 s warm, 0.143 s first call | issue comment 6097810983 |
| Stored-row read, 3 round trips | at most 0.02 s | 0.82 ms per statement observed. S2 shim |
| `ListRepoFiles` of related repositories | 0 (gone) | 128 lists, 0.242 s in sum in the 2026-10-10 trace prefix (local kit note `7126-story-span-probe-20261010.md`, not in the repository) |
| Anchor live work | at most 0.05 s | R2 whole stage 0.004 s. R1 anchor has 1 file |
| Name step, merge, dedup, sort of about 3,000 rows | at most 0.02 s | not measured |
| Live fallback | 0 at steady state | each fallback repository costs its old share |

If the corpus changed between the baseline and the roll, re-baseline the body
in `live` mode first. The first call of R1 had 1.016 s outside this stage
(`target_documentation` 0.498 s cold, from the same local kit note). The cold
route stays above 1 s unless those stages change, which is outside PR3.

## 7. PR breakdown and sequencing

| PR | Content | Starts | Proof |
|---|---|---|---|
| S0 test, version guard | The S0 name-free equality test (exists on the local branch `proof/7126-pr3-shims`, `953f68712e`) and the `extractor_version` digest guard, as new files | now | S0 passes (done locally). Seeded RED/GREEN for the guard |
| PR3b-1a | Migration N: version, state and rows tables with `config_artifact_utf8` and the generated `row_digest`, the autovacuum setting, the backfill marker and reconcile cursor tables. No trigger, no seed. The UTF8 startup check, the walker skeleton (no derive), the read-model port, docs | merges after PR-cap-1. May start after PR3a (cap ruling, section 4). It changes no `content_files` behavior, so no S3 arm gates it | Ordinary code-change proof. Live Postgres tests. A schema test that recomputes every digest in SQL. **Proposed**: a test that no Go statement deletes version rows outside a documented retirement path |
| PR3b-1b | Migration 184: the commit-time fence (arb-8155-f1): four `content_files_version_fence_*` statement triggers (`OLD TABLE` DELETE shape from S3) stage repositories into the UNLOGGED `config_artifact_fence_pending`, and one deferred constraint trigger bumps them at COMMIT in `repo_id` order with the `(bumped_xid, bumped_at)` guard. Migration 185: one version-0 row per repository in `repo_id` order, `ON CONFLICT DO NOTHING`. Two files, not one transaction, because the #7961 lock-shape gate refuses `CREATE TRIGGER` and bulk DML in one file (coordinator ruling 2026-10-16; evidence note `8155-content-version-fence.md`). The trigger-order schema test | after the arm 3 rerun on the real `prune_content_files` phase and the attributed arm 5 rerun pass | S3. Arm 7 cases 1a, 1b, 2 and 3 under the chosen shape (1b and 2 under both). R-1, two-scope and prune interleaving tests. Time the seed scan: `CREATE TRIGGER` stalls content writes until commit |
| PR3a | Move the per-repository function and extractors to the neutral package. Shared `content_files` SQL. Total sort. Split `normalizeRepoScopedPath` into clean and strip | after PR1, PR1b and PR2 merge | Existing tests and the PR1 differential pass unchanged. Tie count. The S0 tests run against the strip step |
| PR3b-2 | Derive call in `ContentWriter.Write`, projector wiring, walker derive (allowance -1, stores `candidate_count`, `candidates_read` and `list_truncated`), the startup fence-trigger check (section 3.1), telemetry. Register the timeout and walker variables in `go/internal/envregistry/entries.go`, regenerate the env registry page, and update the telemetry reference and `telemetry-coverage.md` | after PR3a, PR-cap-1 and PR3b-1b. It must not deploy before 1b | S1, then S4. `verify-env-registry-doc.sh` and `verify-docs-cli-env-refs.sh` |
| PR-cap-1 | L1, L2, L3 and L6 of appendix section 9: floor F and budget E, the `RepoID` source sort, the allowance parameter, the limit+1 sentinel (related lists and the service route's own anchor list), the three reasons, counts, counter, OpenAPI fragment, docs, and a blank check chunked at 500 paths. E changed from 10,000 to 12,000 after the S6 census, and this record carries that ADR edit. Stacked on PR3a | after PR3a, before PR3b-2 and PR3b-1a. It must not merge before the deployed read confirms E = 12,000 (S6 landed: 12,000 is 1.54 x R1's extra of 7,774) | Appendix section 10: cap shims 1-3 pass locally before an executor starts. The accuracy checks (51, 500 and 5,001 candidates, budget bite, ordering stability, monotonic differential, kustomize inversion, seeded RED/GREEN, access) |
| PR-cap-2 | L7-L10, the anchor-only loaders: chunked reader, the same allowance and disclosure, a sentinel on the three lists read when `files` is nil | independent of PR3. May run beside PR3b-1a | Tests that pin the cap change with intent. #7951 stays open until it merges |
| PR3c | Read mode flag and its env registry entry, name step, one merge and dedup, per-repository fallback, the 5,000-window reason from `list_truncated` (never the budget reason for a stored repository), shadow compare, stage log fields, access test, docs | after PR3b-2 | Handler tests per fallback reason. Out-of-grant test. Two related repositories with one graph name. Anchor repeated in the related list. QA-environment shadow then acceptance |

Merge order (cap ruling): the acceptance reads, then PR3a, PR-cap-1 and
PR3b-1a. PR-cap-2 is independent. PR3a is implemented in the package
`go/internal/content/artifacts`; the evidence note
`docs/internal/evidence/7126-neutral-content-package.md` records its row order
and tie count. PR3a waited because PR1 edits the loader, hydration, kustomize, HCL and
Ansible files, PR1b edits the HCL files and adds `prefilter/`, and PR2 edits
the hydration and stats files and adds a batched read. PR1b and PR2 are
siblings on PR1, so a move before they merge would rebase across renames of
files all three edit. Re-check open PRs for migration numbers at build time.
169 was the newest on `origin/main` on 2026-10-10.

## 8. Proof before build

Run the shims in this order. Record commands and numbers in the PR evidence
note. Details are in section 6 of the protocol companion.

| Shim | Where | Pass | On fail |
|---|---|---|---|
| S0 name-free rows | local Go test | 0 differing rows between the named build and the name-free build plus the name step, for 3 or more names | store the name in the state row and run S5. **Ran: PASS**. The two loader-level merge cases are not part of S0. They are in S4 and PR3c |
| S2 read plan | local PostgreSQL 18 | The reader's three-table join for 300 ids and the dirty join over 815 repositories. Each runs in at most 5 ms with a generic plan. No scan of a table that grows with `content_files` | change the key or the statement. **Ran: PASS** with the four-column key: reader join 2.44 ms, dirty join 0.18 ms. With the digest key only the paths read was measured (2.14 ms). The digest-key reader join is not measured. See the evidence appendix, section 4 |
| S3 fence cost | local PostgreSQL 18 | Six arms (companion 6). Full Write, delta Write and a 50,000-row insert at most +5%. Prune at most +5% of `prune_content_files` of the real retention store, at 1 KiB inline and 8 KiB TOASTed bodies. Two concurrent Writes at most +10% per Write, lock waits attributed by class, 0 deadlocks in 20 rounds. One version-row rewrite per repository per content transaction, never per Write (a delta Write with few tombstones has two). A delta Write expects ceil((tombstones + reaped paths) / 500) delete transactions plus its upsert batches. Arm 1 includes the generated `row_digest` insert. Arm 7 is a separate correctness test in PR3b-1b | other DELETE shape, then the **Proposed** fallback (companion 2, needs an F2 amendment). Do not build the trigger. **Ran 2026-10-11**: arms 1, 2, 4 and 6 PASS. Arm 1 confirmation (5 rounds, default sizes) rides with the arm 5 rerun; a reversal reopens it (appendix, section 6). Arm 3 failed on a proxy denominator and arm 5 failed a mis-specified bar, so both rerun (appendix section 6). **Arm 3 bar** (ruling arb-8155-c1, 2026-10-16): added `ScopeLockHold` at most +0.25 s per 100,000 doomed rows, mean of at least 5 interleaved rounds, both classes. By session means the +10%/+5% bar of arb-8155-s3 was missed in one of three sessions (S2); by session medians it was missed in all three on `prune_content_files` and in two on `ScopeLockHold`; replaced by arb-8155-c1. 1 KiB, three sessions, commit-time fence: +0.11 to +0.17 s `prune_content_files` (+9 to +14%), +0.11 to +0.17 s `ScopeLockHold` (+4 to +6%), 181.1 MB temp per prune at the retention `work_mem` of 64MB (evidence note `8155-content-version-fence.md`). The 8 KiB class and the attributed arm 5 stay pending for the merge |
| S6 cap census | QA environment, read-only | counts, hidden bytes by class and hidden rows, recorded on #7951. E >= 1.5 x R1's measured extra | raise E and redo the cost table in PR-cap-1. **Ran 2026-10-11**: FAIL at E = 10,000 (1.5 x 7,774 = 11,661), so E is 12,000 (1.54 x) and the cost table is redone in the evidence appendix, section 10. PR-cap-1 must not merge before the deployed read confirms E |
| S1 derive cost | local, PR1, PR1b and PR2 stacked | at most 10% of content-stage p95. At most 2 s for the worst repository | reopen F1 |
| S4 equality | PR3b gate | Symmetric difference `0/0` for every golden-corpus repository, against the live path at PR-cap-1's head with allowance -1. Two loader-level merge cases too: two related repositories with one graph name, and the anchor repeated in the related list | do not merge PR3b-2 |

S5 (name binding, read-only) stays optional, because S0 passed.

The S2 bar first read "primary-key index scans only, no hash". The planner
does not meet that wording: it seq-scans the 9-page versions table into a
hash join. The plan that forces every primary key is slower (2.9-3.2 ms
against 2.44 ms). The versions table grows with repositories, not with files.
Re-ruling 2 set the bar to "execution at most 5 ms at 300 ids and no scan of
a table that grows with file count", and accepted the hash join.

## 9. Arbiter rulings of 2026-10-10 and 2026-10-11

The ruling and re-rulings 1, 2 and 3 bind. The re-ruling replaced the first
fence. Re-ruling 2 decided the trigger lock order, the arm 6 count, the row
key and the S2 bar. Re-ruling 3 adopted the IMMUTABLE wrapper and the UTF8
startup check, stated the trigger timing again, and reduced arm 7 case 3 to a
convergence check. The S3 ruling of 2026-10-11 judged the first full-size S3
run. The cap ruling of 2026-10-11 superseded F3. This table maps each item
to its section here.

| Fork | Ruling | Where |
|---|---|---|
| F1 place of the derive | B, with the derive never failing the Write, a deadline, panic recovery, and parse outside the lock. The re-ruling replaced the conditional mark delete with an advance-only write | 3.1, companion 3 |
| F2 fence (re-ruled) | a content version bumped by a `content_files` trigger for every session, once per transaction per repository. No mark table, no pre-write mark, no session setting. A fallback gate that sets a transaction-local setting needs an F2 amendment by ruling (companion 2) | 3.1, 3.2, companion 2 |
| F3 caps (superseded by the cap ruling, 2026-10-11) | L2 lifted by PR-cap-1 before PR3b-2. Floor F = 50 per related repository, extra budget E = 12,000 per request (S6 landed; provisional until the deployed read), spent in `RepoID` order. limit+1 sentinel on every related list and on the service route's own anchor list. Three reasons with counts exact within the list window. The live path at PR-cap-1's head is the S4 oracle, allowance -1. The derive hydrates the whole window and stores the counts. The stored read derives the 5,000-window reason and never the budget reason. Tracked by #7951, open until PR-cap-2 | 3.3, 5, 7, appendix 9 and 10 |
| F4 anchor | stays live | 3.3 |
| F5 repair vehicle | reducer walker, same protocol, K workers by `repo_id`, skip after 5 failures | 3.1, companion 4 |
| F6 package home | neutral package, plus one shared SQL constant and row mapper per `content_files` statement | 3.3, companion 5 |
| F7 order | `evidence_kind` as the fourth key in both paths in PR3a | 3.3 |
| F8 fallback | per repository and counted. The stored read takes the access-filtered set | 3.3 |
| G1 name binding (re-ruled, closed by S0 in re-ruling 2) | Rows stored name-free with the cleaned path. A strip-only read step on `name_scoped` rows. One merge across all sources, then dedup on the four-tuple and the four-key sort | 3.3, companion 4 |
| G2, G6 | Write failure semantics, lock during parse | companion 3 |
| G7 version bump | a sized fallback window | 3.4 |
| G8 key width (re-rulings 2 and 3) | Stored generated digest key through the IMMUTABLE wrapper `config_artifact_utf8`. `row_too_wide` and a length precheck rejected. A UTF8 startup check | 3.1, 3.2, companion 1 |
| Trigger lock order and arm 7 (re-rulings 2 and 3) | `ORDER BY r.repo_id` accepted. No cross-table cycle through a version row for a prune against a Write, under either DELETE shape. S3 cost alone picks the shape, with `OLD TABLE` as the tie-breaker. Arm 7 has three cases, case 3 a convergence check. Case 1 split into 1a and 1b (**Proposed**) | companion 2 and 6 |
| Arm 6 count and S2 bar (re-ruling 2) | one rewrite per content transaction. At most 5 ms and no scan that grows with file count | 8, companion 6 |
| S3 ruling (2026-10-11) | Arm 3 is not final: re-measure both DELETE shapes on the real `prune_content_files` phase before any fallback. Arm 5 fails only a mis-specified, unattributed bar: rerun with wait-class attribution. Q1 stands, with a load note. Fence trigger names fix the firing order. PR3b-1 splits into 1a (no trigger) and 1b (trigger and seed). The startup check covers the fence triggers | 3.1, 3.2, 3.5, 7, 8, companion 2 and 6 |
| arb-8155-f1 (2026-10-16) | Review finding F1: the immediate fence deadlocked a retention chunk cycle (two content DELETEs in one transaction). The bump moves to COMMIT through a pending table and a deferred constraint trigger. F2 seed in `repo_id` order. F3 and F4 text plus source guards | 3.1, 7, companion 1 and 2 |
| arb-8155-c1 (2026-10-16) | Arm 3 bar restated to an absolute +0.25 s `ScopeLockHold` budget per 100,000 doomed rows. The retention bump cannot be skipped. The `work_mem` spill is a follow-up, out of PR3b-1b | 8, companion 6 |
| G9 stage log | the new fields land in PR3c | 3.5 |
| R-1 and G3 (lost update) | decided: the version fence replaces the mark protocol | companion 7 |

<details>
<summary>Evidence and measurements</summary>

See [the evidence appendix](7126-ingest-time-config-artifacts-evidence.md).
It holds the measurement table with sources and limits, the source facts by
symbol, and the split between evidence and theory.

</details>
