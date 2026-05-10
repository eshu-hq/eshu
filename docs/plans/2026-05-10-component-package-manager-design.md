# Component Package Manager Design

## Goal

Eshu core should stay small, Git-first, and read-only where possible, while
operators can install optional collectors and supporting services only when
they need them. The package manager gives Eshu a safe boundary for that: a
component can be inspected, verified, installed, enabled, disabled, or removed
without pretending that every deployment needs every collector.

## Product Shape

The first implementation should support local and air-gapped component
packages. OCI distribution and Sigstore verification are part of the long-term
contract, but the first slice must not fake those guarantees. It should define
the manifest and registry boundary now, make local package operations work, and
fail clearly when a user asks for a trust mode that this slice cannot prove.

Git remains built in. Optional collectors such as AWS, Kubernetes, registry,
SBOM, vulnerability intelligence, and future updater-side services should use
the component boundary.

## Architecture

The design has three layers.

1. Component manifest
   - Declares component identity, version, component type, collector kinds,
     compatible Eshu core range, emitted fact kinds, consumer contracts,
     permissions, telemetry, and artifacts.
   - Uses `apiVersion: eshu.dev/v1alpha1` and `kind: ComponentPackage`.
   - Keeps install metadata separate from activation state.

2. Local installed-component registry
   - Stores installed manifests under a managed component home.
   - Records install time, digest, verification state, and activation records.
   - Is file-backed in the first slice so local and air-gapped workflows work
     without adding database coupling too early.

3. Activation layer
   - Enabling a component creates an activation record with instance ID, mode,
     claims flag, and config path or inline config.
   - Runtime scheduling still belongs to the workflow coordinator and
     `CollectorInstance` contract. The package manager does not directly start
     arbitrary code in this first slice.

## CLI Contract

Add a new `eshu component` command group:

- `eshu component inspect <manifest>`
- `eshu component verify <manifest> [--policy <policy>]`
- `eshu component install <manifest> [--component-home <dir>]`
- `eshu component list [--component-home <dir>]`
- `eshu component enable <id> --instance <id> [--mode <mode>] [--claims]`
- `eshu component disable <id> --instance <id>`
- `eshu component uninstall <id> [--version <version>]`

Do not reuse `eshu add-package`; that command remains a removed compatibility
stub for old package-indexing muscle memory.

## Trust Policy

The first slice supports:

- `disabled`: verification fails closed for all optional components.
- `allowlist`: component ID and publisher must be explicitly allowed.
- `strict`: fails closed unless a future verifier is wired in. This is better
  than accepting an unsigned artifact while claiming strict mode.

The policy shape should leave room for digest pins, publisher revocation, and
signature/provenance verification.

## Data Model

The registry is intentionally boring JSON:

- installed components keyed by component ID and version
- manifest digest
- manifest path in the component home
- verification mode and status
- activation records keyed by instance ID

Registry writes must be atomic enough for local CLI use: write a temp file in
the same directory, then rename it.

## Failure Policy

- Invalid manifests fail before install.
- Incompatible core ranges fail before install.
- Disabled or untrusted policies fail before install.
- Strict mode fails closed until real provenance verification exists.
- Enabling a component that is not installed fails.
- Uninstalling an active component fails unless the user disables it first.

## Testing

The PR should include:

- manifest validation tests
- trust-policy tests for allowlist, disabled, revoked, and strict modes
- registry install/list/enable/disable/uninstall tests
- CLI tests for inspect, install, list, and enable failure cases
- docs build
- full Go test suite

