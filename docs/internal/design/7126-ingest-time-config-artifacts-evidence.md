# Evidence for the config-artifact derive decision (#7126)

This appendix belongs to
[Derive repository config artifacts when content is written](7126-ingest-time-config-artifacts.md)
and its [protocol companion](7126-ingest-time-config-artifacts-protocol.md).
It holds the measurements the decision rests on, the source facts, and the
split between evidence and theory. No measurement was taken for this record.
Every figure below comes from an earlier read-only probe, a local bench, a
local kit note (named where used, not in the repository), or an arbiter
ruling of 2026-10-10 or 2026-10-11. Source facts are read at `origin/main`
`b7ca5f2f0a`. The cited paths are unchanged at `6ddfc65d55`. The S3 ruling
read the retention source at `main` `64f6600d98`.

## 1. Measurements and their limits

All QA-environment figures are read-only probes of 2026-10-10 on one repository story
(R1) with a second repository (R2) as a contrast.

| Measurement | Value | Source | Limit |
|---|---|---|---|
| `deployment_artifacts` stage, 4 R1 calls | 6.953, 6.750, 6.627, 6.643 s | [comment 6097810983](https://github.com/eshu-hq/eshu/issues/7126#issuecomment-6097810983) | one repository, one image. Call 1 is first-in-session, not a guaranteed cold read |
| Route time, same calls | 7.969, 7.037, 6.919, 6.933 s | same | client wall times are load-affected. Server figures are not |
| Stages outside `deployment_artifacts` | 1.016 s on call 1, about 0.29 s on calls 4-6 | same, route time minus stage time | `target_documentation` 0.498 s cold and 0.025-0.026 s warm come from the stage log lines in the local kit note `7126-story-span-probe-20261010.md`, not in the repository |
| Reader statements per story | R1 8,255-8,258. R2 34 | same, `eshu_dp_postgres_reader_member_attempts_total` delta | the list/get split is extrapolated from a truncated trace |
| Neo4j reads per story | 0.143 s call 1, 0.010 s calls 4-6 | same | at most 2% of the stage |
| Related repositories N | at least 141 (distinct `source_repo`). About 300 by extrapolation | same | N was not counted directly |
| Database execution | about 0.03 ms per file, single statement | [comment 6098312440](https://github.com/eshu-hq/eshu/issues/7126#issuecomment-6098312440) | host load1 17-32 on the client side. Figures labeled non-comparable |
| API CPU per R1 story | 6.70-6.88 CPU-s. CFS throttled 0.40-0.59 s | [comment 6098498452](https://github.com/eshu-hq/eshu/issues/7126#issuecomment-6098498452) | no pprof on the pod. Split between per-statement cost and parse is an inference |
| Per statement work | 6 spans, 5 histogram records, 1 counter add, 3 round trips | same, source read | about 49,600 spans per R1 story |
| Hydrated content mix | 4,632 files, 52.2 MB. JSON 69.4% of bytes, never parsed unless a kustomization references it (0 here). HCL 2,498 files, 12.37 MB | [comment 6098969523](https://github.com/eshu-hq/eshu/issues/7126#issuecomment-6098969523) | the about 160 repositories the body does not name are NOT_CHECKED |
| Local CPU bench, 300 repositories, 8,000 synthetic files | shipped 3.72 CPU-s median. Parse skipped 0.91. Batched 2.99. Output identical in all arms | local kit note `7126-cpu-bench-20261010.md`, sections 3 and 5, not in the repository. The comment gives the 20% batching saving and the failed anchor | anchor failed: 0.54 of the pod figure. Only arm comparisons hold |
| HCL literal gates | 1,250 of 2,498 HCL files pass no scan gate. Projected regex work 18-19% of ungated | [comment 6100249949](https://github.com/eshu-hq/eshu/issues/7126#issuecomment-6100249949) | a projection from laptop rates. No gated parse ran on real content |

The PR1 and PR1b figures in the decision record come from their review
verdicts: PR1 round 2 CLEAN at `fe1c35698f`, PR1b round 3 CLEAN at
`5d4cda0767`. PR2 was at `f5423842a2` (a chunked content read) when this
record was last revised. None of the three is merged or published as a PR on 2026-10-10. The PR1
and PR1b verdicts are local kit notes, not in the repository.

## 2. Source facts behind the design

Paths are under `go/internal/query/repositoryartifacts/` unless a row says
otherwise. PR3a moved the extractors, the hydration plan, the per-repository
function and the `content_files` statements to `go/internal/content/artifacts/`
(`CA/` below). Each source names a symbol, not a line.

| Fact | Source |
|---|---|
| Related repositories come from one Neo4j UNION over eight relationship types, then a grant filter, before any file is read | `QueryRelatedRepositoryArtifactSources` and `FilterRepositoryArtifactSourcesForAccess` in `repository_config_artifacts_loader.go` |
| Related repositories are visited one at a time. Each lists its files with a 5,000-row limit (`ListLimit`, pinned to `querycontract.RepositorySemanticEntityLimit`) and no sentinel | `loadRepositoryConfigArtifactsForSources` in the same file, which calls `LoadConfigPaths` in `CA/repository.go` |
| The hydration set is the first 50 candidates in list order that carry no content, read 8 at a time | `SelectHydrationIndexes` and `HydrationLimit` in `CA/hydration_select.go`, `ReadChunkSize` and `ReadChunksInFlight` in `CA/hydration_chunked.go`. The anchor's one-file full pass is `HydrateRepositoryCandidateFiles` in `repository_artifact_hydration.go` |
| Blank content is dropped before the build. Rows are deduplicated on `path|source_repo|relative_path|evidence_kind` and sorted on those four fields in that order (PR3a added `evidence_kind`; before it the sort used the first three with `sort.Slice`) | `hydrateConfigFiles` in `CA/hydration.go`, `MergeConfigPaths` and `CompareConfigPaths` in `CA/build.go` |
| Each row has four fields. `source_repo` is the name passed in | `appendConfigArtifactRow` in `CA/build.go`, `extractKustomizeConfigPathRows` in `CA/kustomize.go`, `extractAnsibleConfigPathRows` in `CA/ansible.go` |
| The extractors drop remote references and do not read them | `localKustomizeRefs` in `CA/kustomize.go`, `normalizeComposeLocalConfigPath` in `CA/compose.go`, `isLocalishConfigArtifactPath` and `normalizeLocalConfigAssetPathValue` in `CA/hcl.go` |
| The anchor repository passes its own file list. Compose and GitHub Actions files are read first, and a file with content is not counted against the 50 cap | `LoadDeploymentArtifactOverview` and `hydrateRepositoryArtifactFiles` in `repository_deployment_artifacts_loader.go`, `HydrateRepositoryCandidateFiles` |
| Three routes call the loader | `getRepositoryStory` in `go/internal/query/repository/handler.go`, `getRepositoryContext` in `repository/context.go`, `loadServiceDeploymentEvidence` in `go/internal/query/service/deployment_evidence.go` |
| `content_files` is keyed on `(repo_id, relative_path)` and is latest-wins | `contentStoreBaseSchemaSQL` in `go/internal/storage/postgres/schema_content_store.go`, migration `169_content_files_generation_id.sql` |
| The content writer already derives a read model after its content statements | `deriveInfraInventory` in `go/internal/storage/postgres/content_writer_upserts.go` |
| `ingester`, `projector` and `bootstrap-index` construct the content writer | `buildIngesterProjectorRuntime` in `go/cmd/ingester/wiring.go`, `buildProjectorRuntime` in `go/cmd/projector/runtime_wiring.go`, `buildBootstrapProjector` in `go/cmd/bootstrap-index/wiring.go` |
| The infra read model backfill runs in the API process today | `StartInfraInventoryBackfill` in `go/internal/query/cloud_resource_owner_backfill.go` |
| Rows from HCL and compose pass through `normalizeRepoScopedPath` (the clean step `CleanRelativePath`, then the strip step `StripRepositoryName`, both in `CA/paths.go`). Kustomize and Ansible rows do not. `nameScopedByEvidenceKind` in `CA/name_scope.go` holds the scope of every kind | `appendConfigArtifactRow` and its callers in `CA/hcl.go` and `CA/compose.go`, against `extractKustomizeConfigPathRows` and `extractAnsibleConfigPathRows` |
| The anchor list is read with limit+1 and sets a truncated flag | `loadRepositorySemanticOverview` in `go/internal/query/repository/semantics.go` |
| `content_files` batches run one after another inside one Write. Entity batches run concurrently and do not write `content_files` | `upsertContentFileBatches` and `upsertContentEntityBatches` in `go/internal/storage/postgres/content_writer_upserts.go` |
| Generation retention deletes `content_files` rows, and only the reducer wires retention | `pruneContentFilesForGenerationsQuery` in `go/internal/storage/postgres/generation_retention_sql.go`, `go/cmd/reducer/generation_retention_wiring.go` |
| Migration 109 uses statement-level INSERT and UPDATE triggers with transition tables and a row-level DELETE trigger | `109_infra_resource_entities.sql` |
| Triggers of one event on one relation fire in alphabetical order by name. Row-level AFTER triggers fire before statement-level AFTER triggers | PostgreSQL 18 documentation, trigger definition. The names of the migration 138 triggers are in `138_content_file_secret_lines.sql` |
| The retention transaction sets `work_mem` to 64 MB, times named phases, and runs the content prune as one DELETE in the phase `prune_content_files`. The row limit 100,000 counts facts and content rows together | `generationRetentionWorkMemStatement`, `GenerationRetentionPhasePruneFiles`, `defaultGenerationRetentionRowLimit` in `go/internal/storage/postgres/generation_retention.go` |
| Row-level AFTER triggers fire at the end of the statement. Retention deletes `content_files` in one statement per transaction | PostgreSQL trigger documentation, and the retention step list of `GenerationRetentionStore` in `go/internal/storage/postgres/generation_retention.go` |
| No import cycle blocks a shared package | `go list -deps` on `./internal/query/querycontract`, `./internal/query/repositoryartifacts`, `./internal/content` and `./internal/storage/postgres` at `b7ca5f2f0a` |
| The newest migration on `origin/main` | `169_content_files_generation_id.sql`. No open PR adds a migration (checked with `gh pr list --state open` on 2026-10-10) |

## 3. Evidence versus theory

Evidence (observed or read from source):

- The stage is the R1 cost, and it is CPU in the API process, not database
  time or Neo4j time.
- The statement count is about 8,250 per R1 story, against 34 for R2.
- The extractor output depends only on the repository name and that
  repository's stored files.
- The infra read model shows the derive, fence, backfill and reconcile pattern
  works in this content writer.
- Name-free rows plus the read-side name step equal the named rows over the
  S0 corpus (section 4).
- The stored reads meet 5 ms at 300 ids on a laptop with synthetic paths
  (S2, section 4).
- A four-column TEXT key fails at a 2,610-byte incompressible path (S2).
- S3 arms 1, 2, 4 and 6 pass on a private PostgreSQL 18.6, with a load note
  on arms 1, 2 and 6 (section 6).

Theory (each has a shim in section 8 of the decision record):

- The derive adds an acceptable cost to each content Write (S1).
- The fence trigger adds under 5% to a content upsert (S3).
- Stored rows equal live rows for every repository (S4).
- The loader-level merge cases (two repositories with one graph name, an
  anchor repeated in the related list) equal the live loader (S4, PR3c).
- The reader join with the digest key stays under 5 ms (not measured).
- The projector repository name equals the graph `Repository.name` (S5,
  optional after S0).
- The stage drops to 0.25 s or less, the route to 1.0 s or less warm, and API
  CPU to 0.7 CPU-s or less per R1 story. These targets are a budget built from
  the contributor list, not a measurement.
- The estimate of 2.5-4.5 s for PR1 plus PR2 in the QA environment.
- The content-version trigger meets S3 arm 3 (on the real `prune_content_files`
  phase) and arm 5 (with attributed waits). Both rerun (sections 6 and 7).
  The R-1 lost update itself is a source-backed sequence, decided by the
  re-ruling.
- The fallback of the protocol companion (section 2) is a design, not a
  measurement. It applies only if the arm 3 rerun fails both shapes.

NOT_CHECKED: S2 on real R1 path widths, a disk-cold read, the production
environment, concurrent stories, and the cold route after a pod restart. From
the S3 ruling: the cause of the 2.0 s heavyweight wait in arm 5, the real
`prune_content_files` duration at 100,000 rows (no figure is recorded), that a
transition table is bounded by `work_mem` (a recollection of the PostgreSQL
source), that a GIN pending-list cleanup by an inserting backend takes the
metapage lock conditionally (a recollection; if wrong, `page` is a fifth wait
class, which the `wait_event` column shows either way), and the QA-environment
p95 of `eshu_dp_generation_retention_scope_lock_hold_seconds`.

## 4. S0 and S2 results (local, 2026-10-10)

Source: local kit note `7126-pr3-shims-s0-s2-20261010.md` and its script
directory, not in the repository. The S0 tests are on the local branch
`proof/7126-pr3-shims` at `953f68712e`, not published.

S0 (Go test, `go test -run TestS0`, exit 0, and exit 0 with `-race`):

| Item | Result |
|---|---|
| Corpus | 42,948 name-to-file-set pairs, 684,948 row observations |
| Scoped rows (10 HCL and compose kinds) | 162,609 observations, 0 differing |
| Unscoped rows (6 kustomize and Ansible kinds) | 522,339 observations, 0 differing, 0 paths changed by the name |
| Raw rows that collapse at read | 11, all in planted trees, 0 in natural corpora |
| Seeded RED: name step on every kind | 10,068 differing rows |
| Seeded RED: no read dedup | 11 differing rows |
| Seeded RED: raw sort order | 386 order-only differences |
| A second clean of a cleaned path | differs on 8,943 of 200,000 random strings, 0 over the corpus |

S2 (PostgreSQL 18.6, generic plans, 5 runs, medians, 815 repositories,
17,175 rows):

| Statement, 300 ids | Median | Plan |
|---|---|---|
| Paths read, four-column key | 1.74 ms | Index Only Scan on the key |
| Paths read after a rewrite with no VACUUM | 2.47 ms | Index Only Scan, 9,202 `Heap Fetches` |
| Paths read after the same rewrite and a VACUUM | 1.76 ms | Index Only Scan, 0 `Heap Fetches` |
| Paths read, digest key | 2.14 ms | Bitmap Index Scan and Bitmap Heap Scan |
| State read | 0.85 ms | Bitmap scan on the key |
| Reader three-table join | 2.44 ms | Hash join over a seq scan of the 9-page versions table, then Index Only Scan on the paths key |
| Reader join with every key index forced | 2.9-3.2 ms | Nested loop over index scans |
| Dirty join, all 815 repositories | 0.18 ms | Hash join over two seq scans |

Caveats: host load1 was 6-10 during S2, so every time is an upper bound for
that host and is not comparable to the QA environment. The OS cache was warm, also after a
server restart. Path widths come from synthetic md5-derived components, not
from R1's real paths. S2 set the versions directly, with no trigger. The S2
reader condition also excluded repositories with a failed derive.

## 5. Generated-column and trigger-timing checks (local, 2026-10-10)

A throwaway PostgreSQL 18.6 cluster, started for the check and deleted after
it, with no Eshu data. The SQL below reproduces each row of the table.

| Check | Result |
|---|---|
| `provolatile` of `convert_to(text, name)` and `textsend(text)` | `s` (STABLE) for both. `sha256` and `length(text)` are `i` (IMMUTABLE) |
| `CREATE TABLE` with the generated column of re-ruling 2 | `ERROR: generation expression is not immutable` |
| Same column through an IMMUTABLE SQL wrapper over `convert_to(t, 'UTF8')` (adopted by re-ruling 3) | table builds. 4 rows insert, 4 distinct digests, including `('a|b', 'c')` and `('a', 'b|c')`. The digest of `'3:a|b1:ck'` matches a manual `sha256(convert_to(...))` |
| AFTER ROW DELETE trigger timing over 9 rows. BEFORE and AFTER row triggers log a sequence | all 9 BEFORE events (1-9) precede all 9 AFTER events (10-18). AFTER row triggers fire at the end of the statement, as the PostgreSQL trigger documentation states. Re-ruling 3 adopted this |

```sql
-- Volatility of the functions in the digest expression.
SELECT proname, provolatile FROM pg_proc
WHERE proname IN ('convert_to', 'textsend', 'sha256', 'length') ORDER BY 1;

-- The expression without a wrapper: fails, not immutable.
CREATE TABLE t (repo_id text NOT NULL, path text NOT NULL,
  relative_path text NOT NULL, evidence_kind text NOT NULL,
  row_digest bytea GENERATED ALWAYS AS (sha256(convert_to(
    length(path)::text || ':' || path || length(relative_path)::text
    || ':' || relative_path || evidence_kind, 'UTF8'))) STORED,
  PRIMARY KEY (repo_id, row_digest));

-- The IMMUTABLE wrapper: builds, and the digests stay distinct.
CREATE FUNCTION config_artifact_utf8(t text) RETURNS bytea
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE RETURN convert_to(t, 'UTF8');
CREATE TABLE b (repo_id text NOT NULL, path text NOT NULL,
  relative_path text NOT NULL, evidence_kind text NOT NULL,
  row_digest bytea GENERATED ALWAYS AS (sha256(config_artifact_utf8(
    length(path)::text || ':' || path || length(relative_path)::text
    || ':' || relative_path || evidence_kind))) STORED,
  PRIMARY KEY (repo_id, row_digest));
INSERT INTO b (repo_id, path, relative_path, evidence_kind) VALUES
  ('r', 'a|b', 'c', 'k'), ('r', 'a', 'b|c', 'k'),
  ('r', repeat(md5('x'), 200), 'main.tf', 'k'), ('r', 'é/ü', 'x', 'k');
SELECT count(*), count(DISTINCT row_digest) FROM b;
SELECT row_digest = sha256(convert_to('3:a|b1:ck', 'UTF8')) FROM b
WHERE path = 'a|b';

-- AFTER ROW timing: every BEFORE event precedes every AFTER event.
CREATE SEQUENCE ev;
CREATE TABLE evlog (n bigint, kind text, repo_id text, rel text);
CREATE TABLE cf (repo_id text, relative_path text,
  PRIMARY KEY (repo_id, relative_path));
INSERT INTO cf SELECT 'r' || (g % 3), 'p' || g FROM generate_series(1, 9) g;
CREATE FUNCTION before_del() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN INSERT INTO evlog VALUES (nextval('ev'), 'before_row', OLD.repo_id,
  OLD.relative_path); RETURN OLD; END $$;
CREATE FUNCTION after_del() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN INSERT INTO evlog VALUES (nextval('ev'), 'after_row', OLD.repo_id,
  OLD.relative_path); RETURN NULL; END $$;
CREATE TRIGGER b BEFORE DELETE ON cf FOR EACH ROW EXECUTE FUNCTION before_del();
CREATE TRIGGER a AFTER DELETE ON cf FOR EACH ROW EXECUTE FUNCTION after_del();
DELETE FROM cf;
SELECT kind, min(n), max(n), count(*) FROM evlog GROUP BY kind ORDER BY 2;
```

## 6. S3 results (local, 2026-10-11)

Source: local kit notes `7126-s3-results-20261011.md` and
`7126-s3-ruling-20261011.md` (the arbiter's reading of the logs), and the shim
branch `proof/7126-s3-fence-cost` at `772376924b`. None is in the repository.
Private native PostgreSQL 18.6, default configuration (`shared_buffers`
128 MB, `work_mem` 4 MB, `fsync` on). Rule PD: load1 below 9.0 as the in-run
maximum. The run wrapper waits for load1 below 7 for 20 s, then samples it
every second. The control canary is arm A of the same round.

| Run | Arms | In-run max load1 | Valid under PD | Note |
|---|---|---|---|---|
| A | 1, 2, 4, 6 | 17.63 | No | A peer ran a `git grep` burst. Arm 4 read +41.86%. Void |
| A4r2 | 4, 9 rounds | 5.05 | Yes | A 2.823 s, B 2.854 s, +1.07%, ranges overlap |
| Q1 | 1, 2, 6 | 9.88 | Accepted with a deviation note | The peak lasted 25 s (15:04:51-15:05:15). By the subtest durations (arm 1 132.19 s, from about 15:02:20) it falls in arm 2, which began about 15:04:32. The in-arm maximum of arm 1 was 7.25 (15:02:36). The shim's own test binary and database backend (arm 2 seeding) and foreign processes (a system profiler, Spotlight, a peer's build and docs scan) were all running. The arbiter read the peak as overlapping the last round of arm 1. The logs place it in arm 2, which still passes (+1.47% against +5%, ranges overlap). Arm 6 counts are exact, so load cannot change them. Arm 1 rides along with the arm 5 rerun, and a reversal there reopens it |
| Q3 | 3 | 5.62 | Yes | |
| Q5 | 5 | 7.47 | Yes | A spread 9.23-16.14 s (1.75x). The 7.47 was the test binary's link step before the arm started |

| Arm | Measure | A | B | Delta | Bar | Result |
|---|---|---|---|---|---|---|
| 1 | full Write, 5,000 files incl. derive (Q1) | 10.678 s | 10.754 s | +0.71% | +5% | PASS |
| 1 | trigger only (Q1) | 10.678 s | 10.749 s | +0.66% | +5% | PASS |
| 1 micro | 50,000 rows, 500-row statements (Q1) | 141.2 ms | 153.0 ms | +0.236 us/row | 0.5 us/row | PASS |
| 2 | delta Write, 1 file + 3 tombstones (Q1) | 2.322 ms | 2.356 ms | +1.47% | +5% | PASS |
| 4 | one 50,000-row INSERT (A4r2) | 2.823 s | 2.854 s | +1.07% | +5% | PASS |
| 6 | version-row rewrites (Q1) | exact | exact | 0 off | exact | PASS: full 10, delta 2, reap 4, bulk 1, prune 50 on both shapes |
| 6 | seeded per-row trigger | RED (5,000 vs 10) | | | | RED then GREEN |
| 3 | prune 100,000 rows, 50 repos, row-level DELETE (Q3) | 528.1 ms | 817.4 ms | +54.79% | +5% | FAIL on a proxy denominator |
| 3 | same, `OLD TABLE` DELETE (Q3) | 528.1 ms | 662.5 ms | +25.46% | +5% | FAIL on a proxy denominator |
| 5 | per Write, trigger last (Q5) | 11.844 s | 12.649 s | +6.79% | +10% | PASS |
| 5 | per Write, trigger first (Q5) | 11.844 s | 11.623 s | -1.87% | +10% | PASS |
| 5 | slower of pair, trigger last / first (Q5) | 12.029 s | 13.535 s / 12.202 s | +12.52% / +1.44% | +10% | not a protocol bar |
| 5 | longest version-row lock wait, last / first (Q5) | | 2.018 s / 1.950 s | | one solo batch (0.969 s) | FAIL on an unattributed wait |
| 5 | deadlocks over 20 rounds | | 0 | | 0 | PASS |

What the S3 ruling says about the two failures:

- Arm 3 is not final, because the shim measured a proxy, not the protocol
  denominator. Four deviations each inflate the ratio. (1) The shim deletes
  from a pre-materialized key table. The real statement first computes the
  candidate keys from `fact_records` and probes each key. The shim log
  itself calls its denominator a lower bound. (2) The shim bodies are
  1,024 bytes (the ruling read the shim source; the results note says 512),
  below the TOAST threshold, so the `OLD TABLE` transition table carries the
  full content of 100,000 rows. Production 8 KiB bodies are TOAST pointers.
  Small files stay inline in production, so this class is real, but it is
  the worst case. (3) The shim ran at the server `work_mem` of 4 MB. The
  real transaction sets 64 MB. (4) The shim has no advisory locks and not
  the eight other statements. That is fine for the trigger delta and wrong
  for the denominator.
- The numbers that survive are the per-row costs: `OLD TABLE` +1.344 us and
  row-level +2.893 us per row. The ranges are disjoint from A (A 517-542 ms,
  `OLD TABLE` 641-769 ms, row-level 799-864 ms). At the row limit of 100,000
  the worst-case add is about 134 ms and 289 ms per retention cycle, with
  inline bodies and 4 MB `work_mem`. This is context, not the protocol bar.
- Arm 5: the bar of one solo batch is mis-specified. Under two writers a
  batch takes about 1.2-1.8 s, and a waiter can only wait for the holder's
  remaining hold. The sampler counts every `Lock` wait of any type and cannot
  name the version row. Arm A's waits were sampled and never reported, so
  there is no control. Candidates that exist in A too are an `advisory` wait
  (both Writes end in `inventory.Mirror`), an `extend` wait, and sampler
  merging of short waits. With the trigger last, the version row is held for
  a commit, so a 2.0 s hold on it is not credible. Both modes show the same
  maximum, and trigger first shows less total wait (501 ms against 771 ms
  per round). A cause outside the version row fits. This is an inference,
  not a measurement. The rerun measures it.
- The slower-of-pair metric is the shim author's addition. Its +12.52%
  inverts against trigger first (+1.44%), over an A spread of 1.75x, so it is
  noise-dominated. It is neither a pass nor a defect. The -1.87% of trigger
  first is inside the noise band and does not overturn the argument for a
  trigger that fires last.
- Not run at full size: the variant that strips secret lines (it isolates the
  version trigger from migration 138), arm 5 with overlapping keys, and a
  first Write of a repository with no seeded version row.
- Arm 3 write probe (informational): a 500-file Write of a pruned repository
  waited for the prune commit. With a 250 ms tail the extra wait was 227 ms
  (`OLD TABLE`) and -335 ms (row-level, sample noise). Waits were on the
  version row only (disjoint content keys).

## 7. S3 rerun specification (S3 ruling, 2026-10-11)

Both reruns use the private PostgreSQL 18.6 and the PD rule: a quiet host,
load1 below 9.0 for the whole run, arm A as the control canary.

Arm 3, on the real retention statement:

- Statement: the real `GenerationRetentionStore.PruneSupersededGenerations`
  (or the in-package `pruneCandidates`), so `SET LOCAL work_mem`, the advisory locks and
  `pruneContentFilesForGenerationsQuery` all run. Denominator:
  `PhaseDurations["prune_content_files"]`. Also record `ScopeLockHold` and
  the whole-transaction wall.
- Fixture: 50 scopes, one superseded prunable generation each, 2,000
  `content_files` rows per repository (100,000 rows), a non-tombstone `file`
  fact per row in the doomed generation and none retained, so every row is
  doomed. Set `BatchRowLimit` high enough to admit one transaction (the
  default 100,000 counts facts and content rows together; the note says so).
  Build the schema as `generation_retention_large_fixture_live_test.go` does:
  migrated, key indexes present.
- Arms: A (no version trigger), B1 (row-level DELETE trigger), B2 (`OLD TABLE`
  DELETE trigger). Interleaved, first mover rotating, 5 measured rounds after
  one discarded round (the shim's `versionFenceInterleave`).
- Body classes, both required and reported separately: 1 KiB inline and 8 KiB
  TOASTed.
- Pass: the chosen shape at most +5% of `prune_content_files` on both
  classes. At equal cost prefer `OLD TABLE`. Report the `ScopeLockHold` delta
  in seconds as informational. Fail: both shapes over 5% on either class.
  The fallback of the protocol companion then becomes available as the
  Proposed fallback, which needs an F2 amendment by ruling. Without it,
  retention pays the trigger.
- The arm 3 write probe of section 6 (a Write of a pruned repository waits
  for the prune commit) is informational. It is not part of the rerun bar.

Arm 5, attributed:

- Sampler first, as its own shim commit: add `wait_event` to the sample SQL.
  For `transactionid` and `tuple` waits, add the blocking pid
  (`pg_blocking_pids`) and the waiter's query prefix. Classify each episode:
  `version_row` (the waiter runs a content INSERT, UPDATE or DELETE and the
  blocker runs a content statement of the other writer), `advisory`,
  `extend`, `other`. Report the maximum and the total per round, per class
  and mode, including A.
- Runs: modes A, B-last and B-first, 20 measured rounds, with disjoint keys
  (default). Then the same with the shim's overlap switch (two scopes
  writing the same paths: the production shape), 20 rounds.
- Bars: per Write at most A + 10%. `version_row`: A must be 0. The B-last
  maximum is at most one contended batch (B-last per-Write median / 10). The
  B-first maximum is at least the B-last maximum. If it is not, the
  classification is wrong and the run is void. `advisory` and `extend` are
  equal between A and B within the A spread. 0 deadlocks in 20 rounds.
- Add arm 1 (5 rounds, default sizes) to the same session to confirm Q1.

## 8. Delete paths on `content_files` (S3 ruling, 2026-10-11)

Source: `rg` over `go/` for `DELETE FROM content_files` and
`TRUNCATE ... content_files`, outside tests. This is the inventory the
fallback needs. Every path keeps the trigger except the one statement that
bumps itself.

| Path | Shape | Fence under the fallback |
|---|---|---|
| `pruneContentFilesForGenerationsQuery` in `generation_retention_sql.go` (reducer only) | one statement per transaction | self-bump in the statement. The trigger is gated off by `SET LOCAL` |
| `deleteContentFilesChunk` in `content_writer_delete_batch.go`: tombstones and the full-snapshot reap, autocommit, chunks of 500 | row-level trigger, at most 500 rows (500 x 2.893 us is about 1.45 ms, an extrapolation from the non-final arm 3 proxy) | trigger. One bump per chunk. Arm 6 counts 4 in the reap case (1,200 reaped paths plus 3 tombstones) |
| `deleteContentFileQuery` through `ContentStore.Write` (`content_writer_sql.go`, `content_store_writes.go`) | one row per statement | trigger. `NewContentStore` has no caller outside tests (`rg`, not a build-graph query), and it stays fenced |
| `TRUNCATE content_files` | no production path (a note in `go/internal/storage/postgres/secret/lines/README.md`) | the statement-level TRUNCATE trigger bumps every version row |
| Manual SQL, migrations, older binaries | arbitrary | trigger |
| Repository retirement (#7766, draft PR #7871) | future | must adopt one disposition for the versions and state tables (protocol companion, section 1). The DELETE trigger covers its content delete, unless it used the gate, which it must not |
| Foreign key cascades into `content_files` | none. `content_files` references no parent (`contentStoreBaseSchemaSQL`, migration 004). `content_file_secret_lines` is its child (migration 138) | not applicable |

## 9. Limits on the deployment-artifact chain (cap ruling, 2026-10-11)

Source: the local kit note `7126-cap-fix-ruling-20261011.md` (the arbiter's
reading, not in the repository). Each symbol was re-checked with `git grep`
at `origin/main` `75d40f219e` on 2026-10-11. Paths are under
`go/internal/query/`. The PR3a branch (`ef4430bb5d`, not merged) moves the
selection to `content/artifacts/`. Symbols, not lines, because a raw line
citation fails the citation gate.

| # | Where (file, symbol) | Limit today | Disclosed today | End state |
|---|---|---|---|---|
| L1 | `repositoryartifacts/repository_config_artifacts_loader.go`, `loadRepositoryConfigArtifactsForSources`: `ListRepoFiles` with `RepositorySemanticEntityLimit` | 5,000 files per related repository, no sentinel | No | Read limit+1, clip, set a per-repository `ListTruncated`, count per request, the window reason (section 10) |
| L2 | `repositoryartifacts/repository_artifact_hydration.go`, `selectHydrationIndexes` and `repositoryArtifactHydrationLimit` (= 50). PR3a: `content/artifacts/hydration_select.go`, `SelectHydrationIndexes`, `HydrationLimit` | first 50 candidates per related repository | No | Lifted. Selection takes an allowance: floor 50 plus a share of the request budget. `HydrationLimit` becomes `HydrationFloor` |
| L3 | `repositoryartifacts/repository_config_artifacts_hydration.go`, `hydrate`: kustomize resource reads only among the selected candidates (`working`) | derived from L2 | No | Follows L2. A resource the kustomization names inside the list window is read |
| L4 | `repository/semantics.go`, `loadRepositorySemanticOverview` | 5,000, anchor, limit+1 | Yes (`repository_semantic_read_truncated_at_5000`) | Unchanged |
| L5 | `repository/context.go`, `getRepositoryContext`: `ListRepoFiles` with `fileLimit+1` | 5,000, anchor | Yes (`repository_context_file_read_truncated_at_5000`) | Unchanged |
| L6 | `service/deployment_evidence.go`, `loadServiceDeploymentEvidence`: `ListRepoFiles`, then `LoadDeploymentArtifactOverview` (no stats) | 5,000, anchor, no sentinel. L1 and L2 for its related set | No | limit+1, clip, append the reason to `workloadContext["limitations"]` (the slot exists in `service/query_enrichment.go`), call `LoadDeploymentArtifactOverviewWithStats`, log the counts on the `deployment_evidence` stage |
| L7 | `repositoryartifacts/repository_deployment_artifacts_loader.go`, `hydrateRepositoryArtifactFiles` to `HydrateRepositoryCandidateFiles` (compose and GitHub Actions pre-read, anchor) | 50, per-file reader | No | PR-cap-2: chunked reader, same allowance and disclosure |
| L8 | `repositoryartifacts/repository_config_artifacts_loader.go`, `LoadRepositoryControllerArtifacts` to `HydrateRepositoryCandidateFiles` (anchor) | 50. `ListRepoFiles` at 5,000 with no sentinel when `files` is nil | No | PR-cap-2 |
| L9 | `repositoryartifacts/repository_runtime_artifacts.go`, `loadRepositoryRuntimeArtifacts` to `HydrateRepositoryCandidateFiles` (anchor) | 50. List at 5,000 with no sentinel when `files` is nil | No | PR-cap-2 |
| L10 | `repositoryartifacts/repository_workflow_artifacts_loader.go`, `LoadRepositoryWorkflowArtifacts` (anchor) | 50. List at 5,000 with no sentinel when `files` is nil | No | PR-cap-2 |
| L11 | `repositoryartifacts/workflow_evidence.go`, `StaticWorkflowArtifactEvidence` | 5,000 with limit+1. Path list capped by `cicdStaticWorkflowEvidencePathLimit`, with `truncated` | Yes (`candidate_pool_status`, `repository_file_scan_limit_reached`) | Unchanged. It is the disclosure precedent to copy |

Re-check result: every symbol above exists at `origin/main` `75d40f219e`,
except the PR3a names in L2 (and the allowance parameter of `LoadConfigPaths`),
which exist only on the PR3a branch.

Routes that share the loader: `getRepositoryStory` and `getRepositoryContext`
in `repository/`, and the service story and context fallback
(`loadServiceDeploymentEvidence`). The MCP tools `get_repo_story` and
`get_repo_context` are transport-only proxies and inherit the fix. The source
order is backend-dependent today (the Neo4j UNION order). The body does not
depend on it now (one dedup, one sort). A request budget makes it matter, so
the access-filtered sources are sorted by `RepoID` before the walk.

## 10. Request budget, proof and acceptance plan (cap ruling, 2026-10-11)

Why a bound. With L2 gone, the only per-repository bound is the 5,000-file
window, so the worst case per request is N x 5,000 candidates: 1.5 million
at N about 300 (an extrapolation, decision record section 1). That fails the
performance and reliability contract. The bound is a request budget known
before any read, so a repeated request has a stable body. Wall time was
rejected as a unit (a host-dependent body), and bytes too (the list statement
returns no size). Serialization and a lower worker count are no part of the
control. The chunk size 10 and the in-flight bound 8 stay.

Estimate (theory: measured per-statement and per-MB rates applied to the known
hidden set of 3,763 candidates, from the local kit note
`7126-batch-shim-20261010.md`; the E = 12,000 row cites the scratch shim):

| Term | Estimate |
|---|---|
| Extra chunk statements for R1 | about 223 (3,763 x 0.592 / 10) |
| Statement CPU | about 0.18 CPU-s (223 x 0.82 ms) |
| Extra HCL bytes | about 10.1 MB (3,763 x 0.54 x 4,952 B) |
| Parse CPU | about 0.18 CPU-s gated, 0.95 ungated |
| R1 total | about +0.4 CPU-s (+0.2 to +1.1 across the mix brackets), +5% to +15% on a 2.5-4.5 s stage |
| Worst case at E = 10,000 (first proposal) | about +1.0 CPU-s gated (+3.0 if every gate misses), on top of the floors. Headroom over R1's known extra: 2.7x |
| Worst case at E = 12,000 | about +1.1 CPU-s gated (+3.6 if every gate misses), on top of the floors. It scales the E = 10,000 row by 1.2 at the same read and HCL shares and is not re-measured. The gated figure scales the unrounded 0.95 of that row (0.49 + 0.46 CPU-s, local kit note `7126-cap-fix-ruling-20261011.md`, section 2), so 1.14 rounds to +1.1, not 1.0 x 1.2. Headroom over R1's measured extra: 1.54x. Measured on the census seed with no bite, run `c_rounds9b` of the local kit note `7126-cap-shim-executor-handoff-20261011.md`: +0.487 CPU-s paired median (low edge of the +0.5 to +2.5 bracket), +232 chunk statements (below the +470 to +1,300 bracket; the census bound is 224 plus kustomize). Verdict PASS, read from the ruling (`7126-cap-fix-ruling-20261011.md`, section 3) and not from the shim note, which names only the digest and the superset as PASS: the ruling's theory to prove is that the lift "adds about 0.2-1.1 CPU-s and about 220-600 statements to an R1-shaped request, holds the per-request bound, and keeps the body a superset of today's body", and +0.487 and +232 are inside it. The ruling's pass bar ("within the section 2 bracket scaled to 300 repositories (about +0.5 to +2.5 CPU-s), statements about +470 to +1,300, and no change to A's digest") is missed on statements, on the cheaper side. The ruling sets no on-fail action for shim 1; it sets one only for shim 2. The shim kept one blank check per repository. Chunking at 500 adds at most about 18 blank-check statements on that seed (local kit note `7951-cap-lift-executor-handoff-20261011.md`, section 5) |
| Census rule (S6, 2026-10-11) | After S6, E must satisfy E >= 1.5 x R1's measured total extra over all related repositories, or E is raised and this table redone in the PR. S6 measured 7,774 hidden candidates over 317 related repositories (local kit note `7126-s6-census-results-20261011.md`): 1.5 x 7,774 = 11,661, so E = 10,000 failed (1.29x) and E is 12,000, provisional until the deployed read. A static check, not the live gate: no golden-corpus fixture has more than 24 config candidates (local kit note `7951-cap-lift-executor-handoff-20261011.md`, check 11) |

Disclosure the response and the stage line carry when a bound bites. Reasons
(story `limitations`, context `partial_reasons`, service story `limitations`)
each also set `answer_metadata.truncated`, which context lacks. The budget
reason is `deployment_artifacts_candidates_budget_reached` (E exhausted, and a
related repository has candidates in its window that were not hydrated). The
window reason is `deployment_artifacts_related_file_list_truncated_at_5000` (a
related list returned the sentinel row, so its pool past the window is
unknown). The anchor reason, service route only, is
`deployment_artifacts_anchor_file_list_truncated_at_5000` (its own list
returned the sentinel row). Counts, exact within the window, summed per
request, no repository id or path: `candidate_count`, `candidates_hydrated`,
`candidates_unhydrated`, `repositories_budget_capped`,
`repositories_list_truncated`, `hydration_budget` and `hydration_floor`. One
counter, `eshu_dp_config_artifact_candidates_total{outcome}`. The stage lines
are `deployment_artifacts` (story), `content_infrastructure_overview`
(context) and `deployment_evidence` (service).
`deployment_artifacts_read_degraded` is unchanged.

Proof before an executor starts (time box 2 h, prove-the-theory-first):

1. Cap shim on the `proof/7126-cpu-bench` harness with the observed tail
   (about 8,000 hidden candidates over the capped repositories). Arms, nine
   ABBA rounds, load1 under 9: A cap 50 (shipped), B F = 50 and E = 12,000
   (no bite), C F = 50 and a small E (bite). Pass: B - A within about +0.5 to
   +2.5 CPU-s at 300 repositories, statements about +470 to +1,300, A's
   digest unchanged, B's rows a superset of A's. Result: PASS under ruling section 3 (reading in the table); digest and superset hold.
2. Plan shim: `EXPLAIN (ANALYZE, BUFFERS)` of the blank-check and batched
   statements with arrays of 10, 500, 1,000 and 4,950 paths, custom and forced
   generic plan. Pass: index scan, no sequential or bitmap heap scan at 4,950.
   Result: failed as worded, see the last paragraph of this section.
3. Heap shim: peak heap of one 3,678-candidate repository against the API pod
   limit. Pass: growth under 25% of the limit. Result: PASS.

PR-cap-1 accuracy checks (each a behavior test): 51, 500 and 5,001 candidates
(exactly 5,000 gives no window reason); a budget bite through a loader test
seam, not an env var; byte-identical bodies for three shuffled source orders;
a monotonic differential (the new `config_paths` is a superset of the capped
one, byte-identical when no repository exceeds 50); kustomize inversion of
`TestNeededContentHydrationReferenceOutsideCapIsNotRead`; disclosure fields on
all three routes, absent when nothing bites; a seeded violation (selection
that stops at 50 and drops the reason) turning the tests RED; live plan and
equality tests at 4,950 paths; an out-of-grant repository adds nothing to any
count (counts follow the grant filter); the golden-corpus gate (`config_paths`
is in two cassettes); `-race`, OpenAPI and package-docs gates.

Roll image check (2026-10-11). `794b56a3fb` is the image the cap ruling named
(the merge of #7949, 04:22 UTC). `1ac63e871a` is the image the QA environment
was reported to run (the merge of #7946, 08:31 UTC; the environment's own
report is NOT_CHECKED here). `794b56a3fb` is an ancestor of `1ac63e871a`. Both
hold PR1 only.

| Commit | Change | In `794b56a3fb` | In `1ac63e871a` |
|---|---|---|---|
| `1be621eb11` | PR1, #7941 | yes | yes |
| `e8f934d294` | PR1b, #7959 | no | no |
| `050b0f5304` | this ADR, #7954 | no | no |
| `6b761d2145` | PR2, #7962 | no | no |

Command: `git merge-base --is-ancestor <commit> <image>`. Read 2 pins main
`6b761d2145` (the #7962 merge, an ancestor of `64f6600d98`), which holds PR1,
PR1b and PR2 and excludes PR3a, PR-cap-1 and PR3b-1a. Whether an image of it
is built is NOT_CHECKED here.

Shim results (local kit note `7126-cap-shim-executor-handoff-20261011.md`).
Shim 2 failed as worded: the custom plan is a bitmap heap scan at 1,171 and
4,950 paths (the generic plan is an index scan, and the flip size is not
stable), so the blank check is chunked at 500. Shim 3 passed: 34.7 MiB peak
growth, 0.42% of the 8 GiB chart limit. NOT_CHECKED: the plan production uses,
the QA overlay's memory limit, hidden rows beyond the 24 replayed
repositories, the live golden-corpus gate, and the CPU of a chunk statement.
