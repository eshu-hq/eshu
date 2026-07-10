# ifa/graphdump

## Purpose

`graphdump` canonicalizes an arbitrary property graph (any label set, any
edge type) into a stable, content-addressed byte form so two reads of a graph
can be compared for exact equality. It is the graph-truth half of Ifá's P3
determinism matrix (issue [#4396](https://github.com/eshu-hq/eshu/issues/4396),
design doc `docs/internal/design/4389-ifa-conformance-platform.md`, Layer 2):
after replaying the same Odù at worker counts N ∈ {1, 2, 4, ...}, a follow-on
slice's determinism matrix canonicalizes the resulting graph at each N and
asserts the bytes are identical, so a divergence is a real concurrency defect
(a MERGE race, a dropped write, an ordering-dependent projection) rather than
a scan-order or backend-ID artifact.

See `doc.go` for the full design rationale (content addressing vs. ID
addressing, the reused canonical JSON core, and the over-normalize /
under-normalize tradeoff behind `normalize.go`'s denylist).

## Ownership Boundary

This package owns canonicalization logic only: `Node`, `Edge`, `Reader`,
`Canonicalize`, `Digest`, and `Equal`. It is deliberately driver-free — it has
no NornicDB/Neo4j/Bolt dependency and no test requires Docker or a live graph
— so its logic is provable against the in-memory `fakeReader` every test in
`graphdump_test.go` uses.

A live, Bolt-backed `Reader` implementation is out of this package's scope by
design: `go/cmd/ifa`'s `ifa graph-dump` verb (`graphdump_reader.go`,
`graph_dump.go`) implements `Reader` over a real NornicDB/Neo4j session and is
the only production caller. Keeping the Bolt dependency in `cmd/ifa` rather
than here means a change to the driver, the connection lifecycle, or the
`runtime.OpenNeo4jDriver` env contract can never silently change this
package's hermetic test guarantee.

## Exported Surface

- `Node{Labels []string, Props map[string]any}` - a node's canonicalizable
  identity; no internal element ID.
- `Edge{Type string, FromLabels/FromProps, ToLabels/ToProps, Props}` - a
  relationship's canonicalizable identity; endpoints are repeated by
  labels+props, never referenced by index or backend ID.
- `Reader` - the narrow `Nodes(ctx)`/`Edges(ctx)` read seam `Canonicalize`
  needs; production callers implement it over a live Cypher session (see
  `go/cmd/ifa/graphdump_reader.go`), tests implement it over a plain slice.
- `Canonicalize(ctx, Reader) ([]byte, error)` - returns the graph's stable
  canonical byte form: content-addressed, order-independent, and idempotent.
- `Digest(ctx, Reader) (string, error)` - the sha256 hex digest of
  `Canonicalize`'s output.
- `Equal(ctx, a, b Reader) (bool, error)` - convenience wrapper comparing two
  Readers' digests.

## Dependencies

`go/internal/replay` for the shared canonical JSON core
(`CanonicalizeValue`/`CanonicalOptions`) — see `doc.go`'s "Reused canonical
JSON core" section for why this package does not implement a second
canonicalizer. No other internal or external dependency.

## Performance and observability evidence

(The marker lines below carry a trailing colon on purpose: the
`verify-performance-evidence.sh` hot-path gate matches `Performance Evidence:`
/ `Benchmark Evidence:` / `No-Regression Evidence:` and `Observability
Evidence:` / `No-Observability-Change:`.)

- No-Regression Evidence: `Canonicalize`/`Digest`/`Equal` are reached only
  from `ifa graph-dump` (a credential-free, read-only local diagnostic verb —
  see `go/cmd/ifa/graph_dump.go`) and this package's own tests; no production
  ingester, reducer, API, or MCP path calls them, so no existing hot path
  changes behavior or timing. `graphdump_reader.go`'s Bolt-backed `Reader`
  (`boltGraphReader`) issues two plain, unbounded `MATCH` reads
  (`MATCH (n) RETURN labels(n), properties(n)` and the one-hop edge
  equivalent) against the graph backend and performs no write of any kind;
  `neo4j.ExecuteQuery`'s default routing (`RoutingControl = Write`, the same
  default `cmd/golden-corpus-gate/graph.go`'s `boltGraphCounter` uses
  unchanged) sends the read to the same instance a write would, so this verb
  adds no new read-replica routing behavior either. This gate-worthy Cypher
  surface has no prior baseline to regress against: it is new, additive, and
  off the ingest/reducer/query hot path entirely.
