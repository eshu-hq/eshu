# internal/reducer/code/value/cleanup

## Purpose

Removes reducer-owned value-flow evidence from older generations beside the
normal reducer intent loop (issue #6061).

`Runner.RunOnce` claims a partition lease (when a `LeaseManager` is wired),
lists a bounded page of active repository-scope generations through
`CurrentGenerationReader`, and for each one retracts stale taint and interproc
evidence for every OLDER generation. When a projected-node/edge ledger and its
writer are both wired, the sweep lists the stale UIDs from the ledger and
issues an anchored by-UIDs delete plus a ledger prune; otherwise it falls back
to the plain scope/generation-scoped retractor ports.

## Ownership boundary

**Owns:** the bounded stale-evidence cleanup cycle (`Runner`), its
cursor-paging cycle (`Run`), and the port interfaces it depends on
(`CurrentGenerationReader`, `TaintStaleEvidenceRetractor`,
`InterprocStaleEvidenceRetractor`).

**Does not own:** the taint/interproc evidence writer and ledger ports
(`code/taint`), the partition lease manager (`sharedintent`), or the
`Service.startSideRunners` wiring that starts this runner as a side runner
(reducer root).

## Exported surface

| symbol | what it is |
|---|---|
| `Runner` / `RunnerConfig` | the side-runner cycle and its tunables |
| `Result` | one cleanup cycle's outcome |
| `CurrentGeneration` / `CurrentGenerationReader` | one active scope/generation, and the port that lists a bounded page of them |
| `TaintStaleEvidenceRetractor` / `InterprocStaleEvidenceRetractor` | the plain (non-ledger) stale-evidence retract ports |
| `ErrCurrentGenerationsRequired` | the validation error when `CurrentGenerations` is unwired |

The reducer root wires `Runner` on `Service.CodeValueFlowStaleCleanupRunner`,
keeping the `reducer.CodeValueFlowStaleCleanupRunner`/
`reducer.CodeValueFlowStaleCleanupRunnerConfig`/
`reducer.CodeValueFlowCurrentGeneration`/
`reducer.TaintStaleEvidenceRetractor`/
`reducer.InterprocStaleEvidenceRetractor` spellings through the
value-flow stanza of `compat_projection.go`, since cmd/reducer's wiring and
internal/storage/postgres' generation reader both still name them that way.

## Dependencies

`internal/reducer/code/taint` (`EvidenceWriter`,
`ProjectedNodeLedger`, `InterprocEvidenceWriter`,
`InterprocProjectedEdgeLedger`, `EvidenceSource`,
`InterprocEvidenceSource`), `internal/reducer/sharedintent`
(`PartitionLeaseManager`), and `internal/telemetry`
(`PhaseAttr`/`FailureClassAttr`). No dependency on the reducer root.

## Telemetry

`Logger` (when wired) emits two structured logs, "code value-flow stale
cleanup cycle completed" (with `lease_acquired`, `scopes_scanned`,
`scopes_skipped`, `taint_sweeps`, `interproc_sweeps`, `cursor_exhausted`,
`duration_seconds`) and "code value-flow stale cleanup cycle failed" (with
the error and `code_value_flow_stale_cleanup_error` failure class). Both
carry `telemetry.PhaseReduction`.

## Gotchas / invariants

- **The ledger-driven path takes priority over the plain retractor.** When
  both `TaintLedger`+`TaintWriter` (or `InterprocLedger`+`InterprocWriter`)
  are wired, `RunOnce` uses the anchored by-UIDs delete and ledger prune
  instead of `TaintEvidence`/`InterprocEvidence`; `validate` only requires
  one path per evidence family to be wired.
- **`leaseDomain`/`leasePartitionID`/`leasePartitionCount` are the runner's
  own single-partition lease identity** (`"code_value_flow_stale_cleanup"`,
  partition 0 of 1) — do not reuse this domain string for another side
  runner.
- **The cursor restarts from the first page once the last page is
  shorter than `ScopeBatchLimit`.** A caller relying on exhaustive coverage
  across restarts must account for this wraparound.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/code/README.md` — the `code/` namespace parent
- `docs/internal/design/reducer-target-tree.md` — the #6061 restructure

No-Regression Evidence: #6061 moves the value-flow stale-cleanup side runner
out of the reducer root into this new package, without changing any field,
exported behavior, wire string, or call order.
`CodeValueFlowStaleCleanupRunner`/`CodeValueFlowStaleCleanupRunnerConfig`/
`CodeValueFlowStaleCleanupResult`/`CodeValueFlowCurrentGeneration`/
`CodeValueFlowCurrentGenerationReader`/
`ErrCodeValueFlowCurrentGenerationsRequired` dropped the
`CodeValueFlow(StaleCleanup)` stutter per `docs/internal/naming.md`
(`CodeValueFlowStaleCleanupRunner` -> `Runner`,
`CodeValueFlowStaleCleanupRunnerConfig` -> `RunnerConfig`,
`CodeValueFlowStaleCleanupResult` -> `Result`, `CodeValueFlowCurrentGeneration`
-> `CurrentGeneration`, `CodeValueFlowCurrentGenerationReader` ->
`CurrentGenerationReader`, `ErrCodeValueFlowCurrentGenerationsRequired` ->
`ErrCurrentGenerationsRequired`); the reducer root keeps every spelling with
an external caller (`Runner`, `RunnerConfig`, `CurrentGeneration`,
`TaintStaleEvidenceRetractor`, `InterprocStaleEvidenceRetractor`)
through the value-flow stanza of `compat_projection.go`, so no external
caller needed a source change. Measured from `go/`, with `GOROOT` unset: `go
build ./...`, `go vet ./internal/reducer/... ./cmd/reducer/...`, and `go test
./internal/reducer/... ./cmd/reducer/... ./internal/storage/postgres/...
./internal/replay/... -count=1` all exited 0 on this branch. `git diff
--check` exited 0.

No-Observability-Change: this move adds no queue domain, worker, lease,
graph, or Postgres operation, runtime setting, metric instrument, metric
label, span, or status surface. The two structured-log messages and their
fields listed under Telemetry above are unchanged; only the package that owns
the code moved.
