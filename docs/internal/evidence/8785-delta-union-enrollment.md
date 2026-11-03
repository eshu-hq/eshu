# Delta union scheduled enrollment (#8785)

The existing `go/cmd/projector/delta_union_live_test.go` contains two exact
proofs. The workflow is configured for daily, manual, and scoped PR runs on
isolated PostgreSQL 18.6 and Neo4j services. Its PostgreSQL database is
disposable: the shared overlay harness drops the public schema. Its Neo4j
graph is likewise owned by the job, because the harness clears it. The job uses pinned service
images, an anchored Go selector, `-race -count=1`, a twelve minute test timeout,
`pipefail`, strict Go JSON event accounting, and uploaded events. Its artifact
also records the checkout SHA, Go version, PostgreSQL and Neo4j versions,
service image IDs, and wall seconds after a successful proof. The workflow
sets nonempty Neo4j username and password values even with `NEO4J_AUTH: none`.

The static mirror compares the actual source functions with the selector and
rejects disabled jobs or steps, suppressed failures, changed service pins,
missing routes, and missing event verification. The ledger pins this file alone
to `delta-union-scheduled` and `backends: neo4j`. The five tests in
`superseded_delta_overlay_live_test.go` remain scheduled.

The PR route includes `go/cmd/projector/**`, `go/internal/**`, `go/pkg/**`,
and the locally replaced `sdk/go/collector/**` and `sdk/go/factschema/**`
modules. The projector package compiles many shared internal dependencies even
when the selector runs only two roots. The broader route can run a 20-minute
backend job for an unrelated internal change; that cost is accepted to keep the
PR proof sensitive to changes in its compiled dependencies. The static mirror
checks the workflow route and registry triggers against actual package imports.

## Pinned local proof before the latest rebase

The isolated local run at `4c1e5dc43614dbc7f6e561427785873b1a368472`
used the pinned PostgreSQL 18.6 Bookworm image
`ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c`
and Neo4j image
`neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`.
Their local image IDs were respectively
`sha256:2e182ed2ff8f5d0835353864264437a26dffc542a9a6c677ec495aa54e491627`
and `sha256:2011a203e1a75bf71d51093b577dc1242bb56b94ddfee05d22f2486465a4fbe6`.
The services reported PostgreSQL `18.6 (Debian 18.6-1.pgdg12+2)` and Neo4j
Kernel `2026.08.1`. The effective Go version inside the module was
`go1.26.10 linux/amd64`; the launcher outside the module was
`go1.26.2 linux/amd64`.

The run used:

```bash
go test ./cmd/projector \
  -run '^(TestDeltaUnionProjectsFullTreeFromEmptyLive|TestDeltaUnionBaseMissingRefusesWithoutPartialLive)$' \
  -json -race -count=1 -p 4 -timeout 8m
```

Each root had exactly one run and one pass, with no skip, fail, or unexpected
test. The Go command and strict event verifier both exited 0. The JSON event stream SHA-256 is
`1e52018dc0cf4691f2f85867f14132d577d3a69c4c3846a8df073df83ea8c756`.
The package reported 9.626 seconds; the Go process took 83.219247568 seconds
including compilation, and the whole local proof took 96.590880451 seconds.
These are different timing boundaries, and no speed comparison is claimed.
Both owned service containers were removed with exit 0.

The receipt and event stream are at
`/tmp/eshu-7533-drive/8785-current-local/receipt.json` and `events.jsonl` on
the proof host. This run proves the two Go roots against the runtime at the
tested commit and pinned local services. A later rebase onto
`34922d077381bbef2b44cb70f518c12bd3f8b24a` added upstream changes;
the fresh combined-tree replay below supersedes that local-proof gap. The older
`/tmp/eshu-7533-drive/8785-local/manifest.txt` run is historical after an
earlier runtime-base change. Hosted PR and scheduled delivery, plus deployed
QA, are NOT_CHECKED. Keep #8785 open for that evidence.


## Local replay before the runtime merge wave

At `a4698cd87ed66d0e7e174ae041f21832f05d0bfe`, based on main
`34922d077381bbef2b44cb70f518c12bd3f8b24a`, the same two roots passed
with Go 1.26.10 and race detection on fresh instances of the pinned backends
above. Each root ran and passed once, with zero skips or failures; the strict
event verifier exited 0. Both owned containers were removed. The test source
and current workflow hashes match the receipt.

