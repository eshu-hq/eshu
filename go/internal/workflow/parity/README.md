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

## Operating the suite

```sh
PARITY_INTREE_DSN='postgres://USER:PASS@127.0.0.1:PORT/postgres?sslmode=disable' \
PARITY_INTREE_DISPOSABLE=1 \
PARITY_SATELLITE_URL=http://127.0.0.1:18181 \
PARITY_SATELLITE_DSN='postgres://USER:PASS@127.0.0.1:PORT/parity_satellite?sslmode=disable' \
  go test ./internal/workflow/parity/ -count=1
```

With the legs configured, e.g. a local flip container on :18181 and one
database for the satellite leg (the harness creates and drops the in-tree
leg's database itself). Without the environment the suite skips. Never point
the satellite leg at an authoritative deployment, and never replay live
claims: synthetic run and scope IDs plus cleanup only.

## Telemetry

None: this is a test-only package with deterministic fixtures. Scenario
mismatches print both outcome records with the underlying error texts.
Throughput numbers log per leg for the dark-deployment no-regression baseline.

## Known divergences

- None on the claim path: an identical replay of an already-applied
  terminal mutation (complete, release, fail-retryable, fail-terminal)
  acks on both legs, a heartbeat after the complete stays rejected on
  both legs, and a replay after a reclaim stays rejected on both legs
  ([#9428](https://github.com/eshu-hq/eshu/issues/9428), strict
  `Outcome.Equal` since the replay-ack alignment).
- Commit pin: in-tree has none (`TestParityPinGap`); pin survival is
  proven on the satellite leg only.
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
