# Inheritance/Shell-Exec Fixture Move + Relative-Path Fix Evidence (#5996, #6001)

Two changes on this branch trip `verify-performance-evidence.sh`'s
content-based hot-path scan because they touch `go/internal/reducer/*.go`
and `go/internal/ifa/*.go`. Neither changes reducer throughput, graph write
shape, or worker/lease/queue behavior.

## #5996/#6001 — move the inheritance_edges/shell_exec vacuity-guard fixtures into go/internal/ifa/materializededges

Pure package relocation forced by the #6053/#6199 materializededges split:
`materialized_edges_inheritance.go`, `materialized_edges_inheritance_test.go`,
`materialized_edges_shell_exec.go`, `materialized_edges_shell_exec_test.go`,
and `shell_exec_family_odu_cassette_test.go` moved from `go/internal/ifa` to
`go/internal/ifa/materializededges` unchanged in logic -- only the package
clause, the `ifa.` qualification the package boundary now requires, and the
identifiers exported from `inheritance_family_odu.go`/`shell_exec_family_odu.go`
so the moved guard/tests can reach them without a second copy (13 new
exported constants/functions, all read-only fixture data or path-joining
helpers; see those two files' own doc comments for the export list and the
`materializededges/AGENTS.md` two-direction mutation-probe rule this
decision follows). No Cypher, extractor algorithm
(`reducer.ExtractInheritanceRows`/`reducer.ExtractShellExecRows`), worker
count, batch size, lease, or queue behavior changed. This is a compile-time
boundary move of pure fixture-construction and assertion code the live
gates never execute on a hot path.

No-Regression Evidence: `cd go && go build ./internal/ifa/...` (clean);
`go vet ./internal/ifa/...` (clean); `go test ./internal/ifa/... ./cmd/ifa
-count=1` (all packages ok, including `internal/ifa/materializededges`);
`go test ./internal/reducer -count=1` (ok); `gofmt -l` / `gofumpt -l` on
every touched file (no output -- already formatted); `bash
scripts/dev/precommit-go.sh lint <touched files>` (2 packages, 0 issues);
`bash scripts/dev/precommit-go.sh dirgate-all` (clean; `go/internal/ifa`
stays well under dirgate's 40-file cap after the split moved files out);
`bash scripts/verify-package-docs.sh` (present).

No-Observability-Change: no metric instrument, metric label, span, log key,
route, graph query shape, queue table, worker, lease, or runtime knob is
added, removed, or changed. The moved code runs only inside
`MaterializedEdgeOduResolver.Resolve` (an offline vacuity-guard/coverage
check invoked by `go test` and the golden-corpus replay-coverage gate, never
by the live reducer/projector/collector runtime) and inside `go test`
itself; no code path a production binary executes changed packages.

## #5996 — read relative_path in inheritance materialization, not path

`go/internal/reducer/inheritance_materialization.go` and its new
`inheritance_materialization_diagnostics.go` split fixed a field-name defect
(the same class #5998 fixed for rationale edges): every `content_entity`
read asked for a `"path"` key production content_entity facts never carry
(`git_content_fact_envelopes.go` emits `relative_path` only), so
`child_path` was always blank in the persisted intent payload and
`inheritanceFilePartitionKey`'s "file-scoped, stable across re-ingest"
property did not hold. No edge was dropped, misrouted, or mis-retracted:
`buildInheritanceDeltaScope` derives `delta_file_paths` from the repository
fact alone, the delta-retract Cypher matches the graph node's own `path`
property (written by a different materializer), and
`buildInheritanceRowMap`'s edge-write param map never included
`child_path` at all. The defect was a provenance/partition-key-stability
gap, not a correctness one, and the fix is a two-line read-key change plus
the new diagnostics split.

No-Regression Evidence: `cd go && go test ./internal/reducer -run
Inheritance -v -count=1` (33 cases, all PASS, including the new 375-line
`inheritance_relative_path_test.go` regression suite and
`TestInheritanceFilePartitionKeyChangesWithProductionChildPath`, which fails
red on the pre-fix `"path"` read and green on the fix); `go test
./internal/reducer -count=1` (ok, full package). The three sibling tests
that deliberately keep a `"path"` fixture key (their assertions never read
`child_path`) still pass, proving the fix is scoped to the actually-broken
read path.

No-Observability-Change: `inheritance_materialization_diagnostics.go` adds
no new metric, span, or log key -- it splits existing diagnostic helper
functions out of `inheritance_materialization.go` to keep that file under
the repository's 500-line cap. The handler's existing
`inheritance materialization started/fact inputs/completed` structured logs
and `load_facts_duration_seconds`/`build_intents_duration_seconds`/
`upsert_intents_duration_seconds`/`total_duration_seconds` fields are
unchanged in shape; only the `child_path` value they now correctly populate
changed, which is the fix itself, not a new observability surface.
