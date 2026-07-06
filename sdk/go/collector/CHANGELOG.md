# Changelog

All notable changes to `github.com/eshu-hq/eshu/sdk/go/collector` are documented
in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this module follows the versioning rule in
[Contract System v1 §5](../../../docs/internal/design/contract-system-v1.md#5-versioning-and-compatibility-policy):
a breaking change to the `collector-sdk/v1alpha1` wire protocol or the public
Go API is a major bump, an additive backward-compatible change is a minor
bump, and a docs-only or internal change is a patch. See
[SDK Compatibility](../../../docs/public/extend/sdk-compatibility.md) for how
this module version lines up with core Eshu releases and the wire protocol.

Tags for this module use the Go subdirectory-module format:
`sdk/go/collector/vX.Y.Z`.

## [Unreleased]

## [0.1.0] - Unreleased

Initial tagged release of the collector SDK.

### Added

- `Claim`, `Scope`, and `Generation` types describing a core-owned work item
  handed to an out-of-tree collector.
- `Fact`, `SourceRef`, and `Redaction` types for source evidence records.
- `Status` and `Result` types for `complete`, `unchanged`, `partial`,
  `retryable`, and `terminal` collector outcomes.
- `Contract`, `FactDeclaration`, `Validator`, and `ValidationReport` for
  fail-closed host-side validation via `NewValidator(contract).ValidateResult`.
- `collector-sdk/v1alpha1` wire protocol, published as
  `schema/collector-sdk-v1alpha1.schema.json`.
- `conformance` subpackage (`sdk/go/collector/conformance`) — the public,
  importable conformance harness (`conformance.Run`) an out-of-tree collector
  runs in its own CI, including payload-shape validation against
  `sdk/go/factschema` schemas via `conformance.Request.PayloadSchemas`.
- `schema/cassette-format.v1.schema.json` — a generated mirror of the host's
  replay cassette envelope contract, so credential-free replay fixtures can be
  validated offline against the same schema the host enforces.

## Convention for future entries

Add a new `## [Unreleased]` entry at the top of this file for every
merge-worthy user-facing change to this module, grouped under `### Added`,
`### Changed`, `### Deprecated`, `### Removed`, `### Fixed`, or `### Security`
as needed (omit empty groups). When a maintainer cuts a release:

1. Rename the top `[Unreleased]` section to `[X.Y.Z] - YYYY-MM-DD` using the
   version chosen per the breaking/additive/patch rule above.
2. Add a fresh empty `## [Unreleased]` section above it.
3. Tag the release commit `sdk/go/collector/vX.Y.Z` per
   [`RELEASING.md`](../../../RELEASING.md).

A wire-protocol-breaking change additionally needs a new protocol version
(for example `collector-sdk/v1`) per Contract System v1 §5; do not reuse
`v1alpha1` for an incompatible wire shape.
