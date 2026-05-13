# GAR Adapter Notes

Read `adapter.go`, `adapter_test.go`, and the OCI registry ADR before changing
this package.

Keep GAR behavior as a narrow adapter over
`go/internal/collector/ociregistry/distribution`. Do not implement Google
credential helper behavior here; the runtime receives already-resolved
credentials through environment variables.

Do not leak service account keys, access tokens, host-private names, or project
details into logs, errors, facts, metric labels, fixtures, or docs.
