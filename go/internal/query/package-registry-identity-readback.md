# Package Registry Identity Readback

Package-registry package reads must never serialize a graph row with a blank
`package_id` as a valid package identity. The package list handler classifies
those rows under `identity_issues[]` with `reason=package_id_missing` and
`missing_evidence=["package_id"]`, while valid package rows with
`version_count=0` remain normal package identities.

No-Regression Evidence: `go test ./internal/query -run 'TestPackageRegistryListPackagesClassifiesBlankPackageIdentityRows|TestPackageRegistryListPackagesPreservesZeroVersionNPMIdentities|TestOpenAPISpecIncludesPackageRegistryIdentityIssues' -count=1`
proves blank package identities are not returned as packages, the readback
keeps scoped and unscoped npm packages with zero observed versions, and the
OpenAPI package-list schema advertises the identity issue classification.

No-Observability-Change: this is response classification after the existing
bounded package-list graph query. Operators still diagnose the path through the
`query.package_registry_packages` span, graph query spans, HTTP status, truth
envelope, and response `count`, `limit`, `truncated`, and `identity_issues`
fields. No graph write, queue, worker, runtime knob, or metric label changes.
