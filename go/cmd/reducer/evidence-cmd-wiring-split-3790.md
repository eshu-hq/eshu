# Evidence — cmd reducer/api wiring split (#3790)

Epic D / D-4 modularization. Records the performance and observability evidence
for splitting `cmd/reducer/main.go` and `cmd/api/wiring.go` by subsystem, so the
hot-path evidence gate (`scripts/verify-performance-evidence.sh`) has a tracked,
in-repo record specific to this change rather than relying on PR prose or
unrelated markers.

## Change shape

Pure same-package wiring extraction — no runtime behavior, wiring order, or
telemetry changed:

- `cmd/reducer/main.go` (504 → 386): the seven adapter-gated handler groups in the
  `reducer.DefaultHandlers` composite literal move into `build*Handlers` helpers in
  the sibling `wiring_handlers.go`. Each group's single-use wiring is constructed
  inside its builder.
- `cmd/api/wiring.go` (581 → 297): router assembly + version/deprecation middleware
  move into the sibling `wiring_router.go`. `wireAPI` / `mountRuntimeSurface` are
  untouched.

## No-Regression Evidence:

- **Baseline / after:** `buildReducerService` builds the same dependencies and the
  same `reducer.DefaultHandlers` value. Composite-literal fields are
  order-independent in Go, so assembling a field group via a builder call yields an
  identical struct value. Structural diff confirms all 221 `DefaultHandlers`
  struct-field assignments and all 9 `cmd/api` top-level declarations are present
  exactly once across the split files; no statement was edited, only moved.
- **Backend/version:** the reducer graph-write gate, worker-count formulas,
  semantic-write timeout, NornicDB grouped-write resolution, and queue
  configuration are byte-for-byte unchanged in `main.go`; no graph backend
  interaction, Cypher shape, batch size, lease, or worker knob changed.
- **Input shape / terminal counts:** the composition root wires the same adapters
  with the same constructor arguments, so queue claim/drain, projection, and graph
  write behavior are unchanged. The single-use wiring helpers
  (`multiCloudRuntimeDriftWiring`, `cloudInventoryAdmissionWiring`,
  `incidentRepositoryCorrelationWiring`, the function-store cluster, the value-flow
  fixpoint projector) are constructed with the same inputs, just relocated next to
  their only consumer; their construction order is independent (no shared mutable
  state).
- **Why safe:** `go build`, `go vet`, `go test ./cmd/reducer/... ./cmd/api/...
  -count=1`, and `golangci-lint run` (filelength plugin loaded) are all green; a
  pure wiring-extraction refactor cannot regress throughput or correctness because
  the composition root produces the same values.

## No-Observability-Change:

No spans, metrics, logs, or telemetry names were added, removed, or renamed. The
reducer and API binaries register the same instruments and emit the same
`eshu_dp_*` signals as before; the split only relocates adapter construction
between sibling files in the same `package main`.
