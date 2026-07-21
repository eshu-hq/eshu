# ifa

## Purpose

`ifa` is the command entry point for the Ifá conformance platform
([#4393](https://github.com/eshu-hq/eshu/issues/4393),
[#4394](https://github.com/eshu-hq/eshu/issues/4394)). P0 shipped a thin shell
proving the command/package boundary. P1 adds two subcommands over that
shell: `ifa coverage`, which reconciles `go/internal/ifa`'s derived
expectations against `specs/ifa-coverage-manifest.v1.yaml`, and
`ifa expectations`, which prints the derivation itself. P2
(`drive.go`, issue #4395) adds `ifa drive`, the concurrent replay driver verb:
it drives `go/internal/replay/concurrentreplay.Driver` over a recorded
cassette against a real Postgres `IngestionStore`, proving the acceptance
clause "driver passes -race; same Odù drains (`fact_work_items.residual_max:0`)
at N=1" end to end. P3 (`graph_dump.go`, `graphdump_reader.go`, issue #4396)
adds `ifa graph-dump`, the graph-truth half of the P3 determinism matrix: it
reads the live graph backend through `go/internal/ifa/graphdump.Reader` and
prints the graph's stable canonical byte form (or, with `-digest`, its sha256
hex digest), so a follow-on determinism-matrix slice can compare the graph
produced at different worker counts for exact equality.

## Ownership Boundary

This command owns CLI entry wiring, flag parsing, and report I/O only. All
conformance, derivation, and coverage logic lives in `go/internal/ifa`;
`coverage.go` and `expectations.go` here are thin subcommand wrappers that load
inputs from disk and call into that library. `drive.go` is a thin wrapper the
same way: cassette parsing and concurrent-safe draining live in
`go/internal/replay/cassette` and `go/internal/replay/concurrentreplay`, not
here. `graph_dump.go` follows the same shape: canonicalization logic lives in
`go/internal/ifa/graphdump`, which is deliberately driver-free (see that
package's README "Ownership Boundary"); `graphdump_reader.go` is this
command's own Bolt-backed `graphdump.Reader` implementation, the one place in
the repo allowed to bridge that hermetic package to a live NornicDB/Neo4j
session.

## Exported Surface

- `ifa -version` - prints the command's version banner (P0, unchanged).
- `ifa coverage [-specs-dir] [-snapshot] [-manifest] [-replay-manifest]
  [-gates] [-report-out] [-blocking]` - runs `ifa.RunCoverage` and prints the
  advisory/blocking summary; writes the JSON report when `-report-out` is set;
  exits non-zero only when `-blocking` is passed and the gate fails.
- `ifa expectations [-specs-dir] [-snapshot] [-replay-manifest] [-kind]
  [-format json]` - prints `ifa.Derive`'s output as JSON, optionally filtered
  to one fact kind.
- `ifa drive -cassette <path> [-workers N] [-postgres-dsn]` - drives
  `concurrentreplay.Driver` at the requested worker count (default 1) over the
  cassette at `-cassette`, committing through a Postgres `IngestionStore`, and
  prints the resulting `Report` (workers used, generations committed, wall
  time). It applies no schema DDL and runs neither the projector nor the
  reducer itself — draining the `fact_work_items` rows it enqueues requires
  `cmd/projector`/`cmd/reducer` running separately against the same database,
  exactly as `scripts/verify-ifa-replay-drive.sh` orchestrates.
- `ifa drive -from-facts -facts-source-dsn <src> -postgres-dsn <target>
  [-workers N]` (issue #5008) - the same driver, but the source is the
  `fact_records` already persisted in `<src>` rather than a cassette. It
  enumerates the active generation of every fact-bearing scope via
  `FactStore.ListScopeGenerationWork` and
  re-drains their recorded facts through `concurrentreplay.FactSliceSource` into
  the **distinct** commit target `<target>`. The two DSNs must differ — a
  re-drain into the source database is a no-op — and `-from-facts` is mutually
  exclusive with `-cassette`. This is the source half of the B-12 corpus
  determinism composition (re-drain the golden corpus at N in {1,4} into fresh
  graph DBs, assert a byte-identical canonical graph across N).

#### `-from-facts` re-drain performance & observability (#5008)

- No-Regression Evidence: the `-from-facts` re-drain and its
  `FactStore.ListScopeGenerationWork` enumeration are new code off every runtime
  hot path — the enumeration is invoked only by this operator/CI verb, once per
  re-drain, never per-fact, per-request, or per-reduce. The query is
  `SELECT DISTINCT scope_id, generation_id FROM fact_records` (bounded by the
  corpus: the B-12 golden corpus is ~20 scope generations) joined to
  `ingestion_scopes`/`scope_generations` for hydration; the DISTINCT keys are the
  two leading columns of the existing `fact_records_scope_generation_idx
  (scope_id, generation_id, …)` composite index. No existing
  ingest/query/reducer path is altered, and the commit half reuses the same
  `postgres.IngestionStore` path cassette-mode `ifa drive` already exercises;
  the `cmd/ifa`, `internal/storage/postgres`, and `internal/replay/concurrentreplay`
  exactness lanes stay green (1562 tests). Baseline vs after: the enumeration is
  a new read with no prior shape to regress; wall time is dominated by the
  unchanged `Driver.Run` commit path, whose worker-count behavior part 2 measures
  across N.
- No-Observability-Change: the re-drain adds no runtime stage. `ifa drive` emits
  its existing one-line `Report` (source label, workers, generations committed,
  duration) through the JSON slog logger, and the commit path's spans/metrics are
  the already-instrumented `IngestionStore` ones; the enumeration is a single
  bounded read with no new telemetry surface.
- `ifa graph-dump [-out FILE] [-digest]` - opens a live Bolt connection to the
  configured graph backend (`ESHU_GRAPH_BACKEND`/`NEO4J_URI`/
  `NEO4J_USERNAME`/`NEO4J_PASSWORD`/`NEO4J_DATABASE`, the same env contract
  every Bolt-backed Eshu binary honours via `runtime.OpenNeo4jDriver`), reads
  every node and relationship, and writes
  `go/internal/ifa/graphdump.Canonicalize`'s stable canonical byte form to
  `-out` or stdout; with `-digest`, it writes the sha256 hex digest instead.
  It is a read-only diagnostic verb: it applies no schema DDL and performs no
  write.
- `ifa assert-edges -domain DOMAIN -expected FILE` (#5351) - the Ifá
  materialized-edge exhaustiveness gate's LIVE, set-exact non-vacuity
  assertion. It opens the same read-only Bolt connection `ifa graph-dump` uses,
  reads every edge of the named materialized-edge family's registry types
  (e.g. `-domain sql_relationships` → the seven types
  `cypher.SQLRelationshipMaterializedEdgeTypes()` accepts), and asserts the
  family's materialized edges are EXACTLY the hand-derived expected set in
  `-expected` (same count, same `relationship_type`/source-uid/target-uid
  triples). This is the assertion `ifa graph-dump -digest`'s determinism
  comparison cannot make: a family that materializes ZERO edges in ALL cells
  has an identical digest in every cell and passes the digest comparison
  vacuously; the absolute expected set catches that regression. Wired into both
  the `ifa-determinism` (per cell) and `ifa-fault-injection` (baseline) live
  gates so the `materialized_edges:sql_relationships` coverage manifest row's
  two `proof_gate`s are actually backed by a replay of the family. Read-only:
  no schema DDL, no write.
- `ifa mutate-cassette -cassette FILE -out FILE -fact-kind KIND -kind
  missing-field|schema-major [-field F] [-schema-major V] [-count N]` -
  Ifá P3 failure-path-determinism fixture generator (ADR step 3a): loads
  `-cassette` through the production `cassette.LoadFile` seam, corrupts `-count`
  facts of `-fact-kind` via `go/internal/ifa.MutateCassette`, and writes the
  result to `-out` — a new file, never the source. It performs no I/O beyond
  reading `-cassette` and writing `-out`.
- `ifa dead-letters [-out FILE] [-postgres-dsn]` - reads the durable
  `fact_work_items` dead-letter set (`status='dead_letter'`) from Postgres
  (same DSN precedence as `ifa drive`) and prints it as deterministic sorted
  JSON via `go/internal/ifa.DeadLetterRecord`/`SortDeadLetterRecords`. Read-only:
  one `SELECT`, no schema DDL or write. Deliberately does not reuse
  `cmd/golden-corpus-gate`'s drain SQL, which counts `dead_letter` rows AS
  residual by design; this verb's whole purpose is to read those rows.
- `ifa synth-cassette -seed N [-projects K] [-resources R] -out FILE` (issue
  #4396 slice 6b) - wraps `go/internal/synth/gcp.GenerateMultiScope`,
  generating a deterministic cassette with `K` independent GCP project scopes
  (`-projects`, default 4) of `R` resources each (`-resources`, default 16),
  and writing its canonical bytes to `-out`. Exists to fix the finding that a
  single-scope cassette gives `concurrentreplay.Driver` exactly one work unit
  for ANY `-workers` count, making `ifa drive -workers N` inert; distinct
  `ProjectID`s per scope (`acme-demo-gcp-00`, `acme-demo-gcp-01`, ...) keep
  every scope's `full_resource_name`/CloudResource-uid disjoint by
  construction. Never overwrites anything; no synth-cassette output is ever
  checked into `testdata/` — every caller regenerates it into a scratch/work
  directory per run.

## Dependencies

The command depends on `go/internal/ifa`, `go/internal/facts`,
`go/internal/goldengate`, `go/internal/replaycoverage`, and
`go/internal/cigates` for loading and reconciling its `coverage`/`expectations`
inputs. It intentionally does not depend on collector or parser internals —
that boundary is unchanged.

`ifa drive` (P2) widens the command's dependency graph beyond P0/P1's
database-free footprint by design: `docs/internal/design/4389-ifa-conformance-platform.md`'s
"Placement" section lists `internal/projector` (`FactStore.LoadFacts`),
`internal/reducer` as a library, and `internal/storage/postgres` "for the
replay slice" as `internal/ifa`'s own contract-only dependencies, alongside
`internal/replay` (cassette codec, canonicalizer, reused verbatim). The ADR
draws the line at collector and parser *internals* specifically ("It must not
import collector internals (1846-file blast radius) or parser internals; it
observes their output through `facts.Envelope`"), not at Postgres or the
reducer as a library. `drive.go` therefore imports
`go/internal/replay/cassette`, `go/internal/replay/concurrentreplay`,
`go/internal/runtime`, and `go/internal/storage/postgres` — the same
collector/database dependencies every cassette-mode collector binary already
takes — without violating that boundary. Do not read the pre-P2 "does not
depend on collector or parser internals" line as forbidding Postgres; the ADR
is explicit that Postgres and reducer-as-library are in scope for the replay
slice.

`ifa graph-dump` (P3) adds `go/internal/ifa/graphdump` (canonicalization,
driver-free) and, in this command only, `github.com/neo4j/neo4j-go-driver/v5`
via `graphdump_reader.go`'s `boltGraphReader` — the same shared
`runtime.OpenNeo4jDriver` seam `cmd/golden-corpus-gate` already uses, not a
new driver dependency for the repo. `internal/ifa/graphdump` itself takes on
no new dependency: it stays driver-free by design (see its README's
"Ownership Boundary").

`ifa mutate-cassette` and `ifa dead-letters` (P3, ADR step 3a) add no new
dependency: `mutate_cassette.go` uses `go/internal/ifa` (`MutateCassette`) and
the already-imported `go/internal/replay/cassette`; `dead_letters.go` reuses
`driveOpenPostgres` (unexported, defined in `drive.go`, same package) and
`go/internal/ifa` (`DeadLetterRecord`, `SortDeadLetterRecords`).

`ifa synth-cassette` (issue #4396 slice 6b) adds `go/internal/synth/gcp` as a
dependency: `synth_cassette.go` is a thin flag/IO wrapper over
`gcp.GenerateMultiScope`, performing no database or graph-backend I/O of its
own.

## Telemetry

No runtime telemetry is emitted. This is not a deployed service; the coverage
report, drive report, graph-dump canonical output/digest, mutate-cassette
report, and dead-letters JSON are the operator-facing artifacts.

## Gotchas / Invariants

- `ifa mutate-cassette`'s two `-kind` values reach very different runtime
  failure paths for a fact kind core registers a schema version for (proven
  empirically with `scripts/verify-ifa-dead-letter-determinism.sh` against a
  real Postgres + NornicDB stack, not just by reading the decode seam):
  `missing-field` is QUARANTINED per fact (metric + log, no durable
  `fact_work_items` row); `schema-major` trips the projector's own
  admission-time schema-version gate
  (`go/internal/projector/schema_version_admission.go`) BEFORE the reducer's
  typed-decode seam is ever reached, dead-lettering the whole projector work
  item durably. The durable row's `failure_class` came back `"projection_bug"`
  in that run, not the reducer's `"input_invalid"` — do not assume a fixed
  `failure_class` literal for a given `-kind`; assert on `status='dead_letter'`
  and compare full rows (`ifa.DeadLetterSetsEqual`) instead. See
  `go/internal/ifa/mutate.go`'s `MutationKind` doc comment for the full
  path-by-path breakdown.

## Related Docs

- `go/internal/ifa/README.md`
- `go/internal/ifa/graphdump/README.md`
- `docs/internal/design/4389-ifa-conformance-platform.md`
- `scripts/verify-ifa-determinism.sh` - the P3 determinism-matrix gate (slice
  5, #4396): drives `ifa graph-dump` at N ∈ {1, 2, 4} against independent
  fresh Postgres + NornicDB stacks and asserts the resulting canonical graphs
  are byte-identical. See `go/internal/ifa/graphdump/README.md`'s "Benchmark
  Evidence" section for a recorded run.
