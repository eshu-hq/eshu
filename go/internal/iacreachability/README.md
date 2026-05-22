# internal/iacreachability

## Purpose

`internal/iacreachability` classifies Terraform, Helm, and Ansible artifacts as
used, unused, or ambiguous from bounded repository content evidence.

## Ownership boundary

This package owns in-memory reachability analysis only. It does not load facts
from Postgres, write status rows, parse source files, or decide graph truth.
Bootstrap and storage adapters call it when materializing IaC reachability
rows.

## Exported surface

See `doc.go` for the package contract. Exported surfaces include
`Reachability`, `Finding`, `File`, `Options`, `Row`, `Analyze`, `CleanupRows`,
`FamilyFilter`, and `RelevantFile`.

## Dependencies

The package uses repository file metadata supplied by callers. Postgres
materialization lives in `internal/storage/postgres`; parser and collector
packages own upstream source facts.

## Telemetry

The package emits no metrics or spans directly. The Postgres materializer wraps
analysis with `SpanIaCReachabilityMaterialization` and outcome metrics.

## Gotchas / invariants

- Ambiguous evidence is a first-class outcome. Do not promote it to used or
  unused without explicit source proof.
- Cleanup rows should include ambiguous results only when the caller asks for
  them.
- `RelevantFile` is a bounded prefilter; parser facts remain the stronger
  evidence source when available.
- Keep family filtering deterministic for repeated bootstrap runs.

## Focused tests

```bash
cd go
go test ./internal/iacreachability -run 'Test.*Analyze|Test.*ProductTruth|Test.*Cleanup|Test.*Relevant' -count=1
go test ./internal/iacreachability -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

## Related docs

- `docs/public/architecture.md`
- `docs/public/reference/local-testing.md`
- `go/internal/storage/postgres/README.md`
