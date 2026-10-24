# SDK Compatibility

Use this page to identify published SDK/core combinations and to track the
separate Eshu SDK repository migration. A candidate module path is not a
supported core/SDK combination until its own qualification evidence exists.

Both SDK modules are public Go subdirectory modules with independent tags —
`sdk/go/collector/vX.Y.Z` and `sdk/go/factschema/vX.Y.Z` — per the repository's
`RELEASING.md`. Core Eshu itself tags as `vX.Y.Z` at the repository root. The
three version lines move independently. The compatibility table states the
combinations supported under the original module paths. The new-path mapping
below records source preparation without adding a supported combination.

## Compatibility table

| SDK module version | Core release | Wire protocol | Fixture-pack version | Support window |
| --- | --- | --- | --- | --- |
| `sdk/go/collector` v0.1.0 (`sdk/go/collector/v0.1.0`, 2026-07-06; proxy-verified per #6708) | Enforced manifest range `>=0.0.5 <0.2.0` (worked example `examples/collector-extensions/scorecard/manifest.yaml`, checked by `verifyCompatibleCore` in `go/internal/component/policy.go`). Latest tagged core is `v0.0.3-pre-release-*` (see [Release Log](../releases/index.md)); dev builds report version `dev`, which skips the range check, so the example activates on dev builds and enforces on release builds. | `collector-sdk/v1alpha1` | `sdk/go/factschema` v0.1.0 (`sdk/go/factschema/v0.1.0`, 2026-07-06) | Supported. Initial tagged combination; 130 fixture-pack schemas. |
| `sdk/go/collector` v0.2.0 (`sdk/go/collector/v0.2.0`, 2026-09-16) | Same enforced range and dev-build behavior as the `v0.1.0` row above. | `collector-sdk/v1alpha1` (unchanged) | `sdk/go/factschema` v0.2.0 (`sdk/go/factschema/v0.2.0`, 2026-09-16) | Published and proxy-verified on the original module paths. Their existing support and security policy remains in force during migration. Additive post-v0.1.0 changes include redaction helper exports, opaque-URI credential refusal, six new fact-schema families (46 new schemas), and fixture-pack growth. Manifests may set `payloadSchemaRef` for a namespaced component fact. |

Both original `v0.2.0` module tags resolve from origin commit
`2c8ab5c228c87a7b47f16063d4b1f43027d33ad6`. The Go proxy module-zip
checksums are `h1:wOlmnu26Q/MAo50lNGlqj+KJvYDnI6x1CYOThhzNlEg=` for
`github.com/eshu-hq/eshu/sdk/go/collector@v0.2.0` and
`h1:47bNIgc95iYH7Thf0YWwIKbcHqf+m1UHC+En6IjOMKU=` for
`github.com/eshu-hq/eshu/sdk/go/factschema@v0.2.0`. The corresponding
`go.mod` checksums and reproducible proxy query are in the
[SDK migration evidence](https://github.com/eshu-hq/eshu-sdk/blob/d6ce994d6709a13b6f2dc797f7b988b7b64a68fc/docs/sdk-migration-evidence.md).

The `v0.2.0` row includes core-side generated adapters between
`sdk/go/collector.Fact`, `go/internal/facts.Envelope`, and
`sdk/go/factschema.Envelope`. Those adapters preserve the
`collector-sdk/v1alpha1` JSON field names and do not require a wire-protocol or
fixture-pack version bump.

This table grows one row per qualified SDK/core combination. Do not remove a
row when a newer one is added: a consumer pinned to an older tag still needs
to find its row. Candidate availability by itself is not qualification.

## New repository path mapping

The separate `eshu-hq/eshu-sdk` repository publishes independent Go module
identities. Both first `v0.1.0-rc.1` candidates are available from the source
in [SDK PR #9](https://github.com/eshu-hq/eshu-sdk/pull/9); fresh public-proxy
resolution and the immutable tag origins agree. The later
`collector/v0.2.0-rc.1` and `factschema/v0.1.0-rc.2` tags are published.
Fresh public-proxy proof now exists for factschema `v0.1.0-rc.2`;
collector `v0.2.0-rc.1` proxy proof remains pending. The preferred next collector
candidate is `collector/v0.1.0-rc.2`; its mapping must merge here before
separate tag approval. No new-path pair has a qualified core release or
deployed acceptance.

| Original published module | New module identity | Available first candidate | Other published candidate | Preferred next candidate |
| --- | --- | --- | --- | --- |
| `github.com/eshu-hq/eshu/sdk/go/collector@v0.2.0` | `github.com/eshu-hq/eshu-sdk/collector` | `collector/v0.1.0-rc.1` | `collector/v0.2.0-rc.1` | `collector/v0.1.0-rc.2` (prepared) |
| `github.com/eshu-hq/eshu/sdk/go/factschema@v0.2.0` | `github.com/eshu-hq/eshu-sdk/factschema` | `factschema/v0.1.0-rc.1` | `factschema/v0.1.0-rc.2` | `factschema/v0.1.0-rc.2` (published; proxy verified) |

The new module version histories are independent of the original paths'
`v0.2.0` lines. The preferred next collector candidate is `collector/v0.1.0-rc.2`;
`collector/v0.2.0-rc.1` remains a published historical candidate.
`factschema/v0.1.0-rc.2` is published and
[public-proxy verified](https://github.com/eshu-hq/eshu-sdk/blob/main/docs/evidence/factschema-v0.1.0-rc.2.md).
Use exact candidate pins; the
higher historical collector version makes `@latest` especially unsuitable.
Go named types from the old and new import paths have different identities;
the new paths are not drop-in replacements. Keep exact pins to the original
published versions until the target consumer and core pair qualifies. Never
use `@latest` as migration evidence.

| State | Required evidence | Current recorded status for the new paths |
| --- | --- | --- |
| Candidate available | Reviewed and merged source SHA, green CI, separate tag approval, immutable directory-prefixed tags, and fresh proxy resolution | First RC1s, collector `v0.2.0-rc.1`, and factschema `v0.1.0-rc.2` tagged; fresh proxy proof for collector `v0.2.0-rc.1` pending; factschema `v0.1.0-rc.2` proxy verified. Collector `v0.1.0-rc.2` prepared, not published |
| Qualified consumer/core pair | Exact new module pins, contract and runtime proof against a named core version, including grant-aware conformance for reserved fact kinds | No pair qualified or core range assigned |
| Deployed accepted pair | Matching deployment, reducer, graph, API/MCP, retry, operator, and rollback proof | No deployment accepted |

The expanded first candidate preparation includes current collector tooling,
`TrustedDeletion`, and the optional trusted `conformance.Request.CoreFacts`
API merged in [SDK PR #8](https://github.com/eshu-hq/eshu-sdk/pull/8).
Candidate publication requires reviewed and merged versioned source, green
CI and separate tag approval. This supersedes the earlier limited source preparation;
it does not retarget a published tag or qualify a consumer.

The preferred collector `v0.1.0-rc.2` candidate includes the neutral marker API merged in
[SDK PR #10](https://github.com/eshu-hq/eshu-sdk/pull/10); the additive API
is already in the published historical `v0.2.0-rc.1` candidate. Factschema
`v0.1.0-rc.2` repairs number-preserving typed decoding, including nested
integer lists, merged in [SDK PR #11](https://github.com/eshu-hq/eshu-sdk/pull/11). Schemas, fixture bytes and wire protocols are unchanged by these
changes. Preserve both first RC1 tags and the later published tags. Prepared
collector `v0.1.0-rc.2` is neither tag approval nor a supported consumer/core
combination.

`CoreFacts` validates a caller-supplied authorization snapshot; it does not
load grants or determine authority. The caller must validate live grants,
expiry/revocation, scope, component and schema versions, and authoritative
payload schemas and reducer phases. Adding the exported `Request` field can
break external unkeyed composite literals; use keyed literals. Wire protocols
and conformance report shapes are unchanged. See the
[trusted context contract](https://github.com/eshu-hq/eshu-sdk/blob/49ccbd54ee45b0cbfa23b8092d8f85ed2729dce7/docs/conformance-grants.md).

Core keeps grant, admission, graph, queue, lease, and durable-state ownership.
The original modules retain their support and security policy until consumer
and deployed acceptance authorize a cutover. The
[initial compatibility policy](https://github.com/eshu-hq/eshu-sdk/blob/d6ce994d6709a13b6f2dc797f7b988b7b64a68fc/docs/compatibility.md)
records the earlier limited preparation and schema comparison; it does not
include the later trusted context API.

## Release ownership

Tagging is coordinator-controlled per the repository's `RELEASING.md`: once a
tag is pushed, the Go module proxy caches that module version permanently, so
a mistake cannot be un-pushed. Contributors prepare the release in a PR
(CHANGELOG entry, this compatibility row, schema/fixture proof); a maintainer
cuts the tag after merge with the exact commands in `RELEASING.md`. Never tag
from a worktree or a stale checkout — only from a clean, fast-forwarded
`main`. For the new repository, merge this mapping first, then bind separate
tag approval to its exact reviewed and merged source SHA with green CI. Check
Git tags and the public Go proxy for current candidate availability.

## Version support policy

The supported-minor rules below apply to the original module identities.
A new-path prerelease does not qualify a consumer/core pair or retire original
`v0.2.0` support. That transition still requires the consumer and deployed
acceptance recorded above.

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
For the original paths, removing the monorepo `replace` directives and pinning
the published versions in the compatibility table remains valid. The separate
`eshu-sdk` paths are now mapped above as candidate source; migrate to them only
after their tags are available and the intended consumer/core pair qualifies.
Use exact versions and no workspace or local replacement for that proof.

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
