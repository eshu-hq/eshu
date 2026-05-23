# Harbor Adapter Notes

Read `adapter.go`, `adapter_test.go`, and the OCI registry ADR before changing
this package.

Keep this package narrow. Harbor should delegate Distribution API behavior to
`go/internal/collector/ociregistry/distribution`; provider-specific code here is
limited to endpoint validation, repository normalization, and auth plumbing.

Do not add secrets to URLs, errors, logs, facts, metric labels, fixtures, or
documentation.
