# AGENTS.md - internal/payloadusage guidance

## Read first

1. `README.md` - derivation pipeline, entry points, and why registry v2 is
   additive here.
2. `doc.go` - package-level godoc summary.
3. `load.go` - `Paths`/`ResolvePaths`/`Load`/`Gate`, the top-level entry
   points every caller (the CLI, the reducer drift-lock test) uses.
4. `docs/internal/design/contract-system-v1.md` §6 (enforcement gates) - the
   design contract this package implements (gate 2).

## Invariants

- This package is build-time/CI-time only. Do not add runtime storage,
  graph, network, or telemetry dependencies.
- Every parse function (`ParseDecodeSeams`, `ParseStructShapes`,
  `ScanDecodeUsage`) reads Go source via `go/parser`/`go/ast` — never via
  reflection over an imported package. `go/internal/payloadusage` MUST NOT
  import `go/internal/reducer` as a Go package: the reducer package's decode
  functions are unexported, and importing it would create the wrong
  dependency direction (a generator tool depending on the business-logic
  package it inspects). Treat `go/internal/reducer` and
  `sdk/go/factschema/{aws,iam}/v1` purely as filesystem paths to parse.
- A struct field tagged `json:"-"` (the untyped `Attributes` pass-through
  every polymorphic AWS envelope carries) MUST stay excluded from
  `StructShape.Fields`. It is not a declared schema property; including it
  would make the gate flag every `Attributes[...]` map-index read as an
  "undeclared field," which is not the break this gate exists to catch.
- `ScanDecodeUsage` MUST follow a decoded struct across a function-call
  boundary when it is passed BY VALUE into a helper parameter typed with the
  seam's qualified struct name (see `usage.go`'s `recordParameterBindings`).
  This is not an edge case: `s3_internet_exposure_rows.go`'s
  `deriveS3InternetExposureDecision`/`deriveS3PublicPolicyDecision` are real,
  already-migrated handlers that only read several `awsv1.S3BucketPosture`
  fields this way. Regressing this to direct-call-site-only tracking silently
  drops real usage from the manifest without any test failure unless the
  cross-function fixture test (`TestScanDecodeUsageFollowsStructValuePassedToHelperFunction`
  in `usage_test.go`) is kept and kept green.
- `LoadDeclaredFieldsFromSchemas` is this gate's declared-field source of
  truth. Do NOT wire `specs/fact-kind-registry.v1.yaml` as a hard requirement
  — issue #4570 (registry v2) owns that file's schema shape and may not have
  landed `payload_schema` refs yet. `MergeRegistryPayloadSchemaFields` exists
  for the day it does, and it is additive-only (union, never narrows) by
  design; do not change it to intersect or replace.
- Keep every file under the repo's 500-line cap. The package is already split
  along its natural seams (seam parsing / struct parsing / usage scanning /
  manifest join+compare / schema loading / top-level orchestration) — add a
  new file rather than growing an existing one past that boundary.

## Common changes

- **A ninth (or later) fact kind is migrated to typed decode**: no code
  change is needed here as long as the new `decode<Kind>` function matches
  the exact shape `ParseDecodeSeams` expects
  (`func decodeX(facts.Envelope) (<pkg>.<Struct>, error)` referencing a
  `factschema.FactKind*` constant in its body) and its schema file follows
  the `<fact_kind>.v1.schema.json` naming convention. If the schema file name
  does not follow that convention, add the mapping to `schema.go`'s
  `factKindSchemaFile` — `Load`/`Gate` fail loudly via
  `UnmappedSeamFactKinds` when a seam has no mapping, so a forgotten mapping
  is a startup error, not a silent gap.
- **A new struct-family directory** (a family beyond `aws/v1`/`iam/v1`): add
  a `ParseStructShapes(dir, "<alias>v1")` call in `load.go`'s `Load` and merge
  its result into the combined `shapes` map, matching the existing
  `awsShapes`/`iamShapes` pattern.
- **New violation detail** (for example distinguishing "required" vs
  "optional" undeclared fields): extend `Violation` in `manifest.go` and
  `CheckManifest`'s construction of it; keep `Violation.String()` naming the
  handler file, fact kind, and field, per issue #4573's acceptance criterion.
