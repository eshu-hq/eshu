# Evidence: retire gpphase repair alias family (#6626 gpphase micro-batch)

## What changed

The last live stanza of `compat_correlation.go` (graph-projection phase
repair family, H5 root-remnant fold from issue #6061) is deleted once its
final callers moved. Five symbols, all `=` into their leaves:

- `GraphProjectionPhaseRepair` -> `gpphase.PhaseRepair`
- `GraphProjectionPhaseRepairQueue` -> `gpphase.PhaseRepairQueue`
- `GraphProjectionPhaseRepairsFromStates` -> `gpphase.PhaseRepairsFromStates`
- `GraphProjectionPhaseRepairerConfig` -> `repair.Config`
- `GraphProjectionPhaseRepairer` -> `repair.Repairer`

Pre-census on `origin/main d63ee0876f` (`git grep`, so no #7750 pruning):

- External `reducer.X` caller files: `cmd/reducer/config_projection.go`,
  `cmd/reducer/main_helpers.go`, `internal/projector/runtime/phase.go`,
  `internal/projector/runtime/projection.go` (+ `projection_test.go`),
  `internal/storage/postgres/graph_projection_phase_repair_queue.go`
  (+ `_test.go`). Union: 7 files, ~30 sites.
- In-root unqualified users: `defaults.go`, `service.go`,
  `graph_projection_phase_publish.go`,
  `workload_materialization_handler.go`,
  `workload_materialization_repo_phase.go`,
  `semantic_entity_repair_queue_adapter.go` (+ 6 root test files).
  `service_side_runners.go` and `cmd/reducer/main.go` touch only field
  names, unchanged.
- No other external `reducer.GraphProjectionPhaseRepair*` spellings (go,
  docs, or otherwise); no retirement-test pins the aliases.

Rewrite: qualifier swap `reducer.X` -> `gpphase.X` / `repair.X` over code
spans, `gpphase`/`repair` imports added where missing, now-unused `reducer`
imports dropped (`phase.go`, `projection.go`, postgres queue + test).
Hand work, all disclosed:

- `service.go` field keeps its name with a repointed type
  (`GraphProjectionPhaseRepairer *repair.Repairer`); `service_side_runners.go`
  and `cmd/reducer/main.go` field accesses unchanged.
- `semantic_entity_repair_queue_adapter.go` now converts
  `semantic.GraphProjectionPhaseRepair` rows to `gpphase.PhaseRepair`
  (field set identical; parity test repointed); the semantic-local port
  stays by design, still bridged, not consolidated.
- Test-local `repair :=` shadowing the new `repair` package import renamed
  to `row` (`graph_projection_phase_repair_test.go`); the parity-test
  `rootType` renamed `leafType` with `(gpphase)` messages.
- `code/semantic` port comments, `gpphase/doc.go`, `repair/doc.go`,
  reducer `README.md`/`AGENTS.md`, `projector/README.md`,
  `cmd/reducer/README.md` (backticked identifiers and the mermaid runner
  label), and the adapter row in `telemetry-coverage.md`
  repointed to the leaf spellings.
- New guard `TestGpphaseRepairRetired` in
  `compat_alias_retirement_gpphase_test.go` (bucket 3-9 pattern) scans the
  whole reducer package directory for any re-declared alias.
- Review round 2 (PR #8692): repointed the `repair/README.md` and
  `repair/AGENTS.md` alias claims, the `gpphase/README.md` repair names,
  the `cmd/reducer/README.md` mermaid runner label, and the
  `graph_ports.go` field-set comment the first pass missed.
- `compat_correlation.go` keeps the retired ledger (design-doc and
  telemetry rows name the file; precedent keeps ledgers in place).

## Why no regression

No-Regression Evidence: every alias is the identical canonical type or
function, so each repoint is spelling-only; no signature, envelope, row,
ordering, or string-value change (storage SQL and Cypher string values are
untouched — only Go identifiers).
Proof on this head: RED build on the deleted stanza
(`undefined: GraphProjectionPhaseRepairQueue`, `service.go:117`
`undefined: GraphProjectionPhaseRepairer`, plus the publish/repo-phase/
handler/adapter sites); GREEN `go build` exit 0 over
`internal/reducer/...`, `cmd/reducer/...`, `internal/projector/...`,
`internal/storage/postgres/...`; `go vet` exit 0 on the same trees;
gofmt clean over every touched file; `git diff --check` clean;
`go test -count=1` green for the full `internal/reducer` package plus
`gpphase`, `code/semantic`, `projector/runtime`, `cmd/reducer`, and the
3 postgres queue tests (named runs confirm each touched test actually
ran); `-race` green on reducer root, `contract`, and `gpphase`; guard
RED (seeded re-declaration fails) / GREEN (clean tree passes);
`ci-gates validate --drift` pass; `verify-telemetry-coverage.sh` pass
(registrations agree, no new untracked stages). Zero residual for all 5
names by `git grep` outside `code/semantic`'s intentional local port,
test names, and historical evidence records.

## Observability

No-Observability-Change: no new metric instrument, metric label, span,
structured log field, queue domain, worker, lease, or runtime knob. Phase
repair keeps reporting through the existing spans and counters (`reducer.run`,
`eshu_dp_reducer_executions_total`, `eshu_dp_reducer_run_duration_seconds`);
only the static type spelling at call sites changed.
