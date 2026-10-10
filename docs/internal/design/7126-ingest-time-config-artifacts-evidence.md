# Evidence for the config-artifact derive decision (#7126)

This appendix belongs to
[Derive repository config artifacts when content is written](7126-ingest-time-config-artifacts.md)
and its [protocol companion](7126-ingest-time-config-artifacts-protocol.md).
It holds the measurements the decision rests on, the source facts, and the
split between evidence and theory. No new measurement was taken for this
record. Every figure below comes from an earlier read-only probe, a local
bench, a local kit note (named where used, not in the repository), or the
arbiter ruling and re-ruling of 2026-10-10. Source facts are read at
`origin/main` `b7ca5f2f0a`. The cited paths are unchanged at `6ddfc65d55`.

## 1. Measurements and their limits

All ops-qa figures are read-only probes of 2026-10-10 on one repository story
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
otherwise. Each source names a symbol, not a line.

| Fact | Source |
|---|---|
| Related repositories come from one Neo4j UNION over eight relationship types, then a grant filter, before any file is read | `QueryRelatedRepositoryArtifactSources` and `FilterRepositoryArtifactSourcesForAccess` in `repository_config_artifacts_loader.go` |
| Related repositories are visited one at a time. Each lists its files with `querycontract.RepositorySemanticEntityLimit` (5,000) and no sentinel | `loadRepositoryConfigArtifactsForSources` in the same file |
| The hydration set is the first 50 candidates in list order that carry no content, read 8 at a time | `HydrateRepositoryCandidateFiles`, `RepositoryArtifactHydrationLimit` and `repositoryArtifactHydrationConcurrency` in `repository_artifact_hydration.go` |
| Blank content is dropped before the build. Rows are deduplicated on `path|source_repo|relative_path|evidence_kind` and sorted on `path`, `source_repo`, `relative_path` with `sort.Slice` | `loadRepositoryConfigArtifactsForSources` |
| Each row has four fields. `source_repo` is the name passed in | `appendConfigArtifactRow` in `repository_config_artifacts.go`, `extractKustomizeConfigPathRows`, `extractAnsibleConfigPathRows` |
| The extractors drop remote references and do not read them | `localKustomizeRefs`, `normalizeComposeLocalConfigPath`, `isLocalishConfigArtifactPath`, `normalizeLocalConfigAssetPathValue` |
| The anchor repository passes its own file list. Compose and GitHub Actions files are read first, and a file with content is not counted against the 50 cap | `LoadDeploymentArtifactOverview` and `hydrateRepositoryArtifactFiles` in `repository_deployment_artifacts_loader.go`, `HydrateRepositoryCandidateFiles` |
| Three routes call the loader | `getRepositoryStory` in `go/internal/query/repository/handler.go`, `getRepositoryContext` in `repository/context.go`, `loadServiceDeploymentEvidence` in `go/internal/query/service/deployment_evidence.go` |
| `content_files` is keyed on `(repo_id, relative_path)` and is latest-wins | `contentStoreBaseSchemaSQL` in `go/internal/storage/postgres/schema_content_store.go`, migration `169_content_files_generation_id.sql` |
| The content writer already derives a read model after its content statements | `deriveInfraInventory` in `go/internal/storage/postgres/content_writer_upserts.go` |
| `ingester`, `projector` and `bootstrap-index` construct the content writer | `buildIngesterProjectorRuntime` in `go/cmd/ingester/wiring.go`, `buildProjectorRuntime` in `go/cmd/projector/runtime_wiring.go`, `buildBootstrapProjector` in `go/cmd/bootstrap-index/wiring.go` |
| The infra read model backfill runs in the API process today | `StartInfraInventoryBackfill` in `go/internal/query/cloud_resource_owner_backfill.go` |
| Rows from HCL and compose pass through `normalizeRepoScopedPath`. Kustomize and Ansible rows do not | `appendConfigArtifactRow` and its callers in `repository_config_artifacts_hcl.go` and `repository_config_artifacts_compose.go`, against `extractKustomizeConfigPathRows` and `extractAnsibleConfigPathRows` |
| The anchor list is read with limit+1 and sets a truncated flag | `loadRepositorySemanticOverview` in `go/internal/query/repository/semantics.go` |
| `content_files` batches run one after another inside one Write | `upsertContentFileBatches` in `go/internal/storage/postgres/content_writer_upserts.go` |
| Generation retention deletes `content_files` rows, and only the reducer wires retention | `pruneContentFilesForGenerationsQuery` in `go/internal/storage/postgres/generation_retention_sql.go`, `go/cmd/reducer/generation_retention_wiring.go` |
| Migration 109 uses statement-level INSERT and UPDATE triggers with transition tables and a row-level DELETE trigger | `109_infra_resource_entities.sql` |
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
- The estimate of 2.5-4.5 s for PR1 plus PR2 on ops-qa.
- The content-version trigger meets the six S3 arms (protocol companion
  section 6). The R-1 lost update itself is a source-backed sequence, decided
  by the re-ruling.

NOT_CHECKED: S2 on real R1 path widths, a disk-cold read, ops-prod, the related repositories the R1 body does not name,
the cap census (S6), concurrent stories, and the cold route after a pod
restart.

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
| Paths read, digest key | 2.14 ms | Bitmap Index Scan and Bitmap Heap Scan |
| State read | 0.85 ms | Bitmap scan on the key |
| Reader three-table join | 2.44 ms | Hash join over a seq scan of the 9-page versions table, then Index Only Scan on the paths key |
| Reader join with every key index forced | 2.9-3.2 ms | Nested loop over index scans |
| Dirty join, all 815 repositories | 0.18 ms | Hash join over two seq scans |

Caveats: host load1 was 6-10 during S2, so every time is an upper bound for
that host and is not comparable to ops-qa. The OS cache was warm, also after a
server restart. Path widths come from synthetic md5-derived components, not
from R1's real paths. S2 set the versions directly, with no trigger. The S2
reader condition also excluded repositories with a failed derive.

## 5. Generated-column and trigger-timing checks (local, 2026-10-10)

A throwaway PostgreSQL 18.6 cluster in the scratch directory, started for the
check and deleted after it. No Eshu data. The SQL is in the handoff of this
revision, not in the repository.

| Check | Result |
|---|---|
| `provolatile` of `convert_to(text, name)` and `textsend(text)` | `s` (STABLE) for both. `sha256` and `length(text)` are `i` (IMMUTABLE) |
| `CREATE TABLE` with the generated column of re-ruling 2 | `ERROR: generation expression is not immutable` |
| Same column through an IMMUTABLE SQL wrapper over `convert_to(t, 'UTF8')` | table builds. 4 rows insert, 4 distinct digests, including `('a|b', 'c')` and `('a', 'b|c')`. The digest of `'3:a|b1:ck'` matches a manual `sha256(convert_to(...))` |
| AFTER ROW DELETE trigger timing over 9 rows. BEFORE and AFTER row triggers log a sequence | all 9 BEFORE events (1-9) precede all 9 AFTER events (10-18). AFTER row triggers fire at the end of the statement |
