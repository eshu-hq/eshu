# Kubernetes Live Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the `kubernetes_live` fact
family: `PodTemplate`, `PodTemplateContainer`, `Relationship`, `Warning`, and
`Namespace`. It must remain independent from Eshu internals.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go generate ./...` from
  the module root and commit the regenerated schema under `../../schema/`.
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofmt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by the
  flat-struct convention required fields are also non-pointer, and optional
  fields are pointers or slices/maps, carrying `omitempty`. Both the schema
  generator (`../../internal/schemagen`) and the decode seam's required-field
  check (`../../decode.go`) derive that set reflectively from the struct's own
  tags via `../../fields.go`, so there is no hand-maintained key list to keep
  in sync. `TestDerivedKeySetsMatchGeneratedSchemas` locks the two derivations
  to the generated schema, `TestPayloadStructShapeConvention` enforces the
  flat-struct convention, and `TestSchemasHaveNoDrift` keeps every checked-in
  schema in lockstep with its struct.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A reducer handler receiving it must dead-letter the
  fact rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- **No Attributes pass-through in this family**: unlike `awsv1.Resource` /
  `gcpv1.Resource`, none of `PodTemplate`, `Relationship`, `Warning`, or
  `Namespace` is a polymorphic generic envelope. Each fact kind describes one
  fixed observation shape, so every collector-emitted payload key is a named
  field. Do not add an `Attributes map[string]any` catch-all here without
  discussing scope — it would be a shape change for this family, not a
  mirror of the AWS/GCP pattern.
- Only `ObjectID` (`PodTemplate`, `Namespace`), `RelationshipType`/
  `FromObjectID`/`ToObjectID` (`Relationship`), and `Reason`/`ClusterID`
  (`Warning`) are required. Every other field stays optional even though the
  collector emitter (`go/internal/collector/kuberneteslive/envelope.go`)
  unconditionally writes most of them: the emitter can validly write an
  empty string for a cluster-scoped or unlabeled object (for example a
  cluster-scoped resource has no namespace), and the reducer's existing read
  path (`kubernetesWorkloadNodeRow`, `workloadImageRefs`,
  `ingestRelationship`, `ingestWarning` in `go/internal/reducer`) already
  tolerates an absent or empty value for all of them. Making one of these
  required would dead-letter a valid fact — do not "round up" to required
  just because the emitter usually writes a field.
- This package defines four fact kinds (`kubernetes_live.pod_template`,
  `kubernetes_live.relationship`, `kubernetes_live.warning`, and
  `kubernetes_live.namespace`). Adding a fifth kind or a `v2` major is
  follow-on epic work, not a casual edit.
- `Namespace.Annotations` is a RESERVED field slot for #5444
  (ArgoCD-destination evidence). #5434's scope is labels only — the
  collector never populates `Annotations` today (always nil/absent on the
  wire) because annotations may carry operator-authored or sensitive data
  that has not been through a redaction review. Do not start populating it
  without first extending `client_redaction_test.go`-style coverage for
  whatever annotation keys #5444 decides to collect.
- `PodTemplateContainer.EnvKeys` carries environment variable NAMES only —
  never values. Do not add a values field; it would violate the collector's
  redaction contract (see `go/internal/collector/kuberneteslive/doc.go` /
  `envelope.go`'s `ContainerSummary` comment).
- `PodTemplateContainer.EnvFromSecret` is a redaction-safe BOOLEAN existence
  flag (it reports only that a container references secret-backed env, never a
  value), and the real in-tree collector emits it. But the `env_from_secret`
  key name matches the `sdk/go/collector` conformance redaction scanner's
  `secret` substring heuristic (`validation.go` `sensitiveQueryPattern`), which
  cannot distinguish a safe existence flag from a secret value. The in-tree
  collector builds `facts.Envelope` directly and never runs that SDK scanner,
  so it emits the key fine; but the fixture-pack conformance test
  (`TestFixturePackPayloadsAgreeWithConformance`) DOES run the scanner against
  every fixture. The `kubernetes_live.pod_template.valid.json` fixture
  therefore deliberately OMITS `env_from_secret` from its example container —
  the field stays optional in the struct and its decode is covered by the
  round-trip tests. Do not add `env_from_secret` back to the fixture without
  first resolving the scanner false-positive (tracked as a follow-up); it
  would re-red the conformance gate.
