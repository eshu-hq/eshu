# goldengate — Agent Instructions

LLM-assistant companion to `README.md`. Read this before editing any file in
`go/internal/goldengate/`.

## Read first

- `README.md` — why this package exists and what it contains.
- `doc.go` — the godoc contract.
- `../../cmd/golden-corpus-gate/` — the in-repo gate that wires the live pipeline
  and feeds observed values to these functions (via `shared.go` aliases).
- `../../conformance/` — the out-of-tree contributor conformance suite that
  replays a cassette offline and feeds in-memory values to these *same*
  functions.

## Invariants

- **Stay I/O-free.** Every `Evaluate*` function turns an observed value plus the
  snapshot contract into a `Finding` with no database, graph, HTTP, filesystem,
  or network access. `LoadSnapshot` is the one exception (it reads the snapshot
  file) and is the only I/O allowed here. Keeping the rest pure is what lets both
  consumers unit-test the assertion rules without a backend.
- **One source of truth — no forks.** Both `cmd/golden-corpus-gate` and
  `go/conformance` import this package. Never copy an `Evaluate*` function or a
  snapshot type into a consumer; if a consumer needs new assertion behaviour, add
  it here so both inherit it. The whole point of the extraction (#4112 / R-10) is
  that the credential-free contributor proof and the in-repo gate cannot drift.
- **Required vs advisory is the safety boundary.** A required finding that fails
  makes `Report.Failed()` true; an advisory finding only warns. Do not silently
  downgrade a required assertion to advisory to make a gate green.
- **An empty `Report` is a failure.** `Report.Failed()` returns true when nothing
  ran — a gate that asserted nothing has proven nothing. Preserve this.
- **Edge properties are absence-zero; node properties are presence-positive.**
  See the doc comments on `EvaluateEdgeProperty` / `EvaluateNodeProperty` and
  `RequiredCorrelation` / `RequiredNode`. An edge missing its (evidence-narrowed)
  property is offending; a label may legitimately hold property-less nodes, so
  nodes assert a floor of carriers rather than absence-of-any-untagged. Do not
  swap these semantics.
- **Changing a snapshot struct is a contract change.** The JSON tags here mirror
  `testdata/golden/e2e-20repo-snapshot.json` and the contributor spec YAML
  (parsed via `sigs.k8s.io/yaml`, which honours the same `json` tags). A field
  rename or tag change must update both snapshot artifacts and their loaders in
  the same change.

## Tests

- `evaluate_test.go`, `report_test.go`, `property_test.go`, `snapshot_test.go`
  run in every default `go test ./...` pass — no backend, no network:
  `cd go && go test ./internal/goldengate/... -count=1`.
- Add a focused unit test for any new assertion rule here before the consumers
  wire it.

## Out of scope here

- Reading observed values from Postgres / Bolt / HTTP (that is the gate's
  `graph.go` / `drains.go` / `query.go` / `mcp.go`).
- Replaying cassettes and deriving in-memory counts (that is
  `go/conformance`).
