# Capability Conformance Spec

Capability conformance is the product contract for what Eshu may claim in each
runtime profile. It keeps local, full-stack, and production query behavior from
drifting into undocumented differences.

The canonical machine-readable contract is:

- `specs/capability-matrix.v1.yaml`

The Go runtime mirrors that contract in `go/internal/query/contract.go`.
`go/internal/query/contract_matrix_test.go` proves the Go truth ceilings stay
aligned with the YAML.

## What The Matrix Controls

Each matrix row answers these questions:

| Question | Matrix field |
| --- | --- |
| What user-facing capability is this? | `capability` |
| Which MCP or API tool exposes it? | `tools` |
| Which runtime profile is being evaluated? | `profiles.<profile_id>` |
| Is it supported in that profile? | `status` |
| What is the strongest allowed truth label? | `max_truth_level` |
| What runtime shape is required? | `required_runtime` |
| What latency budget applies? | `p95_latency_ms` |
| What scope size is allowed? | `max_scope_size` |
| What proves the row? | `verification` |
| What caveat should reviewers know? | `notes` |

Do not copy the full capability list into prose. Read
`specs/capability-matrix.v1.yaml` for the current set.

## Runtime Profiles

The current profile IDs are:

| Profile | Meaning |
| --- | --- |
| `local_lightweight` | Local host mode without authoritative graph-backed capabilities. |
| `local_authoritative` | Local host plus managed graph backend, normally embedded NornicDB. |
| `local_full_stack` | Docker Compose or equivalent full local service stack. |
| `production` | Deployed services. |

The profile is separate from the graph backend. A profile says how the runtime
is operated; the backend says which graph adapter served a graph-backed answer.

## Runtime Execution Modes

`required_runtime` is the runtime-shape requirement for one profile row.

Allowed values:

- `local_host`
- `local_host_plus_graph`
- `full_stack`
- `deployed_services`

If a capability requires `local_host_plus_graph`, `local_lightweight` must not
fake it with fallback prose. It must return the structured unsupported
capability error when the matrix marks the row unsupported.

## Truth Levels

Successful responses use the truth labels from
[Truth Label Protocol](truth-label-protocol.md):

| Matrix value | Runtime behavior |
| --- | --- |
| `exact` | Response may claim authoritative graph truth or durable semantic truth. |
| `derived` | Response may claim deterministic derived truth from indexed content or structured relational state. |
| `fallback` | Response may claim exploratory fallback truth only when the matrix allows it. |
| `unsupported` | Runtime returns a structured error instead of a success truth label. |

Rules:

- A successful response must not exceed `max_truth_level`.
- Unsupported rows map to `error.code=unsupported_capability`.
- Graph-backed high-authority questions must not silently downgrade to
  fallback in a profile that cannot answer them correctly.
- `BuildTruthEnvelope` panics on unknown capability IDs, so new runtime
  capability strings must be registered in `capabilityMatrix`.

## Status Values

Allowed `status` values:

- `supported`
- `unsupported`
- `experimental`

`experimental` rows must not be described as production-ready in docs or user
interfaces.

## Scope Sizes

The matrix uses scope-size labels to keep broad tools bounded.

Common values include:

- `active_repo`
- `active_monofolder`
- `indexed_workspace`
- `multi_repo_platform`
- capability-specific bounded scopes such as `bounded_query_window`,
  `bounded_repo_scope`, or `aws_account_or_scope`

When adding a new scope label, update the YAML, the relevant runtime tests, and
the user-facing tool documentation that explains the bound.

## Verification Types

Allowed verification keys:

- `go_test`
- `integration_test`
- `compose_e2e`
- `remote_validation`

The value must identify an actionable test, proof name, or validation target.
Do not leave a supported or experimental row without verification.

## Graph Backends

The capability matrix lists official graph backend IDs under `graph_backends`.
Current values are:

- `nornicdb`
- `neo4j`

Backend conformance is a related but separate contract:

- `specs/backend-conformance.v1.yaml`
- [Backend Conformance](backend-conformance.md)
- `go/internal/backendconformance/`

The capability matrix says what user-facing behavior may be claimed in each
runtime profile. The backend matrix says which graph-adapter behavior classes
each official backend currently supports.

The backend matrix tracks:

- canonical writes
- direct graph reads
- bounded path traversal
- graph-native full-text support
- dead-code readiness
- performance envelope evidence

Both official backends must stay listed in both matrices while they remain
official Eshu graph backends. `ParseCapabilityMatrixBackendIDs` and the backend
conformance tests prevent the backend list from drifting.

## Runtime Enforcement

Runtime query code enforces the matrix through:

- `QueryProfile` in `go/internal/query/contract.go`
- `GraphBackend` in `go/internal/query/contract.go`
- `capabilityMatrix` in `go/internal/query/contract.go` and companion
  `contract_*.go` files
- `BuildTruthEnvelope`
- `capabilityUnsupported`
- `requiredProfile`
- structured `unsupported_capability` errors

The test `TestCapabilityMatrixMatchesYAMLContract` compares
`specs/capability-matrix.v1.yaml` with the Go `capabilityMatrix` truth ceilings.

## Change Policy

Changing the capability matrix is a product contract change. Every matrix edit
must include:

- the updated machine-readable YAML
- matching Go `capabilityMatrix` updates when runtime truth ceilings changed
- user-facing docs when behavior, profile requirements, or tool support changed
- verification updates for affected profile/capability rows
- focused tests for new or changed unsupported-capability behavior

Lifecycle rules:

| Change | Requirement |
| --- | --- |
| Add | Introduce the capability ID, map its tools, define every profile row, update Go truth ceilings, and add verification. |
| Deprecate | Keep the ID in the matrix with a deprecation note for at least one release. |
| Remove | Remove only after the deprecation window and after client-facing docs have been updated. |

## Validation

Run these focused checks after changing capability or backend conformance:

```bash
cd go
go test ./internal/query -run TestCapabilityMatrixMatchesYAMLContract -count=1
go test ./internal/backendconformance -count=1
go run ./cmd/eshu docs verify ../docs/public/reference/capability-conformance-spec.md --limit 1200 --fail-on contradicted,missing_evidence
```

Use the live backend conformance script only when changing backend behavior or
backend matrix evidence:

```bash
ESHU_GRAPH_BACKEND=nornicdb ./scripts/verify_backend_conformance_live.sh
ESHU_GRAPH_BACKEND=neo4j ./scripts/verify_backend_conformance_live.sh
```

## Related Docs

- [Truth Label Protocol](truth-label-protocol.md)
- [Backend Conformance](backend-conformance.md)
- [Graph Backend Operations](graph-backend-operations.md)
- [NornicDB Tuning](nornicdb-tuning.md)
- [MCP Tool Contract Matrix](mcp-tool-contract-matrix.md)
- `specs/README.md`