The Go process took 30.588538 seconds and the complete local run took
44.251013 seconds. These are observed boundaries on this host, not a speed
comparison. The local command retained its eight-minute test bound; the
workflow uses twelve minutes. The compact receipt is
`8785-latest-local-proof-summary.json`; raw events and the complete receipt
are in `/tmp/eshu-7533-drive/8785-latest-local/`. This replay establishes
local acceptance on the latest base. Hosted delivery and deployed QA remain
NOT_CHECKED, so #8785 stays open.


## Post-merge local acceptance

Main's supply-index, publishes-projection, and parser-admission changes were
integrated before a fresh replay at
`59258b81216035ce4badf50df553c1fdd9a4e0ea`, based on
`4d78b8327311c56baa2fe54c9b59b56ba6d6935f`. Both exact roots ran and passed
once with race detection, with zero skips, failures, or unexpected roots.
The existing strict event verifier exited 0. The same pinned PostgreSQL 18.6
Bookworm and Neo4j 2026.08.1 images were used, and both owned containers
were removed with exit 0. The source and event hashes were independently
rechecked after the run.

The observed Go-process wall was 24.32 seconds, and the complete fixture run
was 37.97 seconds. These are functional observations, not a speed comparison
with earlier bases. `8785-latest-local-proof-summary.json` records this
checkpoint; its complete raw receipt and events are retained under
`/tmp/eshu-7533-drive/8785-postmerge-local/`. The earlier runs remain
historical evidence. Hosted delivery and deployed QA remain NOT_CHECKED.


## Combined delivery and queue image

PR #8937 combines this enrollment with #8927’s IaC readiness mapping. The delta workflow now uses the existing GHCR Neo4j mirror with the same immutable digest. The original Docker Hub reference failed the queue-path image gate; the existing verifier is the regression check. Image contents and projector test sources are unchanged, so the recorded backend replays remain historical source-bound evidence. Current combined-head hosted execution and deployed QA remain NOT_CHECKED.

## Acceptance item 2: scheduled lane green

`.github/workflows/delta-union-scheduled.yml` ("Delta Union Scheduled Proof")
ran on the daily schedule and on manual dispatch. Observed on 2026-11-02 with
`gh run list --workflow "Delta Union Scheduled Proof" --limit 5`: runs 825
(`93d191b7f8d9`, 13:37:46Z), 826 (`a36984a42b00`, 13:46:14Z), 827
(`622544622a97`, 14:17:06Z), 828 (`62ef607a9125`, 15:42:15Z), and 829
(`3cd5cf098327`, 15:47:47Z), each `completed` / `success`. The two-stable-read
rule was satisfied by re-querying the same command and comparing the full check
set. This closes the `class: scheduled` gap the issue raised: the two
`superseded_delta_overlay_live_test.go`-style scheduled entries now have a
workflow that actually executes them.

## Acceptance item 3: hosted QA deployment, 2026-11-02

Environment: the hosted QA cluster, namespace `eshu`. The projection loop runs in the
ingester statefulset pod `eshu-0`, image
`ghcr.io/eshu-hq/eshu:sha-56f6d19678bbe2922db766cf08cdf89a11dfd752@sha256:107711488e8c0e17ace517a55bde3dcb9e2bae4c6ffb7ca990bb47e7fa541e4d`,
pod `startTime` `2026-11-02T14:55:45Z`, `restartCount` 0. Metrics were read
with `kubectl exec eshu-0 -- sh -c 'wget -qO- http://127.0.0.1:9464/metrics'`
(the `get --raw` proxy form times out on this cluster), and Postgres through
`eshu-postgresql-read-0`.

Delta generations reach active through the union path:

```sql
select count(*) filter (where status='active'),
       count(*) filter (where delta_union_base_commit_sha is not null),
       max(activated_at)
from scope_generations where is_delta;
-- 20 | 1045 | 2026-11-02 15:56:20.332747+00
```

Two consecutive reads gave 21/1045 and 20/1045 active/union-based deltas with
the same `max(activated_at)`, so activation is live in this process window
(15:56:20Z against a 14:55:45Z start). 1045 of the delta rows carry a non-null
`delta_union_base_commit_sha`, the #8780 column the union read resolves
(`COALESCE(union base, baseline)` in `deltaUnionCommitsFor`).

