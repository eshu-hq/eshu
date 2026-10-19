# Write-time derive wiring (#8155, PR3b-2c-2)

This note records the A2 theory proof and the S1 measurement that admit the
2c-2 wiring slice, and the wiring proof itself. Parent records:
[the decision](7126-ingest-time-config-artifacts.md) (section 8 chain),
[the protocol companion](7126-ingest-time-config-artifacts-protocol.md)
(section 3 derive protocol),
[the derive core](8155-derive-core.md) (PR3b-2b),
[the version fence](8155-content-version-fence.md) (PR3b-1b).
Refs #8155. Arbiter verdict 2026-10-19: APPROVED-WITH-AMENDMENTS A1-A4.

## What landed

- `ContentWriter.Write` derives the written repository as its last step,
  after the infra read-model derive succeeds
  (`go/internal/storage/postgres/content_writer_config_artifacts.go`: the
  `ConfigArtifactDeriver` port, `WithConfigArtifactDeriver`, `WithTracer`,
  the step, and the `NewConfigArtifactWriteDeriver` startup helper).
- The ingester, the projector and bootstrap-index build the deriver once at
  start (encoding + fence checks, `ExtractorVersion` 1, timeout from
  `ESHU_CONFIG_ARTIFACT_DERIVE_TIMEOUT`, kill switch from
  `ESHU_CONFIG_ARTIFACT_DERIVE_ENABLED`) and attach it to their
  `ContentWriter`.
- `OutcomeCanceled` in `config/artifacts/status.go`: a canceled caller
  records outcome `canceled`, kept apart from `error` (the telemetry set
  already registered it; a parity test pins both sets).
- Timeout text (10 s deadline, 15 s caller-facing total) in the decision
  record, the protocol companion, the `DeriveRepository` godoc and the
  env-registry entry. The default stays 10 s.

## A2: per-stage attribution of the ~2.4 s residual (theory proof)

