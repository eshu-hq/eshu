# OCI Registry Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the `oci_registry` fact family:
`Repository`, `ImageManifest`, `ImageIndex`, `ImageDescriptor`,
`TagObservation`, `ImageReferrer`, and `Warning`, plus the shared nested
`Descriptor`. It must remain independent from Eshu internals.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go run ./internal/schemagen/cmd`
  from the module root and commit the regenerated schema under `../../schema/`,
  AND refresh the fixture-pack copy:
  `cp ../../schema/oci_registry.*.v1.schema.json ../../fixturepack/schema/`.
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofmt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by the
  flat-struct convention required fields are also non-pointer and optional
  fields are pointers or slices/maps carrying `omitempty`. Both the schema
  generator (`../../internal/schemagen`) and the decode seam's required-field
  check (`../../decode.go`) derive that set reflectively from the struct's own
  tags via `../../fields.go`. `TestDerivedKeySetsMatchGeneratedSchemas`,
  `TestPayloadStructShapeConvention`, and `TestSchemasHaveNoDrift` lock the two
  derivations to the generated schema, and the reducer-side
  `TestFactSchemaKindsMatchWireFactKinds` locks each `FactKind*` constant to its
  `go/internal/facts.*FactKind` wire counterpart.
- **The required set is identity-only.** Mark a field required ONLY when its
  ABSENCE today produces a broken or empty graph identity (see the README table:
  `RepositoryID`, `Digest`, `Tag`, `ResolvedDigest`, `SubjectDigest`,
  `ReferrerDigest`, `WarningCode`). A field that today tolerates an empty value
  stays optional — flipping a present-but-empty value into a dead-letter is an
  accuracy regression, not a fix.
- `DescriptorID` is OPTIONAL on the digest-addressed kinds: the projector
  synthesizes it from `(RepositoryID, Digest)` when absent, so its absence must
  stay a valid decode.
- These kinds are FULLY TYPED closed structs, NOT polymorphic envelopes. Do not
  add an `Attributes map[string]any` pass-through. Every payload key a read path
  consumes is a named field. Nested descriptor objects use the shared
  `Descriptor` struct.
- The fact-kind constant VALUES are the exact wire strings the collector emits
  and the projector/reducer load (`go/internal/facts.*FactKind`). They are
  DOTTED (`oci_registry.repository`), like the incident family. The schema
  filename is the dotted kind plus `.v1.schema.json`; a dot in a filename is
  valid and needs no transform.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- **`Warning` is DEFERRED — typed but not consumed.** No projector or reducer
  read path decodes `oci_registry.warning` today (design §3.4). Keep the struct,
  schema, fixturepack entry, and registry `payload_schema` ref so the kind is
  contract-complete, but do NOT wire a decode site, `input_invalid` regression
  test, or benchmark for it — there is no read path to convert. It migrates its
  decode site WITH its future consumer, matching the gcp wave's deferred
  `gcp_image_reference` / `gcp_tag_observation`. If you add a consumer, convert
  its decode site and its accuracy proof in that change.
- The reducer and projector decode only the latest struct per fact kind.
  Older-schema-major shims live in the parent package's `decodeLatestMajor`,
  never here or in handler code.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1/§3.2/§3.4/§5/§7.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` and `AGENTS.md`.
- `../gcp/v1/AGENTS.md` — the gcp family whose deferred-kind pattern this
  package's `Warning` mirrors.
