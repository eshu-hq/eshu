# Status

## Purpose

`status` projects raw runtime counts, queue state, generation lifecycle rows,
and collector snapshots into the operator-facing report used by the CLI, HTTP
admin surface, and runtime status views.

## Ownership boundary

This package owns report shapes, health evaluation, text/JSON rendering, the
`/admin/status` handler adapter, and retry-policy metadata attachment. It does
not own queue persistence, HTTP routing, or metric emission.

## Exported surface

Use `doc.go` and `go doc ./internal/status` for the operator contract. The
stable anchors are raw snapshots, projected reports, health summaries, queue and
generation views, collector status sections, renderers, loaders, and the HTTP
handler adapter.

## Dependencies

`status` imports `internal/buildinfo` for rendered version information. It does
not import storage or telemetry packages; storage implementations provide the
`Reader` data.

## Telemetry

This package emits no metrics or spans because it is itself an operator signal
surface. Failure messages, conflict keys, safe locator hashes, and bounded
details may appear in status payloads, but must not become metric labels.

## Gotchas / invariants

- Health priority is `stalled`, `degraded`, `progressing`, then `healthy`.
- Shared projection backlog is unfinished graph-visible work. Lease-only
  activity stays visible without blocking healthy.
- Domain backlog rows are capped by `Options.DomainLimit`.
- A nil coordinator snapshot means the runtime did not wire a coordinator status
  source.
- AWS cloud status keeps scan state separate from fact commit state.
- AWS freshness output is aggregate only; resource IDs, ARNs, event IDs, and
  raw payloads stay out of the report.
- Terraform-state status uses safe locator hashes and grouped warning kinds, not
  raw paths, bucket names, or object keys.

## Related docs

- `docs/public/reference/runtime-admin-api.md`
- `docs/public/reference/cli-reference.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/architecture.md`
