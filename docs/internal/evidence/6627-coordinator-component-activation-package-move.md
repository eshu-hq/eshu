# #6627 coordinator component-activation package move

## Scope

Base: `a0b5ca04cd15e537041cb0ef3f1b6921235469d7`

This slice moves the dependency-neutral activation configuration contract from
the historical flat path `internal/coordinator/componentactivation` to
`internal/coordinator/component/activation`. The leaf package name changes to
`activation`; the exported identifiers `ConfigSchema`, `Config`,
`RuntimeConfig`, and `ParseConfig` remain exact.

The new `internal/coordinator/component` package is a documentation-only
namespace. It adds no imports, declarations, initialization, or runtime
ownership. Coordinator root still owns component registry readback,
scheduling, egress policy and audit, durable admission, retries, queues,
leases, and telemetry. The activation leaf still owns only the shared JSON
shape and validation rules.

## Rebased inventory and dependency edges

At the base, the leaf contained exactly five files: `AGENTS.md`, `README.md`,
`config.go`, `config_test.go`, and `doc.go`. The destination contains those
same five file names; only the Go package name and documentation references
change with the path. The new namespace parent contains its required trio:
`AGENTS.md`, `README.md`, and declaration-only `doc.go`.

Coordinator root contained 49 direct non-test Go files before the move and
still contains 49. Its dirgate count and digest remain `49` and
`e39400aa04976497d8e4f169e8b8de10e3e1b16d40a5f9b9f31ff7f7da64377e`.
Adding the `component` namespace makes two root-owned files collide with the
new child basename. Their package clauses carry narrow file-local dirgate
justifications: registry readback and collector-instance construction remain
in `component_activation_config.go`; scheduling, policy, audit, and durable
admission remain in `component_extension_service.go`. The dirgate source
ledger and generated Go map do not change; regeneration is byte-identical.

Before and after path substitution, the leaf's sole internal production import
is `internal/component`; its test imports only `testing`. The destination
imports neither coordinator root nor a coordinator sibling, storage,
transport, workflow, or telemetry implementation.

The six reverse importers are unchanged after substituting the package path:

- `internal/coordinator/component_activation_config.go`
- `internal/coordinator/component_activation_config_test.go`
- `internal/coordinator/component_extension_service.go`
- `internal/coordinator/governance_audit.go`
- `internal/coordinator/pagerduty_service.go`
- `internal/coordinator/planner/component/extension/planner.go`

The five production consumers retain configuration construction, scheduling
eligibility and policy, governance audit identity, PagerDuty exclusion, and
extension planning responsibilities. `component_activation_config_test.go` is
the sole test importer. No forwarding package or duplicate contract type
remains.

## TDD evidence

The genuine red phase happened before production moved. Only
`config_test.go` was moved to the destination and changed to
`package activation`. With the ambient `go1.27.1 linux/amd64` toolchain,

```text
go test ./internal/coordinator/component/activation -count=1
internal/coordinator/component/activation/config_test.go:96:23: undefined: ParseConfig
FAIL github.com/eshu-hq/eshu/go/internal/coordinator/component/activation [build failed]
exit_code=1
```

Production then moved without a forwarding package. A later Go 1.26.6
sensitivity check temporarily withheld `config.go` after implementation and
reproduced the same `undefined: ParseConfig` compile failure with exit 1; that
check confirms test sensitivity but is not presented as the chronological red
phase.

The destination `go test -list .` enumerates `TestParseConfig`. The extension
planner enumerates its four contract tests, and coordinator root enumerates
the component/activation construction, scheduling, policy, and idempotency
tests selected by `Component|Activation`.

## Preserved contracts

- The activation schema value, JSON field names, required-field validation,
  unrelated-configuration miss behavior, host normalization, SDK protocol,
  adapter validation, and returned errors are unchanged.
- All six consumers were repointed in one working-tree change.
- A normalized diff of the base and destination `config.go`, substituting only
  `package componentactivation` with `package activation`, is empty.
- The sole caller-side adjustment beyond import paths and qualifiers renames a
  local `activation` parameter to `registeredActivation` so it cannot shadow
  the new package name. Its type, values, and uses are unchanged.
- No route, tool, wire/API, fact, reducer, query, queue, graph, storage,
  concurrency, or runtime behavior changes.

## Verification

All authoritative Go commands below ran serially under
`GOTOOLCHAIN=go1.26.6`, `CC=clang`, `CGO_CFLAGS=-std=gnu17`, an isolated
worktree-local `GOCACHE`, and a short `GOTMPDIR` outside every worktree:

- `go test -list` for the activation leaf, extension planner, and relevant
  coordinator root tests: exit 0; the intended tests enumerated.
- `go test ./internal/coordinator/component/activation ./internal/coordinator/planner/component/extension ./internal/coordinator -count=1`:
  exit 0.
- The same three-package command with `-race`: exit 0.
- `go build ./...`: exit 0.
- `go vet ./...`: exit 0.
- Repository lint over the four affected packages: four packages selected,
  zero issues, exit 0.
- Both custom golangci-lint plugins built successfully in this worktree.
- `go doc ./internal/coordinator/component` and
  `go doc ./internal/coordinator/component/activation`: exit 0.

The base and working tree have the same B-7 cassette tree
`16f9f804dd5f303ea600ad986c75f278317c264b` and B-12 snapshot blob
`e28983e5ad44eb23de8751ed97dfc7d6eb99e472`; the B-12 SHA-256 remains
`894b8e51905461c8f328a52e5b1c789d89a54c7c24bb14b14785d62ae357fe63`.
The working-tree diff against both golden surfaces is empty.

No-Performance-Change: this is a package path/name move plus documentation
and file-local lint ownership comments. No executable algorithm, I/O,
allocation, lock, query, queue, or concurrency path changes, so there is no
performance theory or benchmark claim.

No-Observability-Change: the activation leaf emits no signal before or after
the move. Root scheduling and policy remain covered by the existing workflow
coordinator reconcile metrics, workflow row and claim status, scheduling-skip
log, and governance audit events. No metric, label, span, log field, status
surface, or telemetry ownership changes; only documentation names the moved
contract.
