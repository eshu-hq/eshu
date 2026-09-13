# #6627 coordinator scanner-worker package move

## Scope

Base: `d5f4bdb6f260a8eb654b3e548454b3116b43aa1b`

This slice moves the scanner-worker planning package from the historical flat
path `internal/coordinator/scannerworker` to
`internal/coordinator/scanner/worker`. The package name becomes `worker`;
`PlanRequest`, `WorkPlanner`, and `PlanScannerWorkerWork` remain exact. All
three consumers retain the explicit `scannerworker` import alias so call sites
stay distinct from the collector runtime package.

The new `internal/coordinator/scanner` package is a documentation-only
namespace. It adds no runtime declaration, import, or initialization. The
`worker` leaf owns configuration validation, requested-scope privacy,
configured target order, and deterministic workflow-row construction.
Coordinator root still owns scheduling order and clocks, active and claims
gates, durable open-target admission, retries, queues, leases, and telemetry.

## Move map and dependency edges

The base leaf contained exactly six files, all moved without a forwarding
package:

- `scannerworker/AGENTS.md` -> `scanner/worker/AGENTS.md`
- `scannerworker/README.md` -> `scanner/worker/README.md`
- `scannerworker/doc.go` -> `scanner/worker/doc.go`
- `scannerworker/planner.go` -> `scanner/worker/planner.go`
- `scannerworker/planner_analyzer_contract_test.go` ->
  `scanner/worker/planner_analyzer_contract_test.go`
- `scannerworker/planner_contract_test.go` ->
  `scanner/worker/planner_contract_test.go`

The base leaf had 316 non-test Go lines and 491 test lines. The new namespace
parent contains exactly three direct files: `AGENTS.md`, `README.md`, and
declaration-free `doc.go` apart from its required package clause.

Before the move, the leaf had five direct internal production dependencies:
`internal/collector/scannerworker`, `internal/coordinator/planner/contract`,
`internal/facts`, `internal/scope`, and `internal/workflow`. The destination
has the same five and no external dependency. It imports neither coordinator
root nor storage, transport, query, reducer, graph-backend, or telemetry
packages.

The three reverse importers are unchanged after substituting the package path:

- `cmd/workflow-coordinator/main.go`
- `internal/coordinator/service_scanner_worker.go`
- `internal/coordinator/service_scanner_worker_test.go`

The command still wires `WorkPlanner`; root still names `PlanRequest` in its
structural planner interface and scheduling call; the root test still captures
the request. All three use the `scannerworker` alias. The moved-reference gate
and a live-reference sweep find no dangling use of the old package path outside
this historical move record and the approved mapping document.

This is a clearer in-process ownership seam, not an independently extractable
service. The direct collector scanner-worker analyzer and target-kind enum
contracts remain future extraction debt.

Coordinator root contained 49 direct non-test Go files before the move and
still contains 49. Its dirgate count and digest remain `49` and
`e39400aa04976497d8e4f169e8b8de10e3e1b16d40a5f9b9f31ff7f7da64377e`.
The root `service_scanner_worker.go` basename does not collide with the new
`scanner` child, so no dirgate suppression or ledger exemption is added.

## TDD evidence

The red phase happened before production moved. Only the two test files moved
to the destination and changed to `package worker`:

```text
go test ./internal/coordinator/scanner/worker -count=1
planner_analyzer_contract_test.go:73:23: undefined: WorkPlanner
planner_analyzer_contract_test.go:73:81: undefined: PlanRequest
planner_analyzer_contract_test.go:122:17: undefined: WorkPlanner
planner_analyzer_contract_test.go:122:75: undefined: PlanRequest
planner_contract_test.go:38:21: undefined: WorkPlanner
planner_contract_test.go:38:79: undefined: PlanRequest
planner_contract_test.go:108:21: undefined: WorkPlanner
planner_contract_test.go:108:79: undefined: PlanRequest
planner_contract_test.go:196:37: undefined: WorkPlanner
planner_contract_test.go:196:95: undefined: PlanRequest
FAIL github.com/eshu-hq/eshu/go/internal/coordinator/scanner/worker [build failed]
exit=1
```

