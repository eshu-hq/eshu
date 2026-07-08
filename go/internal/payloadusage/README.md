# payloadusage

`payloadusage` implements Contract System v1 §6 enforcement gate 2 — the
payload-usage manifest
([design doc](../../../docs/internal/design/contract-system-v1.md#6-enforcement-gates),
[issue #4573](https://github.com/eshu-hq/eshu/issues/4573)).

The schema-diff gate ([#4569](https://github.com/eshu-hq/eshu/issues/4569),
`go/cmd/factschema-diff`) catches a collector breaking the payload shape it
emits. This package catches the **reverse** break: a graph/query/loader handler
starting to require a payload field that no declared schema promises, so the
failure surfaces in core CI instead of an external collector's production run.

## Files

| File | Purpose |
| --- | --- |
| `decodeseam.go` | `ParseDecodeSeams` — finds every `decode<Kind>` function in one `factschema_decode*.go` file and the struct type + fact kind it decodes; `load.go`'s `resolveDecodeFiles` globs every such file so per-family split files are all parsed |
| `structshape.go` | `ParseStructShapes` — extracts a typed struct's named, JSON-tagged fields and required/optional flag |
| `usage.go` | `ScanDecodeUsage` — AST-walks handler files and finds which declared fields each handler actually reads, direct or across a helper-function call |
| `rawpayload.go` | `CheckRawPayloadConvention` — ratchets direct raw payload reads on W2c/W2d loader, relationship, and replay surfaces behind an explicit shrinking exemption list |
| `manifest.go` | `BuildManifest`, `CheckManifest`, `Violation` — joins the three derivations and compares used fields against a declared set |
| `schema.go` | `LoadDeclaredFieldsFromSchemas` — reads `sdk/go/factschema/schema/*.json` as the declared-field source of truth; `MergeRegistryPayloadSchemaFields` — additive hook for issue #4570's registry `payload_schema` refs |
| `load.go` | `Paths`, `ResolvePaths`, `Load`, `Gate`, `MarshalIndent` — the package's top-level entry points |

## Derivation, not string literals

The manifest is derived from the typed `factschema.Decode*` calls that landed
in [#4640](https://github.com/eshu-hq/eshu/pull/4640) — never from a
hand-maintained list of field names. Concretely:

1. `ParseDecodeSeams` parses every `factschema_decode*.go` file under the
   reducer, projector, query, loader (`go/internal/storage/postgres`),
   relationships, and replay surfaces. The reducer glob is fail-closed because
   reducer decode seams are always present; the other surfaces may be empty
   while migrations land. It matches the exact
   `func decode<Kind>(facts.Envelope) (<pkg>.<Struct>, error)` shape, reading
   the `factschema.FactKind*` selector referenced in the body to attribute each
   seam to its wire fact kind.
2. `ParseStructShapes` parses the typed struct packages
   (`sdk/go/factschema/aws/v1`, `sdk/go/factschema/iam/v1`,
   `sdk/go/factschema/incident/v1`) and reads each named field's `json` tag. A
   field is required when it is not a pointer/slice/map and carries no
   `omitempty` — the same rule
   `sdk/go/factschema/decode.go`'s `requiredFields` registration and the
   schema generator use. A field tagged `json:"-"` (the untyped `Attributes`
   pass-through every polymorphic envelope carries) is excluded: it is not a
   declared schema property.
3. `ScanDecodeUsage` AST-walks every non-test file directly under the configured
   reducer, projector, query, loader, relationships, and replay directories and
   records every `ident.Field` selector where
   `ident` was bound to a decoded value — either directly
   (`resource, err := decodeAWSResource(env)` in the same function), or
   because the decoded struct was passed BY VALUE into a helper function
   whose parameter is typed with the qualified struct name (for example
   `func deriveDecision(posture awsv1.S3BucketPosture)` in
   `s3_internet_exposure_rows.go`). The second case is real: several
   AWS/IAM/security-group handlers thread the decoded struct through one or
   two derivation helpers rather than reading every field at the decode call
   site.

`BuildManifest` joins the three into a `Manifest`. `CheckManifest` compares
each kind's used fields against an externally supplied declared-field set and
returns one `Violation` per field a handler reads that the set does not
cover — naming the specific handler file, fact kind, and field.

`Gate` also runs `CheckRawPayloadConvention` against the loader, relationships,
and replay surfaces. It allows the current documented raw reads through a fixed
25-entry exemption budget, skips `factschema_decode*.go` seam files, and fails
on any new `.Payload["field"]` or `payloadString` / `payloadStrings` read. That
turns the W2c/W2d convention into a ratchet: exemptions can be removed as typed
seams land, but adding one requires an explicit budget change in review.

## Limitations / attribution boundary

The usage scan attributes two shapes: direct reads off a decode-call result,
and reads inside a helper whose parameter is typed as the seam struct. It does
**not** attribute a field read mediated only through a **wrapper struct
field**.

The IAM handlers are the concrete case. `iam_can_perform.go` stores a decoded
`iamv1.Permission` in a wrapper and collects the wrappers into a slice:

```go
byPrincipalARN[permission.PrincipalARN] = append(
    byPrincipalARN[permission.PrincipalARN],
    iamPermissionStatement{factID: env.FactID, permission: permission},
)
```

`buildIAMCanPerformGrant([]iamPermissionStatement)` and the escalation
builders then read `statement.permission.Actions`, `.NotActions`,
`.NotResources`, `.HasConditions`, `.Resources`. That is a two-level selector
(`statement.permission.Actions`), and the scan matches only `ident.Field`
where `ident` is bound to a seam value, not `ident.wrapperField.SeamField`.

The same shape applies to `aws_iam_principal`: `secretsIAMRoleCloudResourceUID`
(`go/internal/reducer/secrets_iam_trust_chain_iam_role.go`) reads
`principal.decoded.AccountID` / `.Region` through the `secretsIAMPrincipal`
wrapper.

Consequences:

- The manifest's used-field set is a **lower bound** for `aws_iam_permission`
  and `aws_resource_policy_permission`. Their `actions`, `not_actions`,
  `not_resources`, `has_conditions`, and `resources` reads happen only through
  the wrapper and are currently absent from `UsedFields`.
- `aws_iam_principal`'s `UsedFields` is **empty** today for the same reason,
  even though the handler does read `account_id` and `region` — the strongest
  form of this undercount.
- The gate stays **sound** today: every one of those fields is present in the
  declared JSON Schema (the schemas are generated from the same structs), so
  no violation is missed.
- The one thing it does not cover: a field reachable **only** through a
  wrapper would be a false-green if a schema drifted to drop it.

Extending attribution to single-field (or seam-typed-field) wrapper structs is
tracked as [#4668](https://github.com/eshu-hq/eshu/issues/4668) (follow-up to
this gate, part of epic #4566). It is deliberately out of scope here because no
violation exists today; implementing wrapper-following now would add complexity
with no correctness gain.

## Entry points

`Load(Paths)` runs the full pipeline and returns a `Manifest`. `Gate(Paths)`
runs `Load` and compares the result against
`sdk/go/factschema/schema/*.json` via `LoadDeclaredFieldsFromSchemas`,
returning any `Violation`s after enforcing the raw-payload convention on the
loader, relationships, and replay surfaces. `Paths` fields default relative to
`RepoRoot` through `ResolvePaths`, so most callers only need to supply the
repository root.

## Registry v2 (issue #4570) is additive, not required

Issue #4570 (registry v2) may add `payload_schema` refs to
`specs/fact-kind-registry.v1.yaml`, but it had not landed those refs as of
this gate's initial implementation. This package's declared-field source of
truth is the checked-in JSON Schemas
(`sdk/go/factschema/schema/*.json`), never the registry. If a caller later
wants to widen the declared set with a registry `payload_schema` ref,
`MergeRegistryPayloadSchemaFields` is available: it only WIDENS the declared
set (union), never narrows it — a registry-authoring bug must not fail this
gate for a field the real schema already declares.

## Callers

- `go/cmd/payload-usage-manifest` — the CLI wrapper. `-mode generate` prints
  the manifest as JSON; `-mode gate` runs the check and exits non-zero on any
  violation.
- `go/internal/reducer`'s own `TestPayloadUsageManifest` — the drift-lock test
  this repository's gate command
  (`go test ./internal/reducer -run TestPayloadUsageManifest`) targets. It
  calls `Gate` directly against the real repository paths so a red result is
  investigated from inside the package whose handlers it is checking.

## Dependencies

Standard library only (`go/ast`, `go/parser`, `go/token`, `encoding/json`,
`os`, `path/filepath`). No git, network, Postgres, or graph-backend
dependency — this package only reads Go source and JSON files already on
disk.

## Telemetry

None. This package runs only in local and CI gate contexts, never in a
deployed Eshu process.