Boundary dead-letters stay frozen:

```sql
select count(*), max(updated_at) from fact_work_items
where status='dead_letter' and failure_message like '%does not match generation%';
-- 188 | 2026-10-23 14:28:11.736906+00
```

188 rows, none newer than 2026-10-23, and 0 rows carry the union variant of the
message (`%or its union base%`), which is the boundary check in
`go/internal/projector/runtime/union_boundary.go`. No new projector dead-letter
carries the boundary message after the #8779 deploy.

Delta-versus-full canonical write duration. The stage histogram is labelled
only by `stage` and scope kind, so the two `canonical_write` series in this
process are:

```
eshu_dp_projector_stage_duration_seconds_sum{stage="canonical_write"} 0.991314882   count 1
eshu_dp_projector_stage_duration_seconds_sum{stage="canonical_write"} 49.83982555   count 40
```

mean 1.25 s over 40 writes, with the writer's own
`eshu_dp_canonical_write_duration_seconds{write_phase="atomic_group"}` at
sum 49.70453744 / count 41 (mean 1.21 s). An earlier read of the same process
gave 39 writes / 40.609 s (mean 1.04 s), so both reads sit near 1 s per
canonical write. The histograms carry no delta-versus-full label, so the
per-kind split is NOT_CHECKED; the union path hands the writer the full file
set, which is the shape the #8780 note predicts.

### Counter gap and its cause

`eshu_dp_projector_delta_union_total` and `eshu_dp_projector_delta_union_facts`
were absent from the ingester's 9464 exposition in four reads, while the
adjacent `eshu_dp_projector_delta_baseline_fence_total` was present. The cause
is wiring, not the union logic: the projector `Service` in
`go/cmd/ingester/wiring.go` built its `FactStore` with
`postgres.NewFactStore(database)` and no `.WithInstruments(...)`, and
`recordDeltaUnion`/`recordDeltaUnionRefused` skip the metric when
`s.instruments` is nil (`go/internal/storage/postgres/delta_union.go`, nil
guard over `go/internal/storage/postgres/facts.go` `WithInstruments`). The
`cmd/projector` wiring already passes instruments
(`go/cmd/projector/runtime_wiring.go:108`), so only the ingester-hosted loop
was silent. This commit chains `.WithInstruments(instruments).WithLogger(logger)`
onto that `FactStore` — the same pair the projector binary passes — plus a
`FactStore.Instruments()` accessor so the wiring is asserted from outside the
package; the counters and the one INFO line per union decision appear from the
image carrying this commit onward, and the DB columns above are the
authoritative union-resolution proof for the window that predates it.

### Local proof for the wiring change

Regression pair in `go/cmd/ingester/delta_baseline_wiring_test.go`
(`TestBuildIngesterProjectorServicePassesInstrumentsToFactStore`), which reads
the instruments and the logger back off the projector `Service` through the new
exported `FactStore.Instruments()` and `FactStore.Logger()` accessors:

```
without the accessors, without the chain  → FAIL (no accessors, %T = *postgres.FactStore)
accessors, no .WithInstruments            → FAIL (instruments = <nil>)
accessors, no .WithLogger                 → FAIL (logger = <nil>)
accessors + both links                    → ok
```

```
go build ./cmd/ingester/ ./internal/storage/postgres/          → 0
go vet   ./cmd/ingester/ ./internal/storage/postgres/          → 0
go test -count=1 ./cmd/ingester/                               → ok (1.237s)
ESHU_DEPLOY_CHECKOUT=<deploy @6b6164d> go test -count=1 \
  ./cmd/ingester/ ./internal/storage/postgres/                 → ok (1.237s / 44.473s)
```

`./internal/storage/postgres` needs `ESHU_DEPLOY_CHECKOUT` pointed at an
`eshu-hq/eshu-deploy` checkout at `6b6164d4fd95b8959284c5af0ba9a233fe032134`
(CI supplies it in `ifa-determinism-gate.yml`); without it only
`TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline` fails, and that is
the environment gate, not this change.

### Performance and observability record

