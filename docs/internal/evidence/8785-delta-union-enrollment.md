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
a local backend run on that combined tree is NOT_CHECKED. The older
`/tmp/eshu-7533-drive/8785-local/manifest.txt` run is historical after an
earlier runtime-base change. Hosted PR and scheduled delivery, plus deployed
QA, are NOT_CHECKED. Keep #8785 open for that evidence.
