# Stored cutover (#8155, PR3c stored slice)

This note records the production shadow wiring and the stored cutover: the
API serves trusted stored rows for related repositories behind
`ESHU_CONFIG_ARTIFACT_READ_MODE`, with a live fallback per repository. It
completes the PR3c row of
[the decision record](../design/7126-ingest-time-config-artifacts.md),
section 5, except the `/admin/status` field, which stays open. Rollback stays
a flag flip to `live` with an API roll.

Pre-cutover R1 baseline (on #8155, 2026-10-21): route p95 ~2.26 s,
body sha `b3a69151`, 3,783,825 B. Acceptance is the record's section 6 table:
same R1 with route p95 under 1 s and the identical-row proof.

## What this slice builds

- Leaf stored load (`go/internal/query/repositoryartifacts/stored.go`): the
  anchor loads live; trusted related repositories serve stored rows through
  `namedRowsToMaps` into the single `MergeConfigPaths` merge and dedup; every
  other related repository falls back live with its reason counted. One stored
  statement answers the whole related set; a failed read serves everything
  live (`stored_error`). Stored repositories spend no request budget and never
  emit the budget reason; their 5,000-window reason comes from the state's
  `list_truncated` (new on the stored read, trusted-only).
- Read-mode wiring (`go/cmd/api/wiring_config_artifacts.go`): parsed once at
  API start; unknown modes and shadow/stored with no Postgres reader fail
  startup. The adapter maps the storage port 1:1 at
  `contentartifacts.ExtractorVersion`.
- Emission: `eshu_dp_config_artifact_reads_total{source,reason}` on every
  story, context and service deployment-artifact load;
  `eshu_dp_config_artifact_shadow_compare_total{outcome}` plus the
  `config_artifacts.shadow.mismatch` (WARN) and
  `config_artifacts.shadow.read_error` (WARN) logs from the production
  reporter. The stage lines carry `read_mode`,
  `stored_repository_count`, `live_repository_count`, `live_reasons`,
  `stored_row_count` and `stored_statements`.

## Proof

- Identical rows: `TestStoredBodyEqualsLiveBody` (leaf, stored mirror of the
  live rows over two related repositories) and
  `TestStoryServesStoredRowsIdentically` (route, full story bodies equal).
- Fallback per reason: `TestStoredFallbackServesLivePerReason` (leaf) and
  `TestStoryStoredFallbackPerReason` (route: missing, stale_version,
  stale_extractor, failed, stored_error), bodies equal to live in every case.
- Access: `TestStoredReadTakesOnlyGrantFilteredIDs` (the stored read
  receives only grant-filtered ids; #5167 W3 P0).
- Merge parity: `TestStoredCollapsesSharedGraphNames` (two related
  repositories sharing a graph name; the anchor repeated in the related
  list), `TestNamedRowsToMapsMergeParity` (converter cannot skew the merge).
- Converter placement: the neutral package's name-scope source guard
  requires string-literal kinds in every row literal, so the generic
  `namedRowsToMaps` lives in the query leaf beside its inverse
  `namedLiveRows`; the identical-body tests pin the key set end to end.
- Wiring verticals (no live Postgres): `TestConfigureStoredModeServesStoredRows`
  (stored mode answers from the stored statement; related content unread) and
  `TestConfigureShadowModeCountsTheCompare` (shadow counts stored_missing
  while serving live).
- Query-plan bar: the stored statement keeps the PR3c predicates
  (primary-key equality over the access-filtered id set, one snapshot); no
  new table scan, so no index change. The `query-methodology` gate re-pins
  only the two handler digests (metric emission lines; callsite counts
  unchanged).

## Current Evidence

No-Regression Evidence: this slice changes no production read behavior —
the read mode defaults to live, so the deployed route serves byte-identical
live rows until an operator flips ESHU_CONFIG_ARTIFACT_READ_MODE; unknown
modes fail API startup. The stored path is proven identical by mirror tests
(TestStoredBodyEqualsLiveBody, TestStoryServesStoredRowsIdentically) and by
per-reason fallback tests whose bodies equal live in every case; the stored
statement keeps the PR3c predicates with no new table scan. Baseline: R1
pre-cutover route p95 ~2.26 s on QA (image sha-6980c08e, Neo4j 1,056,791
nodes, body sha b3a69151, 3,783,825 B). After-measurement (route p95 under
1 s on the same R1) is pending a QA deploy with stored mode and will be
recorded here before stored serves production traffic.

Observability Evidence: every deployment-artifact load emits
eshu_dp_config_artifact_reads_total{source,reason} (stored vs live
partition with fallback reasons); shadow mode emits
eshu_dp_config_artifact_shadow_compare_total{outcome} plus WARN logs on
mismatch (config_artifacts.shadow.mismatch, repository ids) and read error
(config_artifacts.shadow.read_error). Stage lines carry read_mode,
stored/live counts, live_reasons, stored_row_count and stored_statements.

## Still open (not this slice)

- The `/admin/status` `config_artifacts` field (record section 3.5).
- The section 6 acceptance reads on QA (route p95, CPU, statements,
  R1+R2 multisets, marker/dirty-join checks) with the deployed image.
- The offline differential of every repository (`0/0`).
