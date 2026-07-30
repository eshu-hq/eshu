# OCI Registry Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
`oci_registry` fact family. A projector canonical extractor (and the
container-image-identity reducer) never reads `Envelope.Payload["some_key"]`
for these kinds directly; it decodes through the parent `factschema` package's
kind-keyed seam (for example `factschema.DecodeOCIImageManifest`) and receives
one of these structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

Seven OCI registry fact kinds decode through this package. The kinds are DOTTED
(`oci_registry.*`), like the incident family.

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `oci_registry.repository` | `Repository` | `factschema.DecodeOCIRegistryRepository` |
| `oci_registry.image_manifest` | `ImageManifest` | `factschema.DecodeOCIImageManifest` |
| `oci_registry.image_index` | `ImageIndex` | `factschema.DecodeOCIImageIndex` |
| `oci_registry.image_descriptor` | `ImageDescriptor` | `factschema.DecodeOCIImageDescriptor` |
| `oci_registry.image_tag_observation` | `TagObservation` | `factschema.DecodeOCIImageTagObservation` |
| `oci_registry.image_referrer` | `ImageReferrer` | `factschema.DecodeOCIImageReferrer` |
| `oci_registry.warning` | `Warning` | `factschema.DecodeOCIRegistryWarning` |

`oci_registry.warning` is consumed by the container-image-identity retirement
planner — see "The warning consumer" below.

## Ownership boundary

This package owns the Go type definitions and JSON codec for these seven fact
kinds' payloads. It does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_ociregistry.go`). It does not own graph projection; the
projector's OCI canonical extractors (`go/internal/projector`) and the
container-image-identity reducer (`go/internal/reducer`) consume the decoded
structs but live outside this module.

## Fully typed, not polymorphic

Unlike `awsv1.Resource` / `gcpv1.Resource`, these structs carry **no
`Attributes` pass-through**. Every payload key a read path consumes is a named
field. The nested descriptor objects (a manifest's `config` and `layers`, an
index's `manifests`) are typed as the shared `Descriptor` struct
(`descriptor.go`); only `Descriptor.Digest` is read for graph truth today, the
rest are typed for round-trip fidelity.

## Required vs. optional fields, per struct

Field mutability encodes the contract, per Contract System v1 §3.1. A required
field is a non-pointer field with no `omitempty`; the decode seam rejects a
payload that omits it, or supplies an explicit JSON null for it, with a
classified `input_invalid` error naming the field. A present-but-empty value
(for example the empty string) is a valid observed value and decodes normally.

The required set is exactly the identity/join keys whose **absence** today
produces a broken or empty graph identity:

| Struct | Required identity fields | Why |
| --- | --- | --- |
| `Repository` | `RepositoryID` | The repository node UID is `RepositoryID`. |
| `ImageManifest` | `RepositoryID`, `Digest` | The descriptor UID is `oci-descriptor://<repo>@<digest>`; the container-image reducer keys on both. |
| `ImageIndex` | `RepositoryID`, `Digest` | Same descriptor UID fold. |
| `ImageDescriptor` | `RepositoryID`, `Digest` | Same descriptor UID fold. |
| `TagObservation` | `RepositoryID`, `Tag`, `ResolvedDigest` | The observation UID folds all three; `ResolvedDigest` is the tag's join target. |
| `ImageReferrer` | `RepositoryID`, `SubjectDigest`, `ReferrerDigest` | The referrer UID folds all three. |
| `Warning` | `WarningCode` | The collector emitter fails closed on a blank warning code. |

Missing a required identity field dead-letters as `input_invalid` rather than
forming an empty-string graph identity — this is the accuracy fix this package
exists to add. A present-but-empty required field stays a valid decode that the
projector's own identity gate still drops, byte-identical to the pre-typing
behavior.

`DescriptorID` on the digest-addressed kinds is **optional**: the projector
synthesizes it from `(RepositoryID, Digest)` when absent, so its absence must
stay a valid decode, not a dead-letter.

## The warning consumer

`oci_registry.warning` (`Warning`) is decoded by the container-image-identity
reducer before it retires a previously canonical image reference. Active
`config_blob_unavailable`, `tag_list_truncated`, and
`missing_manifest_digest` warnings declare a bounded collector-incompleteness
condition. The retirement planner holds affected config digests and tag
references; `missing_manifest_digest` conservatively holds the repository
because the payload has no scanned-reference field. A malformed active warning
fails the retirement pass closed instead of treating unknown completeness as
an authoritative demotion. The typed decode keeps that accuracy gate on the
versioned payload contract.

## Changing a struct

Any field change here is a payload-schema change.

- **Additive optional field** (new pointer/`omitempty` field): a minor schema
  bump. Add the field, regenerate, and commit the schema in the same change.
- **Remove, rename, or narrow a field**: a major schema bump. It needs a
  conversion shim in the parent package's decode seam (`decode.go`,
  `decodeLatestMajor`), never a silent edit here.

Regenerate after any struct change and refresh the fixture pack copy:

```bash
cd sdk/go/factschema
go run ./internal/schemagen/cmd
cp schema/oci_registry.*.v1.schema.json fixturepack/schema/
```

`schema_gen_test.go`'s `TestSchemasHaveNoDrift` and
`fixturepack_drift_test.go`'s `TestFixturePackSchemasMatchCanonical` fail the
build on drift.

## Telemetry

None. This package has no runtime, network, queue, graph, or telemetry emission
path — it is pure type definitions and a JSON codec.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §3.4 provenance-only kinds, §5 versioning, §7 migration plan.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
- `go/internal/reducer/README.md` — container-image identity publication and
  warning-gated retirement.
