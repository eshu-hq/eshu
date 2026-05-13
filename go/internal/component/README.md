# Component

## Purpose

`component` owns Eshu's local component package metadata model. It validates
component manifests, applies local trust policy, records installed packages,
and tracks whether an installed package is activated for a collector instance.

This package does not download remote artifacts, start collectors, mutate core
storage schemas, or write customer documentation. It is the read/write boundary
for package-manager state only.

## Where this fits in the pipeline

```mermaid
flowchart TB
  A["Component manifest"] --> B["LoadManifest"]
  B --> C["Manifest.Validate"]
  C --> D["Policy.Verify"]
  D --> E["Registry.Install"]
  E --> F["Registry.Enable"]
  F --> G["Collector instance config"]
```

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

- `Manifest`, `Metadata`, `Spec`, `Artifact`, `FactFamily`,
  `ConsumerContracts`, and `Telemetry` model the component manifest contract.
  Each `FactFamily` declares supported schema versions and the non-unknown
  source-confidence values the component emits.
- `LoadManifest(path)` loads and validates a manifest from disk.
- `Policy` and `VerificationResult` implement local trust checks.
- `NewRegistry(home)` creates a file-backed installed component registry.
- `Registry.Install`, `List`, `Enable`, `Disable`, and `Uninstall` manage local
  install and activation state.
- `InstalledComponent` records the manifest, digest, install time, verification
  metadata, and activations.
- `Activation` records the collector instance, execution mode, claim behavior,
  and optional configuration for an enabled package.

## Invariants

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

## Tests

Run package tests with:

```bash
go test ./internal/component -count=1
```

The package has focused tests for manifest validation, trust policy decisions,
registry install/list/enable/disable/uninstall behavior, and active uninstall
protection.
