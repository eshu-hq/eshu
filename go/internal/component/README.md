# Component

## Purpose

`component` owns Eshu's local component package metadata model. It validates
component manifests, applies local trust policy, records installed packages,
and tracks whether an installed package is activated for a collector instance.

This package does not download remote artifacts, start collectors, mutate core
storage schemas, or write customer documentation. It is the read/write boundary
for package-manager state only.

The CLI in `go/cmd/eshu` calls this package for `eshu component inspect`,
`verify`, `install`, `list`, `enable`, `disable`, and `uninstall`.

## Internal flow

1. `LoadManifest` reads a YAML component manifest from disk and validates its
   top-level contract.
2. `Policy.Verify` evaluates the manifest against the selected trust mode,
   allowlists, and revocation lists.
3. `Registry.Install` persists the manifest under the component home and records
   the manifest digest in `registry.json`.
4. `Registry.Enable` records an activation only after a component is installed.
   Installed packages are inert until activated.
5. `Registry.Disable` removes an activation without deleting the installed
   package or its manifest digest.
6. `Registry.Uninstall` removes an inactive installed package version and
   rejects removal while any activation references the component.

## Exported surface

See `doc.go` and exported comments in `manifest.go`, `policy.go`, and
`registry.go` for the godoc contract. Keep field-level manifest and registry
details in source comments so validation changes stay reviewable beside the
code.

## Dependencies

- `internal/facts` for source-confidence constants used by manifests.
- Standard library filesystem and JSON/YAML helpers for local registry state.
- `golang.org/x/mod/semver` for compatible-core and package version checks.

## Telemetry

None. Commands that call this package own user-facing output and any future
runtime instrumentation.

## Gotchas / invariants

- Git remains built in. Optional collectors and services must be installed and
  enabled explicitly.
- Installed does not mean enabled. Enabled does not mean claim-capable.
- Trust policy fails closed when provenance cannot be verified.
- Registry writes are atomic so a partial write cannot corrupt
  `registry.json`.
- Component manifests must pin artifact images by digest.
- Component manifests must declare source-confidence values per emitted fact
  family. `unknown` remains a storage compatibility fallback, not component
  output.
- Unknown or unsupported package behavior must remain inert at install time.

## Verification

```bash
go test ./internal/component -count=1
go run ./cmd/eshu docs verify ../go/internal/component --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related docs

- `docs/public/reference/component-package-manager.md`
- `go/cmd/eshu/README.md`
