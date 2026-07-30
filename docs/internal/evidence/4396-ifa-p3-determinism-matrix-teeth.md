# Ifá P3 Determinism-Matrix Teeth Evidence: ifadeterminismteeth Build Tag (#4396)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Ifá P3 determinism-matrix teeth: `ifadeterminismteeth` build tag (#4396)

No-Regression Evidence: `gcpCloudResourceNodeRow` (gcp_resource_materialization.go)
now calls `ifaTeethStampCloudResourceRow(row)` unconditionally before returning
every GCP `CloudResource` row. In every normal, CI, and production build
(build tag `!ifadeterminismteeth`, `gcp_resource_materialization_teeth_off.go`)
that call is a compiled-in no-op — it neither mutates `row` nor allocates — so
`ExtractGCPCloudResourceNodeRows`'s output, `WriteCloudResourceNodes`'s Cypher
parameters, and the committed graph are byte-identical to before this change.
Confirmed with the package's existing table-driven tests
(`go test ./internal/reducer -run TestExtractGCPCloudResourceNodeRows -count=1`,
`go test ./internal/storage/cypher -run TestCloudResourceNodeWriter -count=1`)
and `TestCanonicalCloudResourceUpsertCypherExcludesTeethClauseByDefault`
(go/internal/storage/cypher), which fails the build if either teeth clause
ever leaks into the default Cypher constant.

Only `go build -tags ifadeterminismteeth ./cmd/reducer` links the real stamp
(`gcp_resource_materialization_teeth.go`), which now writes TWO properties per
row, persisted by matching build-tag-gated Cypher SET clauses in
`go/internal/storage/cypher/cloud_resource_node_writer_teeth.go`:

- `ifa_teeth_seq` — a process-global monotonic sequence number
  (`ifaTeethSequenceCounter`), incremented once per `CloudResource` row this
  reducer process builds. This is the counter issue #4396 slice 5 originally
  shipped, then found MEASURABLY INERT on the single-scope demo-org cassette
  alone: `concurrentreplay.Driver` has exactly one work unit for ANY worker
  count on that fixture, so N never changes commit order, and all three
  N=1/N=2/N=4 cells produced the identical canonical digest
  (`48f30267f1c0773d137d14c64ae008e7fe0a5a39db481f524ac07d8ddcb09310`) with
  the counter alone. Slice 6b fixes the ROOT cause the inertness exposed —
  not the counter: `scripts/verify-ifa-determinism.sh` now also drives a
  synthetic multi-scope cassette
  (`go/internal/synth/gcp.GenerateMultiScope` via `ifa synth-cassette`, K
  disjoint GCP project scopes) into the same cell alongside the unmodified
  demo-org cassette, giving `concurrentreplay.Driver` K+1 genuinely
  independent work units. With more than one work unit, `-workers N` actually
  varies commit interleaving into `fact_work_items`, and this reducer's own
  worker pool (`ESHU_REDUCER_WORKERS`) races to claim and process those rows
  in an order that also varies — so `ifa_teeth_seq` is reintroduced as a real
  interleaving-sensitive signal, not an inert one.
- `ifa_teeth_write_order` — wall-clock nanoseconds (`time.Now().UnixNano()`),
  stamped unconditionally alongside the counter. This stays wired as the
  fault's guaranteed-red FLOOR: even if a future fixture change made the
  counter inert again, two independent process invocations reading wall-clock
  nanoseconds are vanishingly unlikely to collide, so `--teeth` can never
  flake green for lack of a divergent signal.

This is issue #4396's deliberately non-idempotent write: at least one of the
two stamped values depends on this reducer process's own commit/processing
order or its own process start time, so `scripts/verify-ifa-determinism.sh
--teeth` sees the digest diverge across N=1/N=2/N=4 and fails on purpose,
proving the graph-determinism matrix catches a real non-idempotent write
instead of passing vacuously. Per Serialization-Is-Not-A-Fix, the correct
response to that (or to any real) divergence is never to lower N, retry, or
serialize workers.

**Recorded slice 6b proof (2026-07-09):** `bash
scripts/verify-ifa-determinism.sh --teeth --keep` against the multi-scope
cassette pair (demo-org + `ifa synth-cassette -seed 4396 -projects 8
-resources 64`, 9 work units) produced three DIFFERENT canonical digests —
N=1 `96a68dfc3bfa89c8a59793cf2a51ec66e0265245f1c90128d1fbb3dbebe4ae5a`, N=2
`0dacc0881163611f067e110cd38ba7d9dfd0a576093a58a0d4ebda14d75932e5`, N=4
`af2f494f6676d0d6734c741020e6b08c393f0b440ea0e3bbd2ad45350681230b` — and the
script's own `TEETH: CAUGHT` acceptance branch fired. Correlating each
`CloudResource` node's stable `resource_id` across the three kept canonical
dumps confirms `ifa_teeth_seq` itself (not only the wall-clock floor)
diverges by interleaving: 591/622 nodes differ between N=1 and N=2, 558/622
between N=1 and N=4, 488/622 between N=2 and N=4 — e.g. the same
`//compute.googleapis.com/projects/acme-demo-gcp-00/computeName/synth-compute-30`
resource got `ifa_teeth_seq=340` at N=1 vs. `ifa_teeth_seq=57` at N=2.
`ifa_teeth_write_order` differed for all 622/622 nodes on every pair, as
expected of the guaranteed-red floor. This is the direct proof that
reintroducing the counter alongside the multi-scope fixture actually restored
its interleaving sensitivity — see `go/internal/ifa/graphdump/README.md`'s
"Multi-scope matrix (slice 6b)" entry for the full recorded run including the
matching NORMAL (non-`--teeth`) green matrix over the same 9-work-unit
fixture.

No-Observability-Change: the no-op path in every normal build adds no new
metric, log, or span, and changes no existing one. The `ifadeterminismteeth`
build only ever runs inside `scripts/verify-ifa-determinism.sh --teeth`
against a scratch Compose stack that is torn down at the end of that run; it
is not a runtime mode any deployed binary can reach.
