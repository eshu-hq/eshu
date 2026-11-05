# parity — workflow differential conformance harness

Owner: workflow extraction drive ([#8875](https://github.com/eshu-hq/eshu/issues/8875)).
Contract: `doc.go` (package godoc is authoritative).

## What this is

One scenario suite, parameterized by `Backend`, run against the in-tree
`ControlStore` on an isolated Postgres schema and against the satellite
claim plane on its own isolated database through the in-tree
`ClaimPlaneClient`. Identical seeded fixtures, deterministic IDs, one
fixed clock. The suite compares return values, error codes, and
read-plane-observable state.

## Dependencies

- In-tree leg: `storage/postgres` (`WorkflowControlStore`) over a
  disposable database that `postgresproof.OpenDisposableBootstrapDatabase`
  creates (from template0) on the Postgres server named by
  `PARITY_INTREE_DSN`, then force-drops at test end. The DSN must name the
  `postgres` maintenance database, and `PARITY_INTREE_DISPOSABLE=1` is the
  required opt-in. The full production bootstrap runs in that database's
  `public` schema because the claim guard SQL joins
  `scanner_image_scan_results`, which only the public-only migrations create;
  an isolated schema skips them and complete/heartbeat fail with
  `relation "scanner_image_scan_results" does not exist` (SQLSTATE 42P01).
  Point it only at a disposable server; nothing else on it is touched.
- Satellite leg: any workflow.v1 server at `PARITY_SATELLITE_URL`
  (local flip container or dark deployment) with direct DB access at
  `PARITY_SATELLITE_DSN` for truncation reset.
- No satellite module import: the HTTP leg speaks the protocol through
  the in-tree client only. No shared tables, ever.
- `NewInTreeBackend` and `NewSatelliteBackend` wrap a caller-built store or
  client as a `Backend`. The contention harness (`contention/`, #9727) uses
  them to set the quota lookup, lease defaults, and HTTP client itself.

## Operating the suite

```sh
PARITY_INTREE_DSN='postgres://USER:PASS@127.0.0.1:PORT/postgres?sslmode=disable' \
PARITY_INTREE_DISPOSABLE=1 \
PARITY_SATELLITE_URL=http://127.0.0.1:18181 \
PARITY_SATELLITE_DSN='postgres://USER:PASS@127.0.0.1:PORT/parity_satellite?sslmode=disable' \
  go test ./internal/workflow/parity/ -count=1
```

Start the satellite with a long reaper interval (for `claim-plane`,
`--reap-interval=1h`). The scenarios reap explicitly at synthetic fixture
times; a background reaper tick, on the wall clock, that lands mid-scenario
reaps a live claim and fails the satellite leg (seen once on
`TestParityStaleWriterCommitFence` with the 20 s default: writer 2's
`CompleteClaim` came back `epoch superseded (claim expired)`).

With the legs configured, e.g. a local flip container on :18181 and one
database for the satellite leg (the harness creates and drops the in-tree
leg's database itself). Without the environment the suite skips. Never point
the satellite leg at an authoritative deployment, and never replay live
claims: synthetic run and scope IDs plus cleanup only.

## Telemetry

None: this is a test-only package with deterministic fixtures. Scenario
mismatches print both outcome records with the underlying error texts.
Throughput numbers log per leg for the dark-deployment no-regression baseline.

## Live re-bind record (2026-11-05, drive/8875-parity-rebind-c5)

- Bound image: `ghcr.io/eshu-hq/eshu-workflow@sha256:e1b564afa5f72167e3911aea7c881973c74bab123183c03d9d6af5a2d80e47a4`
  (tag `v0.1.1`), the exact digest the live dark deployment serves (read
  back from the deployment spec the same day). Local leg ran that digest as a
  container (`--platform linux/amd64`, `--reap-interval=1h`) against a
  disposable Postgres; the in-tree leg ran on a second disposable Postgres.
- Result: full `go test ./internal/workflow/parity/ -count=1` green on
  three two-legged runs, no skips: twice at 10/10 on the pre-change tree
  (8-scenario conformance slice, pin gap, pin-survives-reaper,
  duplicate-admission gap, stale-writer fence, throughput), and once at
  11/11 on the final tree including the new read/report live compat
  (`TestReadReportPlaneLiveCompat`, all five routes round-tripped).
- Residue: satellite tables 0/0/0/0/0 (claims/items/runs/progress/
  completeness) after the run. The first run left one
  `eshu_content_index_proof_*` disposable database behind despite its green
  verdict (dropped by hand, no active backends at drop time); the repeat
  run disposed every database, so the helper's cleanup is best-effort and
  the repeat run is the standing check before any dark-leg residue claim.
- NOT_CHECKED: the dark-deployment leg (synthetic writes to the shared
  satellite database stay with the coordinator under the standing grant
  preconditions). No shared-infra writes were made from this lane.
- No-Regression Evidence: baseline full parity suite green twice at 10/10
  pre-change (8.338s, 8.720s wall); after final tree green 11/11 (8.356s
  wall) on the same legs. Backend: claim-plane image
  sha256:e1b564afa5f72167e3911aea7c881973c74bab123183c03d9d6af5a2d80e47a4
  (linux/amd64 emulated on arm64, `--reap-interval=1h`), Postgres 18.6 on
  disposable servers, Go 1.26.6. Input shape: identical seeded fixtures
  with deterministic run/item IDs and one fixed clock per scenario, plus
  stub-server round trips for the client unit tests. Terminal counts:
  satellite claims/items/runs/progress/completeness 0/0/0/0/0 and no
  leftover disposable databases on the repeat run. Telemetry: suite verdict
  lines only. Why safe: the five routes are new methods no existing caller
  uses; the `mutate()` early-reject and the store validation fire only on
  values outside `[a-z0-9_]{1,64}`, which the legitimate path never emits
  (producers sanitize) — the full workflow, parity, and focused storage
  suites stay green.
- No-Observability-Change: no new spans, metrics, logs, or dashboards; a
  rejected mutation returns before `post()`, so it emits no Observe event
  (no request, no outcome), consistent with the exactly-one-outcome
  contract.

## Known divergences

- None on the claim path: an identical replay of an already-applied
  terminal mutation (complete, release, fail-retryable, fail-terminal)
  acks on both legs, a heartbeat after the complete stays rejected on
  both legs, and a replay after a reclaim stays rejected on both legs
  ([#9428](https://github.com/eshu-hq/eshu/issues/9428), strict
  `Outcome.Equal` since the replay-ack alignment).
- Commit pin: in-tree has none (`TestParityPinGap`); pin survival is
  proven on the satellite leg only.
- Stale-writer commit fence (#9376): not a divergence. Both legs refuse
  a reaped writer's core commit after the item is reclaimed at the next
  token (`TestParityStaleWriterCommitFence`): the in-tree leg through the
  core claim heartbeat on the fact transaction, the satellite leg through
  the claim-plane pin before any core transaction
  (`IngestionStore.ClaimPlane`, `storage/postgres/claim/fence`).
- Guarded admission: the in-tree store refuses a second admission on an
  already-covered target tuple (`TestDuplicateAdmissionGap`); the satellite
  has no guarded route (`ErrGuardedAdmissionNotSupported`) and admits
  through separate `CreateRun`/`EnqueueWorkItems` calls. The rehearsal must
  cover the duplicate-admission scenario on the flipped scope
  ([#8875](https://github.com/eshu-hq/eshu/issues/8875)).

No-Regression Evidence: this change adds a harness-only `Backend` method and
a test; production guard SQL, wire shapes, queue depths, and row counts are
untouched. Baseline and after: full `go test ./internal/workflow/parity/`
two-legged green before and after (5.685s on 77c751be0c) with
identical scenario sets plus the new gap test; backend Go 1.26 linux/arm64
against local Postgres 18 and the QA claim-plane. Terminal counts: 0 parity
rows remain satellite-side, no local schema remains.

No-Observability-Change: no new spans, metrics, logs, or dashboards; the
test emits only the standard `go test` verdict lines.

Performance Evidence: the in-tree leg now bootstraps a full database per
test instead of an isolated schema (change in `openInTreeBackend`). Baseline:
the suite with the public schema bootstrapped by hand, 8.6 s and 9.1 s wall
time across two runs, Postgres 18.6, the pinned `eshu-workflow` image.
After: 6.3 s (and 9.8 s under `-race`) on the same shape, 7 top-level tests
including the 30-claim throughput probe. Terminal counts: 0 rows in the
satellite tables, no leftover `parity_*` schema, and the per-test databases
dropped (`pg_database` lists only `postgres`, `template0`, `template1`).

No-Regression Evidence: pass set unchanged apart from the 12 tests that
failed on a fresh database before this change; production code, the claim
guard SQL and `postgresproof` are untouched, and the tests still skip when
`PARITY_INTREE_DSN` is unset.

Observability Evidence: none needed. This is a test-only package; a failing
run prints both outcome records with the underlying error texts, and the
opt-in or admin-DSN guards fail with the variable name.