Production then moved without a forwarding package. The following Go commands
ran from `go/`. The focused leaf, root, and command suite passed, followed by
the recursive coordinator suite:

```text
go test ./internal/coordinator/scanner/worker ./internal/coordinator \
  ./cmd/workflow-coordinator -count=1
exit=0

go test ./internal/coordinator/... ./cmd/workflow-coordinator -count=1
exit=0
```

`go test ./internal/coordinator/scanner/worker -list .` exits 0 and lists all
six destination tests. `go test -race` over the leaf, coordinator root, and
workflow-coordinator command also exits 0.

Whole-module compile proof passes:

```text
go build ./...
exit=0

go vet ./...
exit=0
```

The custom filelength and dirgate plugins build with the pinned toolchain. A
scoped `golangci-lint` run over the parent, leaf, coordinator root, and command
reports `0 issues` and exits 0. Dirgate evaluates the four touched package
directories and exits 0; telemetry coverage, Markdown line cap, strict MkDocs,
and diff check also exit 0. After the move was committed, the package-doc gate
confirmed all changed Go package docs are present, and the moved-reference gate
found four vacated Go paths with no dangling references. Both exit 0 against
the exact reviewed head.

All Go proof uses `GOTOOLCHAIN=go1.26.6`, `CC=clang`,
`CGO_CFLAGS=-std=gnu17`, and task-local `GOCACHE`, `TMPDIR`, and `GOTMPDIR`
directories outside the repository.

## Preserved contracts

- Collector instance, kind, enabled, claims-enabled, observation-time,
  plan-key, analyzer, target-kind, target-path, and duplicate-scope validation
  are unchanged.
- Configured work-item order, sorted requested-scope metadata, run and
  work-item identities, generation IDs, trigger selection, fairness keys, and
  timestamps are unchanged.
- Requested-scope metadata still contains only collector instance ID, analyzer,
  scope IDs, and target kinds; runtime-local root, rootfs, and layer paths
  remain excluded.
- Analyzer-specific empty and required-path behavior remains unchanged.
- Normalized comparisons of `planner.go` and both test files, substituting only
  the package declaration, are empty. `doc.go` changes only the package clause
  and the package name in its godoc sentence.
- No wire, API, fact, reducer, query, graph, storage, queue, lease,
  concurrency, or runtime behavior changes.

## Golden and protected-path evidence

The base and working tree have the same B-7 cassette tree
`16f9f804dd5f303ea600ad986c75f278317c264b` and B-12 snapshot blob
`e28983e5ad44eb23de8751ed97dfc7d6eb99e472`. The B-12 SHA-256 remains
`894b8e51905461c8f328a52e5b1c789d89a54c7c24bb14b14785d62ae357fe63`.
The working-tree diff against `internal/query` and `internal/reducer` is empty.

NornicDB v1.3.1 is the target backend release, but it is outside this package
move's proof surface: the planner has no graph-backend dependency, and this PR
makes no backend-version compatibility claim. Version alignment and live
validation remain separately owned by #6657.

No-Regression Evidence: the normalized production source and direct planner
tests are unchanged. Focused leaf, coordinator root, command wiring, recursive
coordinator, test-list, and race proof exercise the same planner contracts and
consumer seam at the destination path.

No-Performance-Change: normalized production source is unchanged. No
algorithm, I/O, allocation, lock, query, queue, or concurrency path changed,
so there is no performance theory or benchmark claim.

No-Concurrency-Change: planning remains synchronous and in process. Root keeps
durable admission, retry, queue, and lease behavior; the leaf introduces no
goroutine, channel, mutex, transaction, worker, or shared state.

No-Observability-Change: the planner emitted no signal before the move and
emits none after it. Root scheduling and durable admission remain covered by
`eshu_dp_workflow_coordinator_reconcile_total`,
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`, workflow rows and
claim status, and existing duplicate/admission logs. No metric, label, span,
log field, status surface, or telemetry ownership changes.
