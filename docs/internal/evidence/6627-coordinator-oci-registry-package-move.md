# #6627 coordinator OCI registry package move

## Scope

Base: `8a6a4035c9b9ca147e46aef8cad210fbb212b923`

This slice moves the OCI registry planning package from the historical flat
path `internal/coordinator/ociregistry` to
`internal/coordinator/oci/registry`. The package name becomes `registry`;
`PlanRequest`, `WorkPlanner`, and `PlanOCIRegistryWork` remain exact. All four
consumers retain the explicit `ociregistry` import alias so call sites stay
clear beside package-registry code.

The new `internal/coordinator/oci` package is a documentation-only namespace.
It adds no runtime declaration, import, or initialization. The `registry` leaf
owns request validation, configured-target normalization, duplicate rejection,
requested-scope construction, and deterministic workflow-row construction.
Coordinator root still owns scheduling order and clocks, active and claims
gates, durable open-target admission, retries, queues, leases, and telemetry.

## Move map and dependency edges

The base leaf contained exactly five files, all moved without a forwarding
package:

- `ociregistry/AGENTS.md` -> `oci/registry/AGENTS.md`
- `ociregistry/README.md` -> `oci/registry/README.md`
- `ociregistry/doc.go` -> `oci/registry/doc.go`
- `ociregistry/planner.go` -> `oci/registry/planner.go`
- `ociregistry/planner_test.go` -> `oci/registry/planner_test.go`

The new namespace parent contains exactly three direct files: `AGENTS.md`,
`README.md`, and declaration-free `doc.go` apart from its required package
clause.

Before the move, the leaf had twelve internal production dependencies. The
destination has the same twelve: `internal/collector/ociregistry`; its seven
`acr`, `dockerhub`, `ecr`, `gar`, `ghcr`, `harbor`, and `jfrog` provider
adapters; `internal/coordinator/planner/contract`; `internal/facts`;
`internal/scope`; and `internal/workflow`. It imports neither coordinator root
nor storage, transport, query, reducer, graph-backend, or telemetry packages.

The four reverse importers are unchanged after substituting the package path:

- `cmd/workflow-coordinator/main.go`
- `internal/coordinator/service.go`
- `internal/coordinator/oci_registry_service.go`
- `internal/coordinator/service_oci_registry_test.go`

The command still wires `WorkPlanner`; root still names `PlanRequest` in its
structural planner interface and scheduling call; the root test still captures
the request. A repository-wide scan finds no old coordinator package path.

These dependencies make the nested path a clearer ownership seam, not an
independently extractable service. The collector identity contract and seven
provider adapters remain future extraction work.

Coordinator root contained 49 direct non-test Go files before the move and
still contains 49. Its dirgate count and digest remain `49` and
`e39400aa04976497d8e4f169e8b8de10e3e1b16d40a5f9b9f31ff7f7da64377e`.
Adding the `oci` namespace makes `oci_registry_service.go` collide with the new
child basename. A narrow package-clause rationale records that OCI registry
scheduling and durable admission remain root `Service` ownership. No dirgate
ledger or generated map changes.

## TDD evidence

The red phase happened before production moved. Only `planner_test.go` moved to
the destination and changed to `package registry`:

```text
go test ./internal/coordinator/oci/registry -count=1
internal/coordinator/oci/registry/planner_test.go:45:21: undefined: WorkPlanner
internal/coordinator/oci/registry/planner_test.go:45:77: undefined: PlanRequest
FAIL github.com/eshu-hq/eshu/go/internal/coordinator/oci/registry [build failed]
exit=1
```

Production then moved without a forwarding package. The focused leaf, root,
and command suite passed, followed by the recursive coordinator suite:

```text
go test ./internal/coordinator/oci/registry ./internal/coordinator \
  ./cmd/workflow-coordinator -count=1
exit=0

go test ./internal/coordinator/... ./cmd/workflow-coordinator -count=1
exit=0
```

The recursive run used task-scoped `GOCACHE`, `TMPDIR`, and `GOTMPDIR` paths
under the home volume after the shared `/tmp` volume rejected an earlier link
with `disk quota exceeded`. `go test -race` over the leaf, coordinator root,
and workflow-coordinator command then passed with the same task-scoped paths.
`go test ./internal/coordinator/oci/registry -list .` exits 0 and lists all
five destination tests.

With `CC=clang` and `CGO_CFLAGS=-std=gnu17`, `go build ./...` and
`go vet ./...` both exit 0. The compiler settings avoid an unrelated host GCC
parse failure in the Perl tree-sitter C scanner.

## Preserved contracts

- Collector instance, kind, enabled, claims-enabled, observation-time,
  plan-key, configuration, provider, and duplicate-target validation are
  unchanged.
- All seven provider identity rules, including explicit ECR, GAR, and ACR host
  precedence and GHCR default-host normalization, are unchanged.
- Configured work-item order, sorted requested-scope metadata, run and
  work-item identities, generation IDs, trigger selection, fairness keys, and
  timestamps are unchanged.
- Requested-scope metadata still contains only `scope_id`, `provider`, and
  `repository`; credentials, base URLs, regions, registry IDs, and tag limits
  remain excluded.
- Blank, `{}`, and empty-target configurations remain errors. Duplicate
  normalized identities are rejected before any row is built.
- Normalized comparisons of base and destination `planner.go` and
  `planner_test.go`, substituting only the package declaration, are empty
  (`diff` exit 0).
- No wire, API, fact, reducer, query, graph, storage, queue, lease,
  concurrency, or runtime behavior changes.

## Golden and protected-path evidence

The base and working tree have the same B-7 cassette tree
`16f9f804dd5f303ea600ad986c75f278317c264b` and B-12 snapshot blob
`e28983e5ad44eb23de8751ed97dfc7d6eb99e472`. The B-12 SHA-256 remains
`894b8e51905461c8f328a52e5b1c789d89a54c7c24bb14b14785d62ae357fe63`.
The working-tree diff against `internal/query` and `internal/reducer` is empty.

No-Performance-Change: normalized production source is unchanged. No
algorithm, I/O, allocation, lock, query, queue, or concurrency path changed, so
there is no performance theory or benchmark claim.

No-Concurrency-Change: planning remains synchronous and in process. Root keeps
durable admission, retry, queue, and lease behavior; the leaf introduces no
goroutine, channel, mutex, transaction, worker, or shared state.

No-Observability-Change: the planner emitted no signal before the move and
emits none after it. Root scheduling and durable admission remain covered by
`eshu_dp_workflow_coordinator_reconcile_total`,
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`, workflow rows and
claim status, and existing duplicate/admission logs. No metric, label, span,
log field, status surface, or telemetry ownership changes.
