# Fact Schema Contracts Agent Rules

This directory is a standalone public Go module for versioned
collector-reducer payload contracts (Contract System v1 §3.1). It must
remain independent from Eshu internals, mirroring `sdk/go/collector`'s
`AGENTS.md`.

## Required Checks

- Read the root `AGENTS.md` and `docs/internal/agent-guide.md` before edits.
- Keep `go.mod` as a standalone module.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`.
- Update `README.md`, `doc.go`, the generated JSON Schema under `schema/`,
  and `decode_test.go` / `schema_gen_test.go` when changing a payload
  struct's shape.
- Run `go generate ./...` and commit the result whenever a payload struct
  changes; `schema_gen_test.go` fails the build on drift.
- Run `go test ./... -count=1` from this directory.
- Run `gofmt` for changed Go files and `git diff --check` from the repo
  root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by the
  flat-struct convention required fields are also non-pointer and optional
  fields are pointers carrying `omitempty`. Both the schema generator
  (`internal/schemagen`) and the decode seam's required-field check
  (`decode.go`) derive that set reflectively from the struct's own tags via
  `fields.go` — there is no hand-maintained per-kind key list. Do not
  reintroduce one when adding a fact kind; register the kind in
  `payloadContracts` (`decode_test.go`) instead so the drift tests cover it.
- A required field **absent** from a payload map is a classified
  `*DecodeError` (`ClassificationInputInvalid`) naming the field, never a
  zero-value struct. A present-but-empty required field decodes
  successfully — do not conflate "absent" with "empty."
- `ClassificationInputInvalid` is this module's own constant. Do not import
  `go/internal/projector`'s dead-letter triage classes; the reducer maps by
  string value instead.
- The reducer only ever decodes the **latest** struct for a fact kind;
  version shims for older schema majors live in this module's decode
  functions, never in reducer handler code.
- Do not add envelope unification (aliasing/generating `Envelope` from
  `go/internal/facts.Envelope` or `sdk/go/collector.Fact`) here — it is
  documented follow-up work in `README.md` and design §3.1/§7, out of scope
  for this scaffold.
- This module carries the AWS/IAM/security-group fact family (`aws_resource`,
  `aws_relationship`, `aws_security_group_rule`, `ec2_instance_posture`,
  `s3_bucket_posture`, `aws_iam_permission`, `aws_resource_policy_permission`,
  `aws_iam_principal`), the incident family (`incident.record`,
  `incident.lifecycle_event`, `change.record`,
  `incident_routing.applied_pagerduty_resource`,
  `incident_routing.applied_alert_route`,
  `incident_routing.observed_pagerduty_service`,
  `incident_routing.observed_pagerduty_integration`,
  `incident_routing.coverage_warning`), and (among others added by later
  waves — see `gcp/v1`, `azure/v1`, `kuberneteslive/v1`, `ociregistry/v1`,
  `terraformstate/v1`, `packageregistry/v1`, `vulnerability/v1`, `cicdrun/v1`,
  `secretsiam/v1`, `workitem/v1`, `securityalert/v1` READMEs) the
  sbom_attestation family (`sbom.document`,
  `sbom.component`, `sbom.dependency_relationship`, `sbom.external_reference`,
  `sbom.warning`, `attestation.statement`,
  `attestation.signature_verification`, `attestation.slsa_provenance` — see
  `sbom/v1/README.md` for which are wired versus typed-but-deferred), and the
  security_alert family (`security_alert.repository_alert` — one kind, see
  `securityalert/v1/README.md`; its single reducer decode site feeds both the
  reconciliation read surface and the supply_chain_impact seeder), and the
  observability family (`observability.declared_*` ×9,
  `observability.applied_resource`, `observability.applied_sync_state`,
  `observability.observed_*` ×5, `observability.coverage_warning`,
  `observability.source_instance` — eighteen kinds across a git-declared lane
  and a live observed/applied lane, all sharing `source_instance_id` as the
  required identity anchor; see `observability/v1/README.md` for the
  per-kind required set). When you
  add a new kind, add its typed
  `sbom/v1/README.md` for which are wired versus typed-but-deferred) and the
  documentation family (`documentation_source`, `documentation_document`,
  `documentation_section`, `documentation_link`,
  `documentation_entity_mention`, `documentation_claim_candidate`,
  `documentation_finding`, `documentation_evidence_packet` — see
  `documentation/v1/README.md` for which are wired versus typed-but-deferred,
  and note `documentation_section` carries its own schema version distinct
  from the rest of the family). When you add a new kind, add its typed
  struct under `<family>/v1` (its required set is whatever the struct's own json
  tags declare — there is no separate registration step), its
  `Decode<Kind>`/`Encode<Kind>` and `FactKind<Kind>` in the family's
  `decode_<family>.go`, a schemagen entry, a `schema/<kind>.v1.schema.json`
  artifact, and a `payloadContracts` row (`decode_test.go`) so the drift tests
  cover it. `TestPayloadContractsCoverAllSchemas`,
  `TestDerivedKeySetsMatchGeneratedSchemas`, `TestPayloadStructShapeConvention`,
  `TestSchemasHaveNoDrift`, and the reducer-side
  `TestFactSchemaKindsMatchWireFactKinds` drift lock all fail until the new kind
  is wired consistently.
- Fact-kind constant VALUES are the exact wire strings the collector emits and
  the reducer loads (`go/internal/facts.*FactKind`). Most are
  underscore-separated (`aws_resource`); the incident family is DOTTED
  (`incident.record`). The reducer-side drift-lock test asserts each
  `FactKind<Kind>` equals its `facts.*FactKind` counterpart. "Never invent a
  namespaced or dotted value" means never ADD dotting a wire kind does not
  already have — when the wire kind IS dotted, MATCH it exactly and do not
  rename it to underscores. The schema filename is the dotted kind plus
  `.v1.schema.json` (`incident.record.v1.schema.json`); a dot in a filename is
  valid and needs no transform in schemagen, `payloadContracts`, or the diff
  tooling.
- Some incident payload fields are read only by raw-SQL-JSONB loaders in
  `go/internal/storage/postgres` (`incident_repository_correlation_loader.go`,
  `service_incident_evidence_loader.go`), which the #4573 payload-usage manifest
  gate cannot see (it scans reducer decode calls only). Those fields MUST still
  be declared in the incident schemas; the reducer-side
  `TestIncidentRoutingSQLProjectedFieldsAreSchemaDeclared` locks that coverage so
  a dropped field fails the build instead of silently breaking the SQL read.
- `aws_resource` and `aws_relationship` are polymorphic envelopes: they type
  their identity + common fields and pass service/verb-specific fields through an
  untyped `Attributes map[string]any` (custom Marshal/Unmarshal, open-object
  schema). Fully typed kinds keep a closed schema. Per-resource_type /
  per-relationship_type attribute typing is deferred follow-up work, not a gap.
- The `file` kind's `parsed_file_data` field is the same open-object story one
  level down: `codegraphv1.File.ParsedFileData` stays an open `map[string]any`,
  and its INNER keys are typed incrementally (issue #4750) through the
  `DecodeParsedFileData*` accessors (`decode_parsed_file_data.go`) over structs
  in `codegraph/v1/parsed_file_data.go`. Each accessor decodes ONE inner key; a
  typed inner struct carrying producer fields no consumer reads uses an open
  `Attributes map[string]any` pass-through. These inner structs are NOT fact
  kinds — no `payloadContracts` row, no `schema/` artifact, no schemagen entry —
  so they never change the `file.v1.schema.json` wire schema. Do not narrow
  `ParsedFileData` itself, and do not type the wide per-language AST buckets
  (`imports`, `functions`, `function_calls`, `classes`, `variables`,
  `framework_semantics`) ahead of their own #4750 increment.
