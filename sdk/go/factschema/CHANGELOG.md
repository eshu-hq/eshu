# Changelog

All notable changes to `github.com/eshu-hq/eshu/sdk/go/factschema` are
documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Per-fact-kind payload schemas follow the breaking-change policy in
[Contract System v1 §5](../../../docs/internal/design/contract-system-v1.md#5-versioning-and-compatibility-policy):
removing or renaming a field, narrowing a field's type, or changing a stable
key's derivation is a major bump with a decode shim in the same change; an
additive optional field is a minor bump; docs-only is a patch. The **module**
version tracks the highest bump any fact kind needed in that release. See
[SDK Compatibility](../../../docs/public/extend/sdk-compatibility.md) for how
this module version lines up with core Eshu releases and fixture-pack pins.

Tags for this module use the Go subdirectory-module format:
`sdk/go/factschema/vX.Y.Z`. The fixture pack
(`sdk/go/factschema/fixturepack`) ships inside this module and has no separate
version — pinning this module pins the fixture pack too.

## [Unreleased]

Initial release candidate for the fact-schema contracts module (closes the
scaffold started in #4567), proposed as `v0.1.0` (see
[`RELEASING.md`](../../../RELEASING.md) for the exact tag command and
reasoning). This section stays named `[Unreleased]` — not a dated `[0.1.0]`
entry — until a maintainer actually cuts the tag, per the convention below.

### Added

- `Envelope` — the canonical fact envelope shared by every typed fact kind.
- Twenty typed `<family>/v1` packages, each with a `Decode<Kind>`/`Encode<Kind>`
  seam: `aws`, `azure`, `gcp`, `iam`, `secretsiam`, `incident` (the first family
  with dotted wire kinds, e.g. `incident_routing.applied_pagerduty_resource`),
  `cicdrun`, `codedataflow`, `codegraph`, `documentation`, `kuberneteslive`,
  `observability`, `ociregistry`, `packageregistry`, `sbom`,
  `securityalert`, `servicecatalog`, `terraformstate`, `vulnerability`, and
  `workitem`. This is an incremental migration (Contract System v1 §7); a fact
  kind without a typed struct yet still decodes through the untyped envelope
  path.
- Generated JSON Schema artifacts under `schema/<kind>.v1.schema.json` for
  every typed fact kind, produced with `invopop/jsonschema`.
- `fixturepack` subpackage — the versioned, importable payload-conformance
  artifact bundling the checked-in schemas plus one valid and one invalid
  example payload per kind, with a drift-lock test guaranteeing the embedded
  copy matches the canonical generated schema in the same commit.
- `DecodeError` classified error type (`ClassificationInputInvalid`) so a
  missing required field on decode dead-letters visibly instead of silently
  zeroing out.

### Changed

- Core Eshu now generates the adapter that maps durable internal fact envelopes
  into this module's `Envelope` for Decode calls. This does not change any
  factschema payload schema, fixture-pack artifact, or public Go API.

## Convention for future entries

Add a new `## [Unreleased]` entry at the top of this file for every
merge-worthy change to this module — a new typed family, a schema version
bump, a fixture-pack payload change, or a decode-seam behavior change —
grouped under `### Added`, `### Changed`, `### Deprecated`, `### Removed`,
`### Fixed`, or `### Security` as needed (omit empty groups). Name the
affected fact kind(s) explicitly; "misc fixes" is not sufficient detail for a
contracts module external collectors pin against.

When a maintainer cuts a release:

1. Confirm the [schema-diff gate](../../../.github/workflows/factschema-diff.yml)
   is green against the intended base ref — it is this module's `buf breaking`
   equivalent and will catch an unbumped major before the tag is cut.
2. Rename the top `[Unreleased]` section to `[X.Y.Z] - YYYY-MM-DD` using the
   highest-severity bump any fact kind in the release needed.
3. Add a fresh empty `## [Unreleased]` section above it.
4. Tag the release commit `sdk/go/factschema/vX.Y.Z` per
   [`RELEASING.md`](../../../RELEASING.md). This is also the fixture-pack
   version — no separate fixture-pack tag exists.
