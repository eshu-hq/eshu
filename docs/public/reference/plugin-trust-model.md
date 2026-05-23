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
  - allowlist plus signature/provenance verification required

Installing a component is not the same as activating it. A component may be
verified and installed in the local registry while still having no enabled
collector instance. Claim-capable execution is a separate operator decision.

## Required Metadata

- plugin ID
- publisher identity
- version
- compatible Eshu core range
- emitted fact kinds and schema versions

Preferred signing model: Sigstore/Cosign-compatible OCI artifact signatures.

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

The first local component-manager slice supports disabled and allowlist checks.
Strict mode fails closed until Sigstore/Cosign or equivalent provenance
verification is wired in.
