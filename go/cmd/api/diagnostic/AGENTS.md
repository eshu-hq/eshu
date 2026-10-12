# API status diagnostic agent guidance

Read `go/cmd/api/AGENTS.md` and `README.md` before changes here. This package
only captures opt-in, private status-route timing evidence. Preserve the
production authentication, checkpoint, reader, and OTEL observer paths. Keep
SDK callbacks bounded and free of I/O or SQL. Never retain request payloads,
URL parameters, credentials, SQL, raw errors, or arbitrary span attributes.

A behavior change needs a failing focused regression first. Verify the exact
route, disabled mode, concurrent ID isolation, failure and overflow reports,
private attribute filtering, mode 0600 flush, and the package's race tests.
Run `go test ./cmd/api/...` from `go/` after edits so this subpackage is not
skipped by a nonrecursive package command.
