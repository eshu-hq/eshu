# Facts

## Purpose

`facts` defines the durable source-observation records Eshu writes before graph
projection and reducer-owned materialization.

## Ownership boundary

This package owns durable fact value types, source-confidence vocabulary,
fact-kind registries, schema-version helpers, stable-ID helpers, and
source-local references. It does not own queue rows, scope assignment, graph
writes, Postgres storage, collector scheduling, or reducer admission.

## Exported surface

Use `doc.go` and `go doc ./internal/facts` for the full contract. The main
surface is `Envelope`, `Ref`, copy-safe cloning, stable ID generation,
source-confidence validation, and fact-kind/schema-version helpers for the
supported evidence families.

## Dependencies

`facts` imports only the Go standard library.

## Telemetry

This package emits no metrics, spans, or logs. Runtime telemetry around fact
commit, queueing, loading, and processing lives in the packages that perform
that work.

## Gotchas / invariants

- `Envelope` fields are on-disk contracts. Remove or rename only with a storage
  compatibility plan.
- New fields must be additive and backward-compatible.
- Clone envelopes before replaying, branching, or passing payloads to code that
  may mutate nested maps or slices.
- `StableID` expects JSON-serializable identity maps. Do not include raw
  credentials, source body content, or mutable high-cardinality values.
- `FencingToken` travels with the envelope so stale workers cannot overwrite
  newer generation data.
- `terraform_state_candidate` is metadata-only evidence from Git collection;
  raw state bytes belong on the Terraform-state collector path.
- Registry, cloud, SBOM, vulnerability, service catalog, documentation,
  CI/CD, and Terraform state facts are reported or observed evidence. Reducers
  decide graph truth.

## Related docs

- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/fact-envelope-reference.md`
- `docs/public/guides/collector-authoring.md`
