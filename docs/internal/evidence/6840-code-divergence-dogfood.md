# #6840 — Code divergence dogfood (own repo)

Date: 2026-09-21. Stack: isolated local dogfood project
(`eshu-6840-dogfood`, no sibling stacks touched: sibling `nornic-6838` and
`eshu6834-pg` containers left running on their own ports).

## 1. Provenance (verified)

- Source: clean `git clone --local` of the `eshu-6840-rollup` worktree at
  `b2b2445d0` (HEAD: origin/main incl. merged #6894 NornicDB fix-490 re-pin,
  #6911 for #6839, #6913). Uncommitted #6840 report files are query-side
  only and absent from the index; findings content is unaffected.
- Graph backend: `ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-490-a427a468@sha256:eb69530fa2951d74d89ed9df947aeea10beb0c5e2f2ede4c0080e09fe78aa555`
  (the #6894 pin — the #6839 leaf's numbers predate it, so everything below
  re-proves on the current image).
- Postgres: `postgres:18-alpine` (repo compose pin).
- Profile: `local_authoritative`. One repo indexed:
  `repository:r_4c8a2e84` (`acme/eshu`), 83,591 Function entities, 60,117
  fingerprinted (floor 50).
- Pipeline: `eshu-bootstrap-data-plane` exit 0, `eshu-bootstrap-index`
  pipeline complete in 260.9s, reducer drain terminal at 23 succeeded /
  0 pending / 0 dead_letter (reducer stopped afterwards to free the machine
  for the golden gate).
- Machine: local laptop (darwin/arm64, Docker Desktop); timings are
  same-machine relative only, `absolute_target_applicable=false`.

## 2. Rollup on own repo (verified)

`POST /api/v0/code/divergence/report` (`top_per_kind: 5`), HTTP 200,
`truth.level: derived`, `source_backend: postgres_content_store`:

| Kind | Count | Truncated | Note |
| --- | --- | --- | --- |
| `parallel_implementation.exact` | 212 | true | 766 groups nominated; windowed at the 500-group hydration cap |
| `parallel_implementation.renamed` | 143 | true | 3,236 groups nominated; windowed |
| `parallel_implementation.drifted` | 500 | true | 8,826 materialized pair facts; windowed |
| `parallel_implementation.wrapper_bypass` | 0 | true | shaped families diverted, zero qualified (see §3) |
| `parallel_implementation.convention_outlier` | 0 | true | sweep cut short by the read budget (see §4) |

Total 855. Suppressions: `not_wrapper_family` 1504, `test_file` 4703,
`trivial_accessor` 393, `wrapper_family` 855. All five kind keys present at
zero or above; a quiet kind is distinguishable from a filtered one.

Counts are assembled post-suppression findings over the scanned window, not
raw nominations: the `truncated` flags mark the windowed kinds honestly.
The findings pages remain the complete per-kind source.

## 3. Review verdicts per kind

Reviewed: top 5 exact, top 5 renamed, top 5 drifted by finding payload, plus
a source read behind one finding per content kind and behind the wrapper
zero. Reviewed 15 findings, confirmed 14, rejected 0, consolidated 0.

- **exact — CONFIRMED.** Top: `cloneStringMap` ×124 across
  `collector/awscloud/services/*` (score 8928) — the genuine per-service
  clone class #6834 hand-labelled; `cloneStrings`/`cleanStrings` ×89;
  `viewFrom*` ×13 same-file siblings in `reducer/obscoverage`;
  `BuildEntitySemanticProfile` ×2 (`query/repository/semantic_profile.go:12`
  vs `query/entitysemantics/entity_semantic_profile.go:83`, score 3088,
  read in full: byte-identical bodies, and the copy itself documents the
  duplication as tracked on #6060); `tagMap`/`mapTags` ×34 across SDK
  mappers.
- **renamed — CONFIRMED.** Same families plus AWS SDK wrappers
  (`ListCisScanConfigurations`/`listSubnetGroups`/`describeJobs`/
  `ListCrawlers` ×28, score 3808): identical structure up to renaming, the
  documented renamed signal.
- **drifted — CONFIRMED.** Top: reducer `Handle` pairs across provider
  packages (azure vs gcp `Handle`, Jaccard 0.86, score 1718; rds/s3/iam/s3
  variants at 0.66). Read both `Handle` bodies in full: same handler
  skeleton with provider-stamped strings, spans, and fact kinds — the exact
  drift hazard the epic targets (a fix landing in one provider's copy and
  not the other's). No provider-adapter suppression rule shipped with the
  children (the catalogue covers generated/vendored/test/accessor plus
  floor and threshold), so these report loudly rather than suppressing.
- **wrapper_bypass — TRUE NEGATIVE (confirmed zero).** The shaped families
  (`wrapper_family` 855 member-rows, incl. the ×130 `recordAPICall` class)
  diverted to graph tracks that qualified nothing. Source read:
  `recordAPICall` is a per-service telemetry wrapper
  (`apprunner/awssdk/client.go:351`) whose `call` argument invokes the AWS
  SDK — external to the repo graph, so no in-repo target exists to bypass.
  Zero is the honest answer here, not a recall gap.
- **convention_outlier — UNAVAILABLE (defect, see §4).** Count 0 with
  `outlier_graph_timeout: 1`, kind truncated.
- **Rejected: 0.** No false positives among the reviewed findings; test
  scaffolds suppress correctly at volume (`test_file` 4703).
- **Consolidated: 0.** `BuildEntitySemanticProfile` is the one actionable
  consolidation and its own comment defers it to #6060 (another lane's
  package); provider `Handle` parallels need provider-lane design calls.
  Both recorded here instead of drive-by refactored.

## 4. Defect: outlier sweep exceeds the graph-read budget at repo scale

The first rollup call on this repo returned HTTP 500
(`graph query exceeded its deadline`, 10.6s): the outlier track ran the
full cohort sweep inside one report call. Per-kind isolation proved it:
`exact`/`drifted`/`wrapper_bypass` pages answer 200 in 0.1–0.3s;
`kind=convention_outlier` answers 500 at exactly 10.0s.

Direct measurement on the dogfood backend (fix-490 image, 83,591 Function
nodes, `function_repo_id` index ONLINE):

| Read | Time |
| --- | --- |
| Package seed enumeration (`MATCH (member:Function) WHERE coalesce(member.repo_id,'')=…` + CONTAINS expand, 83,591 rows) | 9.6s |
| Same with plain `member.repo_id = …` equality | 6.7s |
| Seek alone, no expand | 3.3s |
| Map-literal anchor + expand | 6.9s |

The sweep needs three such enumerations plus the callee fan-out, so it
cannot fit the 10-second logical-read budget on this corpus however the
predicate is spelled: the index exists but the plan still scans, and the
CONTAINS expansion over 83k members costs ~3.5s on its own. #6839's
sub-millisecond numbers were seeded-scale medians; they do not transfer to
own-repo scale, and the `local_full_stack` p95 1000ms matrix cell does not
hold here.

Fix in this issue (report surface only): a graph track the budget cuts
short now degrades to a counted `*_graph_timeout` suppression with its kind
truncated (`TestCodeHandlerDivergenceReportDegradesSlowGraphTracks`), so one
slow family cannot veto the other four. The findings page keeps the loud
500 so the slow sweep stays visible as a defect. The sweep itself needs a
follow-up: bounded enumeration (index-backed seek or DB-side cohort-size
filtering) re-proven at repo scale — proposed as the #6840 PR follow-up,
not smuggled into the rollup.

## 5. Corpus leg status

- Golden-corpus gate (fixture corpus + B-12 snapshot, the local corpus
  vehicle): PASS twice on this branch. First run 570 pass / 0
  required-fail / 1 advisory-warn (187s); second run after the
  `report_code_divergence` B-12 shape landed 571 pass / 0 required-fail /
  1 advisory-warn (172s), including
  `mcp:report_code_divergence: fields [repo_id counts total top top_per_kind truncated suppressions source_backend] present; values [repo_id]` —
  the new tool asserted live through the MCP layer on the fixture corpus.
- 20-repo real corpus + full-corpus NornicDB PROFILE on the fix-490 image:
  pending remote validation — `docs/internal/remote-validation/code-divergence.md`.
  The remote host was unreachable from this machine (SSH timeout, §6 of the
  PR handoff); the matrix production cells stay `unsupported` until that
  artifact exists.
