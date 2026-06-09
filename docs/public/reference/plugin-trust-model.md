# Plugin Trust Model

This document defines the minimum trust requirements for optional Eshu
components and OCI-packaged collector plugins.

## Goals

- prevent arbitrary untrusted plugin execution by default
- make provenance and compatibility checks explicit
- give operators a clear allowlist model

## Trust Requirements

Before a plugin is activated, Eshu must verify:

- artifact identity
- artifact provenance
- compatibility with supported fact-schema versions
- operator allowlist or equivalent trust policy

## Activation Modes

- `disabled`
  - plugins are ignored
- `allowlist`
  - only explicitly approved plugin identities may load
- `strict`
  - allowlist plus Sigstore/Cosign signature and attestation verification
    required

Installing a component is not the same as activating it. A component may be
verified and installed in the local registry while still having no enabled
collector instance. Claim-capable execution is a separate operator decision.

## Required Metadata

- plugin ID
- publisher identity
- version
- compatible Eshu core range
- emitted fact kinds and schema versions

Preferred signing model: Sigstore/Cosign-compatible OCI artifact signatures
with keyless certificate identity and OIDC issuer verification.

## Strict Provenance Contract

Strict mode keeps publisher allowlists and Sigstore identities separate. The
manifest `metadata.publisher` is a local Eshu policy identifier. The expected
Sigstore certificate identity and OIDC issuer are operator-supplied trust
inputs.

For each digest-pinned OCI artifact declared by the manifest, strict mode:

- verifies the Cosign signature with claim checking enabled;
- requires the configured certificate identity and OIDC issuer;
- requires signature annotations for component ID, publisher, version,
  compatible core range, SDK protocol, runtime adapter, collector kinds, and
  emitted fact kinds, schema versions, source-confidence values, reducer
  phases, and telemetry prefix;
- verifies a `slsaprovenance1` attestation for the same artifact.

Missing signatures, digest-claim mismatches, wrong certificate identity,
unallowlisted publisher IDs, revoked IDs or publishers, incompatible core
ranges, and unsupported attestation shapes fail closed.

## Revocation

Operators must be able to revoke a plugin ID or publisher identity without
removing all plugin support globally.

Allowlist and revocation policy should be sourced from explicit operator
configuration. Initial implementations may use a local configuration file before
adding central policy distribution.

Publisher identity rotation must support an explicit trust-transfer procedure,
not silent key replacement.

## Failure Policy

- incompatible or untrusted plugins fail closed
- failure should identify the plugin and the violated rule
- operators may choose whether one plugin failure blocks startup entirely

## Non-Goals

- guaranteeing safety of arbitrary plugin code beyond the stated trust checks
- automatic approval of new publishers

The component-manager strict path verifies provenance through the Cosign CLI
boundary. Verification and installation do not execute component code; runtime
activation and claim capability remain separate operator decisions.