- No-Observability-Change: this slice mints no new metric instrument, span,
  or dashboard. `runGraphDumpCommand` returns a plain error and writes its
  canonical bytes/digest to stdout or `-out`; there is no `log/slog` logging,
  no `telemetry.Instruments` field, and no operator-facing counter. Operator
  visibility for this verb is its own CLI output (canonical bytes or a
  digest), not a runtime signal — it has no runtime deployment to observe.
- Benchmark Evidence: prove-the-theory-first shim for the P3 determinism
  matrix (issue #4396), run ahead of the matrix script itself (a later
  slice). Three independent, fresh (fresh Postgres + fresh NornicDB, `docker
  compose down -v` between every run, distinct compose projects/ports) drives
  of `testdata/cassettes/gcpcloud/supply-chain-demo.json` through
  `scripts/verify-ifa-replay-drive.sh`'s `eshu-ifa drive` + projector/reducer
  drain, followed by `ifa graph-dump -digest` against the kept NornicDB:
  - Run A (`-workers 1`, fresh stack): digest
    `f692b33c72b99bb2ca44f25ca08804be425c96324186acd48995a6d59ccbc873`.
  - Run B (`-workers 4`, independent fresh stack, same unmodified cassette):
    digest `f692b33c72b99bb2ca44f25ca08804be425c96324186acd48995a6d59ccbc873`
    — byte-identical to Run A (`diff` of the two full canonical dumps is
    empty). Proves the canonical graph dump is deterministic across worker
    counts on fresh databases; no denylist change was needed for this pass
    (the existing `eshu_orphan_observed_at_unix` entry was sufficient), and no
    genuine cross-worker-count reducer nondeterminism was found.
  - Run C (`-workers 1`, independent fresh stack, cassette with exactly one
    payload value mutated: the `analytics` BigQuery dataset's
    `gcp_cloud_resource.payload.display_name`, `"analytics"` ->
    `"analytics-mutated-runC"`, which `go/internal/reducer/gcp_resource_materialization.go`
    projects onto the `CloudResource` node's `name` property): digest
    `e6adf7a86dfaafb884e226a68da3f5dc9f267bb76b9711163ac0834078bc8676` — differs
    from Run A. The full-dump diff isolates to the mutated `name` property plus
    the expected cascading node-digest/edge-endpoint/sort-order changes content
    addressing produces from that one changed value. Proves the canonical
    graph dump is sensitive to a single changed input value, i.e. the matrix's
    equality check cannot pass vacuously.
  - Reviewer rerun (three sequential invocations; ports/projects below avoid
    colliding with `verify-golden-corpus-gate.sh`'s 15432/7687/7474 or
    `verify-ifa-replay-drive.sh`'s own default 15532/7788/7575):
    ```bash
    # Run A (workers=1)
    REPLAY_DRIVE_COMPOSE_PROJECT=ifa-det-a ESHU_POSTGRES_PORT=15632 \
      NEO4J_BOLT_PORT=7789 NEO4J_HTTP_PORT=7676 REPLAY_DRIVE_WORKERS=1 \
      scripts/verify-ifa-replay-drive.sh --keep
    # note the "[--keep] work dir retained: <dir>" path, then:
    NEO4J_URI=bolt://localhost:7789 NEO4J_USERNAME=neo4j NEO4J_PASSWORD=change-me \
      NEO4J_DATABASE=nornic ESHU_GRAPH_BACKEND=nornicdb <dir>/bin/eshu-ifa graph-dump -digest
    docker compose -p ifa-det-a -f docker-compose.yaml down -v

    # Run B (workers=4, same cassette) — repeat with a fresh project/ports
    # (e.g. ifa-det-b / 15633 / 7790 / 7677) and REPLAY_DRIVE_WORKERS=4;
    # compare the digest to Run A's.

    # Run C (workers=1, mutated cassette) — copy
    # testdata/cassettes/gcpcloud/supply-chain-demo.json, change the
    # "analytics" gcp_cloud_resource fact's payload.display_name, then repeat
    # with a fresh project/ports (e.g. ifa-det-c / 15634 / 7791 / 7678) and
    # REPLAY_DRIVE_CASSETTE=<mutated path> (verify-ifa-replay-drive.sh's
    # cassette path is currently hardcoded; the reviewer's copy needs the same
    # one-line parameterization the shim used, or a manual `eshu-ifa drive
    # -cassette <mutated path> -workers 1` call against the same stack).
    # Compare the digest to Run A's; it must differ.
    ```
  - Each run tore down with `docker compose -p <project> -f docker-compose.yaml
    down -v`, confirmed via `docker ps -a` / `docker volume ls` / `docker
    network ls` filtered on the project name showing no leftovers.
  - **Automated matrix (slice 5, `scripts/verify-ifa-determinism.sh`):** the
    manual 3-run shim above is now a repeatable gate. One invocation drives
    N ∈ {1, 2, 4} sequentially against a reused Compose project/port triple
    (`eshu-ifa-determinism-<pid>`, postgres:15636, neo4j-bolt:7793,
    neo4j-http:7680 — distinct from every sibling `verify-ifa-*.sh` script's
    own defaults), `docker compose down -v` between every cell for a
    genuinely fresh Postgres + NornicDB each time, and asserts all three
    `ifa graph-dump -digest` outputs are byte-identical, printing the full
    canonical-graph diff on any divergence instead of hiding it. Rerun:
    `bash scripts/verify-ifa-determinism.sh`. Recorded run (2026-07-09, clean
    unmutated demo-org Odù):
    - N=1: digest
      `f692b33c72b99bb2ca44f25ca08804be425c96324186acd48995a6d59ccbc873`,
      cell wall time 72s.
    - N=2: digest
      `f692b33c72b99bb2ca44f25ca08804be425c96324186acd48995a6d59ccbc873`,
      cell wall time 71s.
    - N=4: digest
      `f692b33c72b99bb2ca44f25ca08804be425c96324186acd48995a6d59ccbc873`,
      cell wall time 68s.
    - All three digests equal (and byte-identical to Run A/B above) — matrix
      green. Total wall time ~211s for the full N ∈ {1, 2, 4} matrix on this
      machine, well inside the ~30-45 minute budget a larger corpus would
      need; the demo-org Odù is small (234 facts, 110 nodes materialized per
      cell), so most of each cell's ~70s is Compose container start/health-check
      overhead, not drive/drain/dump work. Confirmed no leftover containers,
      volumes, or networks after the run (`docker ps -a` / `docker volume ls`
      / `docker network ls` filtered on the project name, all empty).
  - **Multi-scope matrix (slice 6b, `scripts/verify-ifa-determinism.sh`,
    unmodified — no `--teeth`):** the demo-org Odù alone gives
    `concurrentreplay.Driver` exactly one work unit for ANY `-workers` count
    (see the reducer README's teeth section), so the recorded slice-5 run
    above proves repeatability, not a worker matrix. This run adds the
    generated synth-multiscope cassette (`ifa synth-cassette -seed 4396
    -projects 8 -resources 64`, `go/internal/synth/gcp.GenerateMultiScope`) as
    a SECOND `eshu-ifa drive -cassette <synth> -workers N` into the SAME cell
    stack, alongside the unmodified demo-org cassette, giving the driver 9
    genuinely independent work units (1 demo-org + 8 synth scopes). Each cell
    logged `fact_work_items enqueued (demo-org + synth-multiscope): 9` before
    draining, proving both drives actually committed work (not a vacuous
    drain). Recorded run (2026-07-09, project `eshu-ifa-determinism-15666`,
    postgres:15636, neo4j-bolt:7793):
    - N=1: digest
      `e3b183cb9e20fba3c3a3bb0690681502fc444263bc4fc9cd883259ef4ddf8682`,
      cell wall time 75s.
    - N=2: digest
      `e3b183cb9e20fba3c3a3bb0690681502fc444263bc4fc9cd883259ef4ddf8682`,
      cell wall time 71s.
    - N=4: digest
      `e3b183cb9e20fba3c3a3bb0690681502fc444263bc4fc9cd883259ef4ddf8682`,
      cell wall time 72s.
    - All three digests byte-identical — matrix green, proving the 8 synth
      scopes are disjoint by construction (no same-uid `MERGE` collision, no
      `source_fact_id` last-writer-wins false-red) even though `-workers N`
      now varies commit interleaving over 9 real work units instead of 1.
      Confirmed no leftover containers, volumes, or networks after the run.
    - **`--teeth` run (same cassette pair, `-tags ifadeterminismteeth`,
      expected and required to go RED):** all three digests diverged from
      each other:
      - N=1: digest `96a68dfc3bfa89c8a59793cf2a51ec66e0265245f1c90128d1fbb3dbebe4ae5a`,
        wall 72s.
      - N=2: digest `0dacc0881163611f067e110cd38ba7d9dfd0a576093a58a0d4ebda14d75932e5`,
        wall 69s.
      - N=4: digest `af2f494f6676d0d6734c741020e6b08c393f0b440ea0e3bbd2ad45350681230b`,
        wall 71s.
      - `TEETH: CAUGHT` — the script's own acceptance branch fired (exit
        non-zero is the expected --teeth pass). Inspecting the three kept
        (`--keep`) canonical dumps confirms the divergence is NOT solely the
        `ifa_teeth_write_order` wall-clock floor: correlating each
        `CloudResource` node's stable `resource_id` across cells shows
        `ifa_teeth_seq` itself differs for 591/622 nodes between N=1 and N=2,
        558/622 between N=1 and N=4, and 488/622 between N=2 and N=4 (same
        node identity, different process-relative sequence number) —
        confirming the counter is genuinely interleaving-sensitive on this
        9-work-unit fixture, unlike the single-scope run recorded in
        `go/internal/reducer/README.md`'s teeth section where the counter
        alone was measured INERT (identical digest across every N). Example:
        `resource_id
        //compute.googleapis.com/projects/acme-demo-gcp-00/computeName/synth-compute-30`
        got `ifa_teeth_seq=340` at N=1 vs. `ifa_teeth_seq=57` at N=2 — the
        same reducer-visible resource landed at a very different point in
        this run's own commit/claim order purely because of the worker
        count. `ifa_teeth_write_order` (wall-clock nanoseconds) also
        differed for all 622/622 nodes across every pair, confirming it
        still holds as the guaranteed-red floor alongside the now-restored
        counter. Confirmed no leftover containers, volumes, or networks after
        the run; the kept work dir (cassette + three canonical dumps) was
        deleted after this analysis — no synth cassette or dump is ever
        checked into `testdata/`.

## Verification

```bash
cd go && go test ./internal/ifa/graphdump/... -count=1
cd go && go test -race ./internal/ifa/graphdump/... ./cmd/ifa/... -count=1
```

## Related Docs

- `doc.go` - full design rationale.
- `go/cmd/ifa/README.md` - the `ifa graph-dump` verb this package's `Reader`
  is implemented for.
- `docs/internal/design/4389-ifa-conformance-platform.md` - the ADR (Layer 2,
  P3 determinism matrix).
