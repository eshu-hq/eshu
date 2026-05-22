# Relationships

## Purpose

`relationships` extracts Terraform, provider-schema, Terragrunt, Helm,
Kustomize, Argo CD, CI, Ansible, Dockerfile, and Docker Compose relationship
evidence before reducer admission.

## Ownership boundary

This package reports evidence and candidates. It does not decide canonical graph
truth, write Postgres rows, enqueue reducer work, or materialize graph edges.
Reducers own persistence, admission, and later graph projection.

## Exported surface

Use `doc.go` and `go doc ./internal/relationships` for the exported contract.
The main surfaces are evidence discovery, deduplication, assertion handling,
relationship resolution, schema-driven Terraform extractor registration, and
the candidate and resolved-relationship model types.

## Dependencies

`relationships` reads `facts.Envelope` from `internal/facts` and uses
`internal/terraformschema` for schema-driven Terraform extraction.

## Telemetry

This package emits no metrics, spans, or structured logs. Reducer and storage
callers expose extraction counts, admission counts, persistence failures, and
graph-write signals.

## Gotchas / invariants

- Extractors must be deterministic for the same facts, catalog aliases, and
  schema inputs.
- Ambiguous signals stay low-confidence unless an explicit assertion admits
  them.
- Do not lower `DefaultConfidenceThreshold` to force graph truth.
- Repository aliases should be real repo names or known aliases; overly short
  aliases can match unrelated text.
- Terraform registry source strings are not repository aliases by themselves.
- Argo CD multi-source applications must preserve source/path/root/revision
  tuple alignment by source index.
- ApplicationSet template extraction needs generator files in the same envelope
  batch.

## Related docs

- `docs/public/architecture.md`
- `docs/public/reference/local-testing.md`
- `go/internal/terraformschema/README.md`
- `go/internal/iacreachability/README.md`
