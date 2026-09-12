# #6627 coordinator Vault live package move

## Scope

Base: `5e33d3d205a96d66242df97471918df20ddbf57a`

This slice moves the Vault metadata planning package from the historical flat
path `internal/coordinator/vaultlive` to
`internal/coordinator/vault/live`. The package name becomes `live`;
`PlanRequest`, `WorkPlanner`, and `PlanVaultLiveWork` remain exact. All three
consumers retain the `coordinatorvaultlive` import alias.

The new `internal/coordinator/vault` package is a documentation-only namespace.
It adds no runtime declaration, import, or initialization. The `live` leaf
owns request validation, target parsing, requested-scope composition, and
deterministic workflow-row construction. It is an in-process ownership seam,
not an independently deployable or extractable service: repository-local
collector, workflow, fact, scope, and plan-key contracts remain dependencies.
Coordinator root still owns scheduling order and clocks, active and claims
gates, durable open-target admission, retries, queues, leases, and telemetry.

## Move map and dependency edges

The base leaf contained exactly six files, all moved without a forwarding
package:

- `vaultlive/AGENTS.md` -> `vault/live/AGENTS.md`
- `vaultlive/README.md` -> `vault/live/README.md`
- `vaultlive/doc.go` -> `vault/live/doc.go`
- `vaultlive/planner.go` -> `vault/live/planner.go`
- `vaultlive/planner_contract_test.go` ->
  `vault/live/planner_contract_test.go`
- `vaultlive/planner_validation_test.go` ->
  `vault/live/planner_validation_test.go`

The new namespace parent contains exactly three direct files: `AGENTS.md`,
`README.md`, and declaration-free `doc.go` apart from its required package
clause.

Before the move, the leaf had five internal production dependencies:
`internal/collector/vaultlive`, `internal/coordinator/planner/contract`,
`internal/facts`, `internal/scope`, and `internal/workflow`. The destination has
the same five dependencies and imports neither coordinator root nor storage,
transport, query, reducer, graph backend, or telemetry implementations.

The three reverse importers are unchanged after substituting the package path:

- `cmd/workflow-coordinator/main.go`
- `internal/coordinator/vault_live_service.go`
- `internal/coordinator/service_vault_live_test.go`

The command still wires `WorkPlanner`; root still names `PlanRequest` in its
structural planner interface and scheduling call; the root test still captures
the request. Outside this historical move record, a repository-wide old-path
scan returns no matches.

Coordinator root contained 49 direct non-test Go files before the move and
still contains 49. Its dirgate count and digest remain `49` and
`e39400aa04976497d8e4f169e8b8de10e3e1b16d40a5f9b9f31ff7f7da64377e`.
Adding the `vault` namespace makes `vault_live_service.go` collide with the new
child basename. The changed-path dirgate first failed on that exact file, then
passed after a narrow package-clause rationale recorded that Vault scheduling
and durable admission remain root `Service` ownership. No dirgate ledger or
generated map changes.

## TDD evidence

The red phase happened before production moved. Only the two test files moved
to the destination and changed to `package live`:

```text
GOTOOLCHAIN=go1.26.6 CC=clang CGO_CFLAGS=-std=gnu17 \
  go test ./internal/coordinator/vault/live -count=1
internal/coordinator/vault/live/planner_contract_test.go:23:22: undefined: WorkPlanner
internal/coordinator/vault/live/planner_contract_test.go:148:24: undefined: PlanRequest
internal/coordinator/vault/live/planner_validation_test.go:19:16: undefined: PlanRequest
FAIL github.com/eshu-hq/eshu/go/internal/coordinator/vault/live [build failed]
exit=1
```

Production then moved without a forwarding package. With the same toolchain
and a task-scoped `GOCACHE`, `TMPDIR`, and `GOTMPDIR`, the focused suite passed:

```text
go test ./internal/coordinator/vault/live ./internal/coordinator/... \
  ./cmd/workflow-coordinator -count=1
exit=0
```

The first focused attempt used the shared temporary directory and failed only
while linking the unrelated AWS freshness and scheduled planner tests with
`mapping output file failed: disk quota exceeded`; the moved leaf, coordinator
root, and workflow-coordinator package were already green in that attempt. The
accepted rerun above used the home-backed task cache and passed every package.

`go test ./internal/coordinator/vault/live -list .` exits 0 and lists all six
destination tests. The same leaf, coordinator-root, and workflow-coordinator
scope under `-race` exits 0.

## Preserved contracts

- Collector-instance, collector-kind, enabled, claims-enabled, observation
  time, plan-key, configuration, and duplicate-target validation are unchanged.
- Configured work-item order, sorted requested-scope metadata, run and
  work-item identities, bootstrap trigger selection, fairness keys, and
  timestamps are unchanged.
- Duplicate target errors still omit raw cluster and namespace identities.
- Durable rows still omit Vault addresses, token environment names, cluster
  IDs, and namespaces. Target keys retain their unambiguous NUL separator.
- A normalized comparison of base and destination `planner.go`, substituting
  only the package declaration, is empty (`diff` exit 0).
- No wire, API, fact, reducer, query, graph, storage, queue, lease,
  concurrency, or runtime behavior changes.

## Golden and protected-path evidence

The base and working tree have the same B-7 cassette tree
`16f9f804dd5f303ea600ad986c75f278317c264b` and B-12 snapshot blob
`e28983e5ad44eb23de8751ed97dfc7d6eb99e472`. The B-12 SHA-256 remains
`894b8e51905461c8f328a52e5b1c789d89a54c7c24bb14b14785d62ae357fe63`.
The working-tree diff against `internal/query` and `internal/reducer` is empty.

No-Performance-Change: this is a package path and name move plus documentation
and a file-local lint ownership comment. The normalized production source is
unchanged. No executable algorithm, I/O, allocation, lock, query, queue, or
concurrency path changes, so there is no performance theory or benchmark
claim.

No-Concurrency-Change: planning remains synchronous and in process. Root keeps
the durable admission, retry, queue, and lease behavior; the leaf introduces no
goroutine, channel, mutex, transaction, worker, or shared state.

No-Observability-Change: the planner emitted no signal before the move and
emits none after it. Root scheduling and durable admission remain covered by
`eshu_dp_workflow_coordinator_reconcile_total`,
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`, workflow rows and
claim status, and existing duplicate/admission logs. No metric, label, span,
log field, status surface, or telemetry ownership changes.
