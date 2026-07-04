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

- Required payload fields are non-pointer struct fields with no `omitempty`
  tag; optional fields are pointers or carry `omitempty`. Both the schema
  generator (`internal/schemagen`) and the decode seam's required-field
  check (`decode.go`) must keep deriving from the same struct shape.
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
- This module now carries the whole AWS/IAM/security-group fact family
  (`aws_resource`, `aws_relationship`, `aws_security_group_rule`,
  `ec2_instance_posture`, `s3_bucket_posture`, `aws_iam_permission`,
  `aws_resource_policy_permission`, `aws_iam_principal`), not a single sample
  kind. When you add a new kind, add its typed struct under
  `<family>/v1`, its `Decode<Kind>`/`Encode<Kind>` and `FactKind<Kind>` in the
  family's `decode_<family>.go`, its `registerRequiredFields` call in that
  file's `init` (never a central map literal), a schemagen entry, and a
  `schema/<kind>.v1.schema.json` artifact. `TestRequiredFieldsMatchStructShape`,
  `TestSchemasHaveNoDrift`, and the reducer-side
  `TestFactSchemaKindsMatchWireFactKinds` drift lock all fail until the new kind
  is wired consistently.
- Fact-kind constant VALUES are the exact wire strings the collector emits and
  the reducer loads (`go/internal/facts.*FactKind`, underscore-separated). The
  reducer-side drift-lock test asserts each `FactKind<Kind>` equals its
  `facts.*FactKind` counterpart, so never invent a namespaced or dotted value.
- `aws_resource` and `aws_relationship` are polymorphic envelopes: they type
  their identity + common fields and pass service/verb-specific fields through an
  untyped `Attributes map[string]any` (custom Marshal/Unmarshal, open-object
  schema). Fully typed kinds keep a closed schema. Per-resource_type /
  per-relationship_type attribute typing is deferred follow-up work, not a gap.
