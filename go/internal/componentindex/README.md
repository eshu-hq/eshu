# Component Index

## Purpose

`componentindex` validates the static community extension index proposed in
[#1830](../../../docs/internal/design/1830-community-extension-index-publication-workflow.md).
The index is a reviewed discovery document for component packages; it is not a
trust grant and does not install, enable, or run extensions.

## Ownership boundary

This package owns index document validation only. It checks entry metadata,
digest pins, lifecycle channels, duplicate component IDs, duplicate fact-kind
claims, schema-version shape, source-confidence vocabulary, reducer consumer
contracts, signature/provenance references, conformance proof references and
status, publication state, compatibility badge metadata, review links, and
revoked installable entries. It does not pull OCI artifacts, inspect GitHub
topics, verify Sigstore provenance, mutate the local component registry, or
bypass `component.Policy`.

## Exported surface

The godoc contract in `doc.go` covers the package API. The main entry point is
`Validate`, which accepts an `Index` and returns a deterministic `Report` with
stable `IssueCode` values for maintainer tooling.

## Dependencies

The package uses the Go standard library, semver parsing, and local fact
vocabulary helpers. It intentionally does not import registry, network, or
GitHub clients, keeping verifier behavior offline and deterministic.

## Telemetry

No telemetry is emitted. The verifier is a pure validation helper. Future CLI,
API, or MCP surfaces that expose index state must add their own bounded metrics,
spans, logs, or status fields.

## Gotchas / invariants

- Index membership never bypasses disabled, allowlist, strict, revocation, or
  compatible-core checks in the component package manager.
- Artifact references must be digest pinned; mutable tags are metadata failures,
  not warnings.
- Two entries cannot claim the same fact kind in the v1 index.
- Emitted fact kinds must be namespaced and must not claim core-owned Eshu fact
  kinds.
- Schema versions must be semantic versions, and source confidence must be one
  of `observed`, `reported`, `inferred`, or `derived`.
- Index entries must point at reducer consumer metadata, signature evidence, and
  passed conformance proof artifacts before they can be treated as
  publication-ready metadata.
- Draft publication entries may record pending signature and provenance status,
  but published entries must not use placeholder digests or pending badge
  evidence.
- Revocation wins over installable state.

## Related docs

- [Community Extension Index And Publication Workflow](../../../docs/internal/design/1830-community-extension-index-publication-workflow.md)
- [Component Package Manager](../../../docs/public/reference/component-package-manager.md)
- [Plugin Trust Model](../../../docs/public/reference/plugin-trust-model.md)
- [Fact Schema Versioning](../../../docs/public/reference/fact-schema-versioning.md)
