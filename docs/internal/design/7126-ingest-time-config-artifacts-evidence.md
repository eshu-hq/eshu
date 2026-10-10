# Evidence for the config-artifact derive decision (#7126)

This appendix belongs to
[Derive repository config artifacts when content is written](7126-ingest-time-config-artifacts.md).
It holds the measurements the decision rests on, the source facts, and the
split between evidence and theory. No new measurement was taken for this
record. Every figure below comes from an earlier read-only probe or a local
bench. Source lines are at `origin/main` `b7ca5f2f0a`.

## 1. Measurements and their limits

All ops-qa figures are read-only probes of 2026-10-10 on one repository story
(R1) with a second repository (R2) as a contrast.

| Measurement | Value | Source | Limit |
|---|---|---|---|
| `deployment_artifacts` stage, 4 R1 calls | 6.953, 6.750, 6.627, 6.643 s | [comment 6097810983](https://github.com/eshu-hq/eshu/issues/7126#issuecomment-6097810983) | one repository, one image. Call 1 is first-in-session, not a guaranteed cold read |
| Route time, same calls | 7.969, 7.037, 6.919, 6.933 s | same | client wall times are load-affected. Server figures are not |
| Stages outside `deployment_artifacts` | 1.016 s on call 1, about 0.29 s on calls 4-6 | same, stage log lines | `target_documentation` 0.498 s cold, 0.025-0.026 s warm |
| Reader statements per story | R1 8,255-8,258. R2 34 | same, `eshu_dp_postgres_reader_member_attempts_total` delta | the list/get split is extrapolated from a truncated trace |
| Neo4j reads per story | 0.143 s call 1, 0.010 s calls 4-6 | same | at most 2% of the stage |
| Related repositories N | at least 141 (distinct `source_repo`). About 300 by extrapolation | same | N was not counted directly |
| Database execution | about 0.03 ms per file, single statement | [comment 6098312440](https://github.com/eshu-hq/eshu/issues/7126#issuecomment-6098312440) | host load1 17-32 on the client side. Figures labeled non-comparable |
| API CPU per R1 story | 6.70-6.88 CPU-s. CFS throttled 0.40-0.59 s | [comment 6098498452](https://github.com/eshu-hq/eshu/issues/7126#issuecomment-6098498452) | no pprof on the pod. Split between per-statement cost and parse is an inference |
| Per statement work | 6 spans, 5 histogram records, 1 counter add, 3 round trips | same, source read | about 49,600 spans per R1 story |
| Hydrated content mix | 4,632 files, 52.2 MB. JSON 69.4% of bytes, never parsed unless a kustomization references it (0 here). HCL 2,498 files, 12.37 MB | [comment 6098969523](https://github.com/eshu-hq/eshu/issues/7126#issuecomment-6098969523) | the about 160 repositories the body does not name are NOT_CHECKED |
| Local CPU bench, 300 repositories, 8,000 synthetic files | shipped 3.72 CPU-s median. Parse skipped 0.91. Batched 2.99. Output identical in all arms | same | anchor failed: 0.54 of the pod figure. Only arm comparisons hold |
| HCL literal gates | 1,250 of 2,498 HCL files pass no scan gate. Projected regex work 18-19% of ungated | [comment 6100249949](https://github.com/eshu-hq/eshu/issues/7126#issuecomment-6100249949) | a projection from laptop rates. No gated parse ran on real content |

The PR1 and PR1b figures in the decision record come from their review
verdicts: PR1 round 2 CLEAN at `fe1c35698f`, PR1b round 3 CLEAN at
`5d4cda0767`. Neither branch is merged on 2026-10-10.

## 2. Source facts behind the design

| Fact | Location |
|---|---|
| Related repositories come from one Neo4j UNION over eight relationship types, then a grant filter, before any file is read | `go/internal/query/repositoryartifacts/repository_config_artifacts_loader.go:108-142` |
| Related repositories are visited one at a time | same file, `for _, source := range sources` at line 177 |
| A related repository lists its files with `querycontract.RepositorySemanticEntityLimit` (5,000), no sentinel | same file, line 185 |
| The hydration set is the first 50 candidates in list order that carry no content, read 8 at a time | `repository_artifact_hydration.go:18-45` |
| Blank content is dropped before the build | `repository_config_artifacts_loader.go:196-205` |
| Rows are deduplicated on `path|source_repo|relative_path|evidence_kind` and sorted on `path`, `source_repo`, `relative_path` with `sort.Slice` | same file, lines 207-255 |
| Each row has four fields. `source_repo` is the name passed in | `repository_config_artifacts.go:64-80`. Kustomize rows at `repository_config_artifacts_kustomize.go:51-56`. Ansible rows at `repository_config_artifacts_ansible.go:28-33` |
| The extractors drop remote references and do not read them | `repository_config_artifacts_kustomize.go:130`, `repository_config_artifacts_compose.go:153`, `repository_config_artifacts_hcl.go:151` and `:405` |
| The anchor repository passes its own file list. Compose and GitHub Actions files are read first, and a file with content is not counted against the 50 cap | `repository_deployment_artifacts_loader.go:24-46`, `repository_artifact_hydration.go:41` |
| Three routes call the loader | `go/internal/query/repository/handler.go:346`, `repository/context.go:211`, `service/deployment_evidence.go:47` |
| `content_files` is keyed on `(repo_id, relative_path)` and is latest-wins | `go/internal/storage/postgres/schema_content_store.go:15-28`. Migration `169_content_files_generation_id.sql` |
| The content writer already derives a read model after its content statements | `go/internal/storage/postgres/content_writer_upserts.go`, `deriveInfraInventory` |
| `ingester`, `projector` and `bootstrap-index` construct the content writer | `go/cmd/ingester/wiring.go:415`, `go/cmd/projector/runtime_wiring.go:168`, `go/cmd/bootstrap-index/wiring.go:139` |
| The infra read model backfill runs in the API process today | `go/internal/query/cloud_resource_owner_backfill.go:248` |
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

Theory (each has a shim in section 9 of the decision record):

- The derive adds an acceptable cost to each content Write (S1).
- The stored-row read is one primary-key statement under 5 ms (S2).
- The fence trigger adds under 5% to a content upsert (S3).
- Stored rows equal live rows for every repository (S4).
- The projector repository name equals the graph `Repository.name` (S5).
- The stage drops to 0.25 s or less, the route to 1.0 s or less warm, and API
  CPU to 0.7 CPU-s or less per R1 story. These targets are a budget built from
  the contributor list, not a measurement.
- The arbiter estimate of 2.5-4.5 s for PR1 plus PR2 on ops-qa.

NOT_CHECKED: ops-prod, the related repositories the R1 body does not name,
the cap census (S6), concurrent stories, and the cold route after a pod
restart.
