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

## [Unreleased] (proposed `v0.2.0`)

Direction for the next release: additive post-`v0.1.0` changes only — 46 new
schemas, zero breaking schema diffs against tag `sdk/go/factschema/v0.1.0`
(`bash scripts/verify-factschema-diff.sh -base-ref sdk/go/factschema/v0.1.0`
exits 0), so this stays a minor bump per the policy above. See
[SDK Compatibility](../../../docs/public/extend/sdk-compatibility.md) for the
version row this release will fill.

### Added

- Six new typed `<family>/v1` packages with `Decode<Kind>`/`Encode<Kind>`
  seams, schemas, and fixture-pack entries: `codeowners`
  (`codeowners.ownership`), `reducerderived` (governed reducer-owned findings
  such as `reducer_supply_chain_impact_finding`,
  `reducer_aws_cloud_runtime_drift_finding`,
  `reducer_multi_cloud_runtime_drift_finding`, plus package-correlation and
  terraform-drift findings), `scannerworker` (`scanner_worker.analysis`,
  `scanner_worker.warning`), `semantic` (`semantic.code_hint`,
  `semantic.documentation_observation`), `submodule` (`submodule.pin`), and
  `vulnerabilitysuppression` (`vulnerability.suppression`, shared by VEX,
  operator-policy, and provider-dismissal producers).
- `reducer_supply_chain_impact_finding` carries the optional
  `environment_evidence` field (`map[string]string`, `omitempty`), labelling
  each name in `environments` as `deploy_event` or `declared` (issue #5426),
  plus the optional `ci_declared_artifact_digest` and `ci_declared_image_ref`
  fields holding the matched `cicd_run_correlation` deployment's own declared
  artifact identity, baked only on a strong-branch match (issue #5469). All
  three are additive-optional: a finding written before them existed still
  decodes with nil fields.
- New kinds in existing families: `vulnerability.reference` and
  `vulnerability.source_snapshot`; `aws` IAM boundary/policy/attachment/trust,
  DNS, image-reference, and warning kinds; `azure` identity-observation,
  image-reference, resource-change, and tag-observation kinds; `gcp` IAM and
  image-reference kinds; `ci.deployment_event`; Kubernetes RBAC and
  service-account-token kinds; `secrets_iam_coverage_warning`; Vault
  auth-mount, identity, and secret-engine-mount kinds; and `workitem` metadata
  extensions.
- `SchemaBytes` — embedded access to the checked-in `schema/*.json` bytes for
  one fact kind, so out-of-module conformance tests can load a committed
  schema without duplicating the schema tree the way `fixturepack` must.

### Changed

- Core Eshu now generates the adapter that maps durable internal fact envelopes
  into this module's `Envelope` for Decode calls. This does not change any
  factschema payload schema, fixture-pack artifact, or public Go API.
- Go directive `1.26.0` → `1.26.5` (toolchain floor only; no language or
  library break for consumers).

## [0.1.0] - 2026-07-06

First tagged release (`sdk/go/factschema/v0.1.0` at `92061fe39`, fetchable via
the Go module proxy; closes the scaffold started in #4567). Content below is
the module tree at that tag: 130 generated schemas, zero breaking diffs since
only the tag itself is the baseline.

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
