# AGENTS.md - cmd/ifa guidance

## Read first

1. `README.md` - command purpose and subcommand behavior.
2. `main.go` - subcommand dispatch and the P0 `-version` shell.
3. `coverage.go`, `expectations.go` - P1 subcommand wrappers.
4. `drive.go` - P2 concurrent replay driver verb (issue #4395); also carries
   the `-from-facts` re-drain mode (issue #5008) that sources
   `concurrentreplay.FactSliceSource` from persisted `fact_records` (enumerated
   by `postgres.FactStore.ListScopeGenerationWork`) instead of a cassette.
5. `graph_dump.go`, `graphdump_reader.go` - P3 graph-truth determinism verb
   (issue #4396).
5b. `assert_edges.go` - the materialized-edge exhaustiveness gate's live,
   set-exact non-vacuity assertion (#5351): reads the family's registry edge
   types off the same Bolt `Reader` and asserts they are exactly the expected
   set. Reuses `boltGraphReader` (graphdump_reader.go) and
   `go/internal/ifa.LoadExpectedEdges`/`MaterializedEdgeDomainEdgeTypes`. Wired
   into the `ifa-determinism` (per cell) and `ifa-fault-injection` (baseline)
   live gate scripts.
6. `mutate_cassette.go` - P3 failure-path-determinism fixture generator verb
   (ADR step 3a, issue #4396): wraps `go/internal/ifa.MutateCassette`.
7. `dead_letters.go` - P3 failure-path-determinism read verb (ADR step 3a,
   issue #4396): reads the durable `fact_work_items` dead-letter set.
8. `synth_cassette.go` - P3 slice 6b multi-scope fixture generator verb
   (issue #4396): wraps `go/internal/synth/gcp.GenerateMultiScope` so
   `ifa drive -workers N` has more than one work unit to fan out across.
9. `go/internal/ifa/AGENTS.md` - library contract.
10. `go/internal/ifa/graphdump/AGENTS.md` - the canonicalization package
    `graph_dump.go` calls into.
11. `docs/internal/design/4389-ifa-conformance-platform.md` - the ADR; read its
    "Placement" section before touching this command's dependency graph, and
    step 3a for the failure-path-determinism requirement `mutate_cassette.go`/
    `dead_letters.go` serve.

## Invariants

- The command is a thin shell over `internal/ifa`; keep conformance,
  derivation, and coverage-reconciliation logic in the library package. New
  subcommands parse flags, load inputs from disk, call into `internal/ifa`, and
  render output — nothing more.
- **P0/P1 dependency line, narrowed by the ADR for P2 — do not widen it
  further without re-reading the ADR.** The ADR's "Placement" section
  (`docs/internal/design/4389-ifa-conformance-platform.md`) lists
  `internal/projector` (`FactStore.LoadFacts`), `internal/reducer` as a
  library, and `internal/storage/postgres` "for the replay slice" as
  `internal/ifa`'s own contract-only dependencies, alongside `internal/replay`
  (cassette codec, canonicalizer). The hard line the ADR actually draws is:
  "must not import collector internals (1846-file blast radius) or parser
  internals; it observes their output through `facts.Envelope`." `drive.go`
  (P2) therefore may depend on `go/internal/replay/cassette`,
  `go/internal/replay/concurrentreplay`, `go/internal/runtime`, and
  `go/internal/storage/postgres` — this is a deliberate, ADR-sanctioned
  widening from P0/P1's database-free footprint, not a rule violation. Do not
  add a collector-internals or parser-internals import to any file in this
  package on the theory that `drive.go` already "broke" the boundary; the ADR
  narrows to collector/parser internals specifically, not everything beyond
  P0/P1's original footprint.
- Keep output deterministic so `make prove`-style integration and CI can
  compare it byte-for-byte. `ifa drive`'s `Report` line (workers, generations
  committed, duration) is the exception: duration is wall-clock and expected to
  vary run to run — do not add it to any byte-for-byte comparison.
- `ifa coverage` must not hard-fail on the `ifa-contract-layer` gate's own
  "not blocking" proof-gate finding; that gate is deliberately kept advisory
  and the finding is surfaced through the goldengate.Report instead. Do not
  copy `cmd/replay-coverage-gate/main.go`'s unconditional proof-gate hard-fail
  here without re-deciding that gate's blocking status first.
- `ifa drive` loads the cassette before opening Postgres (see `runDriveCommand`
  in `drive.go`). A bad `-cassette` path must fail without requiring a live
  database, so hermetic tests (`drive_test.go`) can cover it without Docker or
  Postgres.
- `ifa drive` does not apply schema DDL and does not run the projector or
  reducer. Those are `cmd/bootstrap-data-plane`/`cmd/projector`/`cmd/reducer`'s
  jobs, orchestrated by `scripts/verify-ifa-replay-drive.sh` — conflating them
  into this verb would hide which stage a drain failure came from.
- `ifa drive -workers` defaults to 1 (`driveDefaultWorkers`), matching the
  #4395 acceptance clause's N=1 mode. Do not special-case `-workers` values
  here beyond what `concurrentreplay.Driver.Run` already normalizes (<=0
  treated as 1); the Driver, not this CLI, owns that default.
- `ifa graph-dump`'s Bolt-backed `Reader` (`boltGraphReader` in
  `graphdump_reader.go`) belongs in this command, not in
  `internal/ifa/graphdump`: that package is deliberately driver-free so its
  canonicalization logic stays hermetically testable. Do not move
  `boltGraphReader` (or any neo4j-go-driver import) into
  `internal/ifa/graphdump` without re-deciding that boundary first.
- `ifa graph-dump` parses flags before opening the graph backend (see
  `runGraphDumpCommand` in `graph_dump.go`), the same ordering `runDriveCommand`
  uses for `-cassette` before Postgres — a bad flag must fail without
  requiring a live database, so hermetic tests can cover it without Docker or
  a graph backend.
- `ifa graph-dump` is read-only: it applies no schema DDL and issues only the
  two `MATCH` reads in `graphdump_reader.go` (`boltNodesCypher`/
  `boltEdgesCypher`). Do not add a write statement to this verb.
- `ifa assert-edges` is the non-vacuity check the P2 digest cannot make: it
  MUST fail on a family that materialized zero edges (an empty family passes a
  digest comparison vacuously). Its `edgeTypes` filter is registry-derived via
  `ifa.MaterializedEdgeDomainEdgeTypes` (never hand-listed), and an edge with a
  missing endpoint `uid` is a real defect (an unmaterialized endpoint node),
  surfaced, never silently skipped — that exact silent no-op is what #5351's
  fixture work surfaced. Read-only, flags-before-backend like `graph-dump`; the
  set-comparison core (`assertMaterializedEdges`) takes a `graphdump.Reader` so
  it is hermetically testable against a fake with no Docker. A new family gains
  live coverage by adding its case to `ifa.MaterializedEdgeDomainEdgeTypes` and
  its cassette/expected-set to BOTH gate scripts — not by hand-listing edge
  types here.
- `ifa mutate-cassette` NEVER overwrites `-cassette`; it always writes to
  `-out`, a separate path. Never point `-out` at a committed `testdata/`
  cassette in a script or CI job.
- `ifa mutate-cassette`'s `-kind` values do not map onto a single fixed durable
  outcome — see this directory's `README.md` "Gotchas / Invariants" and
  `go/internal/ifa/mutate.go`'s `MutationKind` doc comment before writing a
  new caller that assumes a specific `failure_class` string for either kind.
- `ifa dead-letters` deliberately does not reuse
  `cmd/golden-corpus-gate/drains.go`'s SQL: that gate's residual query counts
  `dead_letter` rows AS residual by design. Do not "fix" `dead-letters` to
  match that gate's semantics; they answer different questions.
- `ifa synth-cassette` never writes to a committed `testdata/` path; every
  caller (in particular `scripts/verify-ifa-determinism.sh`) regenerates its
  output into a scratch/work directory per run and never checks it in. Do not
  add a default `-out` that points inside `testdata/`.
- `ifa synth-cassette`'s disjointness guarantee (distinct `ProjectID` per
  scope, so no two scopes' `full_resource_name`/CloudResource uid collide) is
  proven in `go/internal/synth/gcp`, not here — this verb is a thin wrapper
  and must not add its own project-id derivation.

## Verification

```bash
cd go && go test ./cmd/ifa -count=1
cd go && go test -race ./internal/replay/concurrentreplay/... ./internal/ifa/graphdump/... ./cmd/ifa/... -count=1
bash scripts/test-verify-ifa-replay-drive.sh
bash scripts/verify-ifa-replay-drive.sh
bash scripts/verify-ifa-dead-letter-determinism.sh -mutation schema-major
bash scripts/verify-ifa-dead-letter-determinism.sh -mutation missing-field
ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main bash scripts/verify-performance-evidence.sh
```
