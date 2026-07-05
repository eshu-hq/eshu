# CI/CD Run Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the six reducer-consumed
`ci_cd_run` fact kinds: `Run`, `Artifact`, `EnvironmentObservation`,
`TriggerEdge`, `Step`, and `WorkflowImageEvidence`. It must remain independent
from Eshu internals.

Three emitted fact kinds (`ci.job`, `ci.pipeline_definition`, `ci.warning`) are
intentionally NOT typed here — no reducer or storage decode call reads them
today (`cicdRunCorrelationFactKinds()` in
`go/internal/reducer/ci_cd_run_correlation.go` does not even load `ci.job` or
`ci.pipeline_definition`). Do NOT add structs, schemas, or `Decode` functions
for those three until the change that converts their read-side consumer; they
migrate WITH that surface (Contract System v1 §7).

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go generate ./...` from
  the module root and commit the regenerated schema under `../../schema/`
  AND its copy under `../../fixturepack/schema/`
  (`TestFixturePackSchemasMatchCanonical` locks the two).
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofmt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by the
  flat-struct convention required fields are also non-pointer, and optional
  fields are pointers or slices, carrying `omitempty`. Both the schema
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
- `Provider` + `RunID` (and, transitively, `RunAttempt`, which stays optional
  because the reducer/collector both default an absent value to `"1"`) are the
  reducer's ENTIRE run join key across `Run`, `Artifact`,
  `EnvironmentObservation`, `TriggerEdge`, and `Step`
  (`cicdRunKey`/`go/internal/reducer/ci_cd_run_correlation.go`). Do not make
  any other field on those five structs required without first checking
  whether the reducer's read path treats an absent value as a valid "no
  evidence" observation (most do — see each struct's own field-level
  godoc) rather than a malformed fact.
- `WorkflowImageEvidence.RepositoryID` is the ONLY required field on that
  struct: it is the sole join key `attachWorkflowImagesToRuns`
  (`go/internal/reducer/ci_cd_run_correlation_workflow_image.go`) uses. Do not
  make `EvidenceClass` or `ImageRef` required — the reducer's own read path
  already treats any `EvidenceClass` other than `"workflow_image_ref"` (or an
  absent one) as "not a resolvable single ref," a valid observation, not
  malformed input.
- `Step.Result`/`Run.Result` (the provider's CI conclusion, e.g. "success")
  must NEVER be treated as deployment truth anywhere a consumer of this
  package feeds a decoded struct into reducer logic — the reducer's Golden
  Rules forbid turning CI success or shell text into deployment truth on
  their own; a `DeploymentHintSource=="shell"` `Step` explicitly REJECTS a
  run's correlation rather than trusting it
  (`classifyCICDRunEvidence`/`ci_cd_run_correlation.go`).
- None of the six structs here carry a polymorphic `Attributes map[string]any`
  pass-through — every kind is flat and fully closed. Do not add one without
  discussing scope; it would be a first for this family.
- `WorkflowImageEvidence` is emitted by a DIFFERENT collector
  (`go/internal/collector/git_workflow_image_facts.go`, the git collector's
  static workflow-file scanner) than the other five structs in this package
  (`go/internal/collector/cicdrun`, the ci_cd_run collector's GitHub Actions
  provider path). It shares the `ci.workflow_image_evidence` fact kind and the
  `ci_cd_run` schema version (`facts.CICDSchemaVersion`) and the reducer's
  `ci_cd_run_correlation` domain reads both origins together, which is why it
  lives here rather than in a separate package — verify BOTH emitters
  (`go/internal/collector/git_workflow_image_facts.go` and
  `go/internal/workflowimage/extract.go`'s `Evidence` struct) before changing
  its required/optional field set.
- This package defines six fact kinds. Typing one of the three deferred kinds
  (see the top of this file) or a `v2` major is follow-on work gated on
  converting the read path, not a casual edit.
