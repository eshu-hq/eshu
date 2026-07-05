# Security Alert Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload struct for the single `security_alert` fact
kind, `RepositoryAlert` (`security_alert.repository_alert`). It must remain
independent from Eshu internals.

`security_alert.repository_alert` is reconciled PROVIDER EVIDENCE, not promoted
graph truth. Type only the reconciliation-evidence fields the reducer reads
today, with the same meaning; do NOT add fields or shapes that would let alert
state flow into `supply_chain_impact` as promoted truth.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing the payload struct's fields, run `go generate ./...` from the
  module root and commit the regenerated schema under `../../schema/` AND its
  copy under `../../fixturepack/schema/`
  (`TestFixturePackSchemasMatchCanonical` locks the two), and keep the
  `../../fixturepack/payloads/security_alert.repository_alert.{valid,invalid}.json`
  fixtures current.
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofmt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by the
  flat-struct convention required fields are also non-pointer, and optional
  fields are pointers or slices carrying `omitempty`. Both the schema generator
  (`../../internal/schemagen`) and the decode seam's required-field check
  (`../../decode.go`) derive that set reflectively from the struct's own tags
  via `../../fields.go`, so there is no hand-maintained key list.
  `TestDerivedKeySetsMatchGeneratedSchemas`, `TestPayloadStructShapeConvention`,
  and `TestSchemasHaveNoDrift` lock the derivations.
- `RepositoryID` is the ONLY required field. It is the repository/provider join
  anchor BOTH reducer consumers key on: the reconciliation read surface
  (`SecurityAlertReconciliationFactFilter`) and the supply-chain-impact seeder
  (`securityAlertCanSeedImpact`, which already required a non-empty
  `repository_id` before an alert could contribute a finding). Do NOT make any
  other field required — two collector paths emit this one kind with different
  field coverage (the per-repo Dependabot envelope sets provider/advisory/
  dependency fields; the org alert-runtime source path additionally sets
  `repository_name`, `source_freshness`, and the `collection_*` coverage
  fields), so requiring any of the others would dead-letter half of this kind's
  real traffic.
- The SINGLE reducer decode site for this kind
  (`extractProviderSecurityAlerts`,
  `go/internal/reducer/security_alert_reconciliation.go`) feeds TWO consumers:
  `BuildSecurityAlertReconciliations` (the reconciliation read surface) and
  `appendSecurityAlertImpactFindings` (the `supply_chain_impact` seeder, a
  CanonicalWrites path). Any change to this struct changes the input to BOTH.
  A change MUST preserve byte-identical reconciliation decisions AND
  byte-identical impact findings for valid facts, and MUST NOT introduce a
  field that promotes alert state into `supply_chain_impact` truth.
- The `cvss` (`map[string]any`), `epss` (`map[string]string`), and `cwes`
  (`[]map[string]string`) container fields model the RAW collector shapes; the
  reducer applies its own trim / drop-empty normalization after decode
  (`security_alert_reconciliation_decode.go`), so this struct must not itself
  prune or reshape them — the decode stays a faithful mirror of the wire
  payload.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A reducer handler receiving it must dead-letter the
  fact rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- This struct carries no polymorphic `Attributes map[string]any` pass-through —
  it is flat and fully closed. Do not add one without discussing scope.
