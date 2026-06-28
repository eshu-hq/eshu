# Capability Conformance Spec

Capability conformance defines what Eshu may claim in each runtime profile.
The machine-readable source of truth is `specs/capability-matrix.v1.yaml` plus
`specs/capability-matrix/*.yaml` fragments. The Go query runtime mirrors the
same ceilings in
`go/internal/query/contract.go`.

Do not copy the full capability list into prose. The YAML matrix and
fragments plus `go/internal/query/contract_matrix_test.go` are the contract and
drift gate.

Evidence-centric public capabilities also need continuity proof from source
facts through projection/read-models to API and MCP answers. Keep those rows in
`specs/evidence-continuity.v1.yaml`; the verifier fails when a rostered
capability, required domain, public route/tool, empty state, or evidence-loss
negative case is missing.

## Matrix Fields

Each capability row defines:

| Field | Meaning |
| --- | --- |
| `capability` | Stable capability ID used by response truth envelopes. |
| `tools` | MCP or API surfaces that expose the capability. |
| `profiles.<profile_id>.status` | `supported`, `unsupported`, or `experimental`. |
| `profiles.<profile_id>.max_truth_level` | Highest successful truth level allowed in that profile. |
| `profiles.<profile_id>.required_runtime` | Runtime shape required for that row. |
| `profiles.<profile_id>.p95_latency_ms` | Expected p95 budget. |
| `profiles.<profile_id>.max_scope_size` | Bounded scope label. |
| `profiles.<profile_id>.verification` | Test, integration, Compose, or remote proof. |
| `profiles.<profile_id>.notes` | Caveats reviewers need. |

## Runtime Profiles

Current profile IDs are:

- `local_lightweight`
- `local_authoritative`
- `local_full_stack`
- `production`

Profiles describe how Eshu is operated. Graph backend identity is separate and
is reported as `truth.backend` only when a graph adapter served the answer.

## Runtime Requirements

Allowed `required_runtime` values are:

- `local_host`
- `local_host_plus_graph`
- `full_stack`
- `deployed_services`

A profile row marked unsupported must return `unsupported_capability` instead
of returning a low-authority success response.

## Truth Levels

Successful responses must not exceed the matrix row's `max_truth_level`.

| Matrix value | Runtime behavior |
| --- | --- |
| `exact` | Authoritative graph or durable semantic truth may be claimed. |
| `derived` | Deterministic content-index or relational-state truth may be claimed. |
| `fallback` | Exploratory fallback truth may be claimed only when allowed. |
| `unsupported` | Runtime returns `unsupported_capability`. |

`BuildTruthEnvelope` panics for unknown capability IDs, which keeps new handler
claims from bypassing `capabilityMatrix`.

## Backend Conformance

Backend conformance is related but separate:

- `specs/backend-conformance.v1.yaml`
- `go/internal/backendconformance/`
- [Backend Conformance](backend-conformance.md)

The capability matrix says what user-facing behavior may be claimed in a
runtime profile. The backend matrix says which graph-adapter behavior classes
each official backend supports. Current official backend IDs are `nornicdb` and
`neo4j`.

## Change Policy

Changing capability conformance is a product contract change. Update the YAML,
the Go `capabilityMatrix` when truth ceilings changed, user-facing docs for
changed behavior, and focused tests for unsupported-capability behavior.

Lifecycle rules:

| Change | Requirement |
| --- | --- |
| Add | Define every profile row, tool mapping, verification, and Go ceiling. Prefer a small fragment under `specs/capability-matrix/` for new rows. |
| Deprecate | Keep the ID with a deprecation note for at least one release. |
| Remove | Remove only after the deprecation window and client docs update. |

## Validation

For capability or backend conformance edits:

```bash
cd go
go test ./internal/query -run TestCapabilityMatrixMatchesYAMLContract -count=1
go test ./internal/backendconformance -count=1
go test ./internal/evidencecontinuity -count=1
../scripts/verify-evidence-continuity.sh
go run ./cmd/eshu docs verify ../docs/public/reference/capability-conformance-spec.md --limit 1200 --fail-on contradicted,missing_evidence
```

Use `scripts/verify_backend_conformance_live.sh` only when backend behavior or
backend matrix evidence changed.

## Related Docs

- [Capability Catalog](capability-catalog.md)
- [Truth Label Protocol](truth-label-protocol.md)
- [Backend Conformance](backend-conformance.md)
- [MCP Tool Contract Matrix](mcp-tool-contract-matrix.md)
- `specs/README.md`