No-Regression Evidence: baseline `ebaf4dfa0d`, deployed image
`sha-56f6d19678bbe2922db766cf08cdf89a11dfd752`. The only code change is one
option call on the projector `Service` `FactStore` plus a read-side getter, so
no query, lock, lease, or queue step is added and no new benchmark was run; the
figures below are two consecutive reads of one pre-change process window, and
the post-change image is not yet deployed, so its numbers are NOT_CHECKED.
Backend and input shape are unchanged: the pinned PostgreSQL 18.6 Bookworm and
Neo4j 2026.08.1 service images, the same synthetic scope with two full base
commits and a two-file delta. The first read of
`eshu_dp_projector_stage_duration_seconds{stage="canonical_write"}` gave
`40.609` over 39 writes (mean 1.04 s); the second read of the same process gave
`49.83982555` over 40 writes plus `0.991314882` over 1 (mean 1.25 s), and the
writer's own `eshu_dp_canonical_write_duration_seconds{write_phase="atomic_group"}`
read `49.70453744` over 41 (mean 1.21 s), so both reads sit near 1 s per write
and the added work is two pointer fields per constructed store, at wiring time.
Terminal counts: 20 active delta
generations, 1045 rows carrying `delta_union_base_commit_sha`, and 188 frozen
boundary dead-letters with `max(updated_at)` `2026-10-23 14:28:11`. Why the
change is safe: `WithInstruments` and `WithLogger` each copy one pointer into a
zero-value field on a struct-local value before the first projection, the
identical pattern `go/cmd/projector/runtime_wiring.go:108` already uses, and they
add no query, lock, lease, or queue step.

Observability Evidence: with instruments and the logger wired, the ingester
exposition carries
the union pair `eshu_dp_projector_delta_union_total` and
`eshu_dp_projector_delta_union_facts` next to the already-present
`eshu_dp_projector_delta_baseline_fence_total` (observed value 14), so an
operator at 3 AM can separate a union-resolution refusal from a fence refusal
without reading the database, and the process log carries the one INFO line per
union decision that `cmd/projector` already emits. The two counters are already
registered and documented in
`docs/public/observability/telemetry-coverage-reducer-core.md`, so
no new row is owed; `scripts/verify-telemetry-coverage.sh` exits 0 and its suite
`scripts/test-verify-telemetry-coverage.sh` passes 69/69.

### Follow-up record (logger parity, PR #9883 follow-up)

No-Regression Evidence: baseline `a82188b6df` (the merged #8785 leaf, the first
image with the instruments link), deployed image
`sha-56f6d19678bbe2922db766cf08cdf89a11dfd752`. The added work is the second
link in the same chain, `.WithLogger(logger)`, plus two read-side accessors: no
query, lock, lease, or queue step, and no new benchmark was run. After
measurement in one process window of that pre-change process:
`eshu_dp_projector_stage_duration_seconds{stage="canonical_write"}` `49.83982555`
over 40 writes plus `0.991314882` over 1, and
`eshu_dp_canonical_write_duration_seconds{write_phase="atomic_group"}`
`49.70453744` over 41, so about 1.2 s per canonical write on both series; the
post-change image is not yet deployed, so its numbers stay NOT_CHECKED. Backend
and input shape are unchanged from the reads above: the pinned PostgreSQL 18.6
Bookworm and Neo4j 2026.08.1 service images, the same synthetic scope with two
full base commits and a two-file delta. Terminal counts: 20 active delta
generations, 1045 rows carrying `delta_union_base_commit_sha`, 188 frozen
boundary dead-letters with `max(updated_at)` `2026-10-23 14:28:11`. Why the
change is safe: `WithLogger` copies one pointer into a zero-value field on a
struct-local value before the first projection, the same shape
`go/cmd/projector/runtime_wiring.go:108` already uses, and the nil logger keeps
the existing skip behavior for any caller that does not pass one.

Observability Evidence: with both links wired the ingester process shows the
same three signals as `cmd/projector` — `eshu_dp_projector_delta_union_total`,
`eshu_dp_projector_delta_union_facts`, and the one INFO line per union decision —
next to `eshu_dp_projector_delta_baseline_fence_total` (observed value 14 in the
pre-change window), and `scripts/verify-telemetry-coverage.sh` still exits 0 with
its suite at 69/69. Local proof at this head: `go test -count=1 ./cmd/ingester/`
ok 1.237s and, with `ESHU_DEPLOY_CHECKOUT` pinned at 6b6164d,
`./internal/storage/postgres` ok 44.473s.
