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
| `sdk/go/collector` v0.1.0 (`sdk/go/collector/v0.1.0`, 2026-07-06; proxy-verified per #6708) | Enforced manifest range `>=0.0.5 <0.2.0` (worked example `examples/collector-extensions/scorecard/manifest.yaml`, checked by `verifyCompatibleCore` in `go/internal/component/policy.go`). Latest tagged core is `v0.0.3-pre-release-*` (see [Release Log](../releases/index.md)); dev builds report version `dev`, which skips the range check, so the example activates on dev builds and enforces on release builds. | `collector-sdk/v1alpha1` | `sdk/go/factschema` v0.1.0 (`sdk/go/factschema/v0.1.0`, 2026-07-06) | Supported. Initial tagged combination; 130 fixture-pack schemas. |
| `sdk/go/collector` v0.2.0 (`sdk/go/collector/v0.2.0`, 2026-09-16; tag cut after merge per `RELEASING.md`, proxy verification to be recorded in #6708) | Same enforced range and dev-build behavior as the `v0.1.0` row above. | `collector-sdk/v1alpha1` (unchanged) | `sdk/go/factschema` v0.2.0 (`sdk/go/factschema/v0.2.0`, 2026-09-16; same tag/proxy note) | Pending tag cut (supported once tagged and proxy-verified). Additive post-v0.1.0 changes only: redaction helper exports, opaque-URI credential refusal, six new fact-schema families (46 new schemas), fixture-pack growth. Manifests may set `payloadSchemaRef` to validate a namespaced component fact against a fixture-pack payload shape. |

The `v0.2.0` row includes core-side generated adapters between
`sdk/go/collector.Fact`, `go/internal/facts.Envelope`, and
`sdk/go/factschema.Envelope`. Those adapters preserve the
`collector-sdk/v1alpha1` JSON field names and do not require a wire-protocol or
fixture-pack version bump.

This table grows one row per tagged SDK release. Do not
remove a row when a newer one is added — a consumer pinned to an older tag
still needs to find its row.

## Release ownership

Tagging is coordinator-controlled per the repository's `RELEASING.md`: once a
tag is pushed, the Go module proxy caches that module version permanently, so
a mistake cannot be un-pushed. Contributors prepare the release in a PR
(CHANGELOG entry, this compatibility row, schema/fixture proof); a maintainer
cuts the tag after merge with the exact commands in `RELEASING.md`. Never tag
from a worktree or a stale checkout — only from a clean, fast-forwarded
`main`.

## Version support policy

- Both modules are `v0.x`: per Go module semantics there is no
  backward-compatibility guarantee across minor versions. Pin exact versions
  in production collectors.
- The **latest tagged minor** of each module is the supported line. Older
  minors receive no backports except security fixes (see below).
- A collector on an older supported SDK minor keeps interoperating while it
  speaks a supported wire protocol and payload-schema major (see the N/N-1
  window below): older-minor output must still validate, and does — the
  `v0.1.0` golden `complete.json` fixture is byte-identical at current `HEAD`
  and validates cleanly.
- Newer-collector against older-core is bounded by `spec.compatibleCore`:
  the host rejects manifests whose declared core range does not cover the
  running core, and admission rejects fact kinds or schema majors the core
  does not know.

## Deprecation policy

- A wire-protocol or payload-schema major bump ships a new protocol/kind
  version alongside the old one; the old major stays accepted for at least
  two minor core releases (the N/N-1 window in "Core's support window"
  below) before removal.
- Deprecations are announced in the affected module's CHANGELOG, in a new row
  of the table above, and — for collector-visible behavior — in
  [Community Extension Authoring](community-extension-authoring.md). The
  announcement names the replacement, the last supporting release, and the
  removal release. A removal without all three is not ready.
- Additive Go API additions are never deprecated in the same release that
  introduces them.

## Security-fix policy

- Security fixes follow the repository Security Policy (`SECURITY.md` at the
  repository root): report privately, never in a public issue; fixes target
  the latest release.
- A fix touching an SDK module ships as a patch release on the latest minor
  line (e.g. `v0.2.1`), with a `### Security` CHANGELOG entry describing the
  impact and the upgrade action, and a compatibility-table note when the fix
  changes validation behavior (fail-closed tightenings reject inputs an older
  minor accepted — that is intended, and the entry says so).
- Credential-handling fixes additionally grow the redaction canary tests, so
  the fixed shape can never regress silently.

## Import migration (`replace` → pinned versions)

In-monorepo consumers (`go/go.mod`, `examples/collector-extensions/scorecard`)
resolve both SDK modules by `replace` directives to the local tree so a PR
builds and tests unreleased SDK changes in the same branch. That is a
monorepo-only convenience. An out-of-tree collector has no local tree and
pins released versions instead:

```text
require github.com/eshu-hq/eshu/sdk/go/collector v0.1.0
require github.com/eshu-hq/eshu/sdk/go/factschema v0.1.0
```

with no `replace` directive and no `go.work` — `go get
github.com/eshu-hq/eshu/sdk/go/collector@v0.1.0` resolves through the public
Go module proxy. The `sdk/go/factschema` pin is also the fixture-pack pin.
Module paths are unchanged since the first tag, so migration today is only
"delete the `replace` lines, `require` the row above"; if paths ever change
(e.g. a future `eshu-sdk` repository split), the new paths land here with a
mapping row before any tag uses them.

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