Method (2026-10-19, QA image sha-3d7cb9c6, pre-#8556 fifty-path chunks):
7 serial `GET /api/v0/repositories/repository:r_e34d5f85/story` (R1)
through a port-forward, 2 s apart; curl wall time,
`Server-Timing`, body sha; then the `repository_query.stage_completed` log
lines of the api pod for the window. Bodies byte-identical 7/7
(sha256 d0a531ea, 3,781,847 B each). `Server-Timing` carries total only,
as the verdict notes.

| Call | Route (curl) | Server-Timing total |
|---|---|---|
| 1..7 | 2.35–2.60 s | 2314.8–2554.2 ms |

Stage walls from the logs (8 lines: 7 series + 1 probe):

| Stage | Wall per call |
|---|---|
| `deployment_artifacts` | 2.04–2.29 s (83–91% of route) |
| `deployment_evidence` | 0.155–0.166 s |
| `target_support` | 0.042–0.045 s |
| `target_documentation` | 0.014–0.027 s |
| `repository_lookup`, `content_coverage`, `graph_summary`, `semantic_overview`, `infrastructure`, `narrative_files`, `relationships`, `ci_cd_evidence` | each at most 0.011 s; graph stages (`graph_summary` 0.000 s, `relationships` ≤0.003 s, `repository_lookup` ≤0.006 s) total ≈0.01 s |

Inside `deployment_artifacts` (worker-sums over 8 in flight, identical all 8
calls: 1,308 statements, 320 related repositories, 5,606 files hydrated,
21.7 MB, 15,999 candidates, `batch_chunks_in_flight_max` 8):

| Split | Worker-sum |
|---|---|
| `hydrate_seconds` | 9.6–10.9 s |
| `list_seconds` | 3.7–4.4 s |
| `parse_seconds` | 2.5–3.2 s |

Verdict: PROVEN. The residual is the `deployment_artifacts` stage: pg
list+hydrate worker time (~14 s over 8 lanes ≈ 1.3 s wall floor) plus handler
parse CPU (~2.9 s), against graph ≈0.4% of the route. No read-side trim or
fan-out change can go below that floor; write-time derivation removes the
whole stage body (list, hydrate, parse) and replaces it with one indexed
statement. Outside the derive's reach: `deployment_evidence` 0.16 s and
~0.1 s of framework/stages, consistent with the section 6 route budget once
the stored read lands. `config_paths` counted 3,655 rows here (2,986 Oct-11
anchor, 3,701 Oct-15): drift continues; re-baseline in `live` mode before the
PR3c acceptance reads (A3).

## S1: derive cost on the built path (merge gate)

Harness: throwaway live test driving the real `ContentWriter.Write` with and
without the wired derive (kept at `~/.cache/coord8155/s1/`, deleted before
the PR), interleaved A/B over fresh repositories. Backend `postgres:18` in
Docker, local, shared host load1 2.5–6.4, 2026-10-19. Shapes: the golden
fixture corpus (194 repositories, 2,130 files, 1.74 MB, same walk as S4) and
one synthetic monster (5,000 files, 4.00 MB in, 5,000 candidates listed and
read, 5,000 rows stored, plus 10,000 entities so the Write wall is
production-width per S3 arm 1).

| Shape | Added Write wall (wired minus plain) |
|---|---|
| Golden corpus, 193 interleaved pairs | median +0.8 to +1.1 ms, p95 +1.8 to +2.1 ms (four runs) |
| Monster, 3 alternating rounds | median +0.211 s (+0.211, +0.324, −1.038 when the plain arm spiked to 3.04 s on host noise); files-only reference median +0.127 s |

Bars: worst-repository 2 s holds with 6x margin (0.21–0.32 s); parse runs
53–81 ms per MB of input against the bench 94 ms per MB. The 10% of
content-stage share reads about 11% on the monster (0.21 s of a 2.0 s
files-plus-entities Write, ±5 pp over 3 noisy rounds); the monster stores
5,000 rows in one repository, ~250x the R1 average row count, so typical
repositories pass with two orders of margin. The content-stage p95
denominator for the ratio bar is a deployment number; the Write-wall proxy
used here is the conservative reading (the full stage with canonical
projection and graph writes is larger).

## Wiring proof

- Hermetic (`go/internal/storage/postgres`, no database): the derive runs
  once per Write with repo/scope/generation/trigger `write` after a
  successful infra derive; no deriver means no derive and no series; a failed
  derive still returns a nil Write error and counts `write/error`; a failed
  infra derive fails the Write with no derive call; one
  `config_artifacts.derive` span per Write carries repo/trigger/outcome;
  the helper returns nil without a statement when disabled, and errors on a
  bad timeout and on a pool without transactions.
- Live (`TestContentWriterWriteDerivesConfigArtifactsLive`, disposable
  Postgres): helper-built deriver over the bootstrapped schema, Write of 2
  files, `write/ok` counted once, state row `extractor_version = 1`,
  `content_version` = the fence-bumped version.
- Trigger/outcome parity (`telemetry` package): the registered sets equal
  the storage constants, `canceled` included.
- Existing suites: `cmd/ingester`, `cmd/projector`, `cmd/bootstrap-index`
  wiring tests now run their builders against failing-query doubles
  (ingester) or a refusing-connector `SQLDB` (projector), proving the
  builders survive failed startup checks; bootstrap-index already used an
  answering fake and passes unchanged.

## Telemetry emitted (first emission)

`eshu_dp_config_artifact_derives_total{trigger=write,outcome}`,
`eshu_dp_config_artifact_derive_duration_seconds{trigger=write}`, the
`config_artifacts.derive` span, the `derive_config_artifacts` stage log, and
`config_artifacts.derive.disabled` at start when disabled.

## NOT_CHECKED and follow-ups

- The walker derive, PR3c shadow/stored, and the section 6 acceptance reads
  (A3/A4 chain, later slices).
- The QA content-stage p95 denominator behind the S1 10% bar; the ratio is
  reported against the Write wall.
- `deployment_evidence` 0.16 s and the cold-route stages outside PR3
  (decision record section 6: the cold route stays above 1 s unless those
  change).
- A panic inside the neutral package's chunk-read goroutines (documented in
  the derive core note, unchanged).
