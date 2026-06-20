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

## Publication Policy

Community index publication is a maintainer-reviewed discovery signal, not a
runtime trust grant. A package is eligible for the index only when the review
record can be checked from package metadata and artifact evidence:

- every runnable artifact is digest-pinned and has a matching signature;
- provenance binds the artifact digest, component ID, publisher, version,
  compatible core range, runtime protocol, adapter, collector kind, emitted fact
  kinds, schema versions, reducer phases, and telemetry prefix;
- declared capabilities name egress, credential-reference classes, resource
  limits, runtime adapter, source-scope class, and claim capability;
- component configuration uses credential handles only and does not include
  secret values, private endpoints, source payloads, or provider responses;
- emitted fact kinds are namespaced, do not collide with core-owned fact kinds
  or another package, and declare supported schema-version majors;
- the package declares whether each fact family is provenance-only or has a
  core-owned reducer, projector, API, or MCP consumer contract;
- revocation state is explicit for component ID, publisher, artifact digest,
  version range, and policy review record.

The community index must fail closed for missing signatures, stale or
unsupported provenance, mutable artifact tags, unreviewed egress, credential
value exposure, schema collisions, incompatible core ranges, unsupported runtime
protocols, absent conformance proof, and revoked identities. Local and hosted
runtime policy still re-verifies the package before install, enablement, or
claim-capable work.

## Compatibility Badge

A compatibility badge is machine-checkable only when it is derived from the
same structured metadata that the verifier consumes. Badge inputs are:

- component ID, publisher, version, and manifest digest;
- compatible Eshu core range and manifest API version;
- artifact digest plus signature and SLSA provenance status;
- supported runtime protocol and adapter;
- emitted fact kinds, schema-version majors, source-confidence values, and
  reducer consumer phases;
- conformance artifact URI or review record, with its status and timestamp;
- local or hosted policy result: `installable`, `blocked`, `revoked`,
  `incompatible`, `missing_proof`, or `unsupported_runtime`.

Badges must not be hand-authored from README text, repository topics, or
marketplace copy. If any input is missing, expired, revoked, or incompatible,
the badge state is blocked and the diagnostics must name the exact rule.

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
