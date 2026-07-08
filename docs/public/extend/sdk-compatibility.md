# SDK Compatibility

Use this page to answer one question: which `sdk/go/collector` version, which
`sdk/go/factschema` version (fixture-pack version), which wire protocol, and
which core Eshu release are meant to run together.

Both SDK modules are public Go subdirectory modules with independent tags —
`sdk/go/collector/vX.Y.Z` and `sdk/go/factschema/vX.Y.Z` — per the repository's
`RELEASING.md`. Core Eshu itself tags as `vX.Y.Z` at the repository root. The
three version lines move independently; this table is the single place that
states which combinations are supported together.

## Compatibility table

| SDK module version | Core release | Wire protocol | Fixture-pack version | Support window |
| --- | --- | --- | --- | --- |
| `sdk/go/collector` v0.1.x (unreleased; see `sdk/go/collector/CHANGELOG.md`) | `v0.0.3-pre-release-*` train (current; stable `v0.0.3` not yet cut, see [Release Log](../releases/index.md)) | `collector-sdk/v1alpha1` | `sdk/go/factschema` v0.1.x (unreleased; see `sdk/go/factschema/CHANGELOG.md`) | Initial supported combination. Manifests may set `payloadSchemaRef` to validate a namespaced component fact against a fixture-pack payload shape. |

The `v0.1.x` row includes core-side generated adapters between
`sdk/go/collector.Fact`, `go/internal/facts.Envelope`, and
`sdk/go/factschema.Envelope`. Those adapters preserve the
`collector-sdk/v1alpha1` JSON field names and do not require a wire-protocol or
fixture-pack version bump.

This table grows one row per tagged SDK release once tags exist. Do not
remove a row when a newer one is added — a consumer pinned to an older tag
still needs to find its row.

## How to read a row

- **SDK module version** — the Go module version an external collector `go
  get`s or pins in `go.mod`. `sdk/go/collector` and `sdk/go/factschema` are
  versioned independently (they can carry different major/minor numbers),
  but this table only lists combinations that were actually built and
  released together, since that is the combination proven to interoperate.
- **Core release** — the core Eshu tag (repository root `vX.Y.Z`) the SDK
  version was validated against, expressed as the range in
  `spec.compatibleCore` on a component manifest built against that SDK
  version, for example `>=0.0.5 <0.2.0`
  (`examples/collector-extensions/scorecard/manifest.yaml` is the worked
  example).
- **Wire protocol** — the `collector-sdk/vN` string a `Result` and the host's
  `Contract` both carry (`sdk/go/collector/types.go`). Two collectors on
  different SDK module versions but the same wire protocol string still
  interoperate at the transport level; the module version otherwise only
  affects the Go API surface and bundled schema/fixture content.
- **Fixture-pack version** — the `sdk/go/factschema` module version, since the
  fixture pack ships inside that module with no separate version number (see
  `sdk/go/factschema/fixturepack/README.md`). Pin this version to prove
  payload-shape conformance against the same schemas the target reducer
  release decodes.

## Core's support window for payload schemas

Per Contract System v1 §5 (`docs/internal/design/contract-system-v1.md`,
internal design doc), the reducer decodes payload schema **major N and major
N-1** for each fact kind via contracts-module conversion shims; a collector
still emitting a payload major older than N-1 is quarantined as
`unsupported_minor`-equivalent and stops being authoritative. The same N/N-1
window applies to the wire protocol: the host dual-accepts protocol N and
N-1 for at least two minor core releases after a protocol bump, and
extensions pin exactly one protocol version at a time.

Today there is only one payload major (`1`) and one wire protocol
(`collector-sdk/v1alpha1`), so the N-1 window is not yet exercised. This
section states the policy now so the first breaking bump has a table row to
extend rather than a policy to invent.

## Choosing a pin

An external collector should:

1. Pin `sdk/go/collector` at the tag in the row matching its target core
   release.
2. Pin `sdk/go/factschema` at the same row's fixture-pack version, declare
   `payloadSchemaRef` for every emitted fact that reuses a core payload shape,
   and run `conformance.Run` with `Request.PayloadSchemas` sourced from that
   pinned module's `fixturepack` package (see
   [Validate payload shape against a pinned fixture pack](community-extension-authoring.md#validate-payload-shape-against-a-pinned-fixture-pack)).
3. Declare `spec.compatibleCore` in its manifest using the core range from the
   same row.
4. Re-run conformance in its own CI before bumping either pin, so a payload
   shape it can no longer satisfy fails closed in the collector's CI before it
   ever reaches a reducer.

## Related docs

- [Community Extension Authoring](community-extension-authoring.md)
- Contract System v1 (`docs/internal/design/contract-system-v1.md`, internal
  design doc; §5 versioning policy, §6 enforcement gates)
- Fixture pack README (`sdk/go/factschema/fixturepack/README.md`)
- Repository `RELEASING.md`
- [Release Log](../releases/index.md)
