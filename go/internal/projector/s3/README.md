# S3 projector intents

## Purpose

This package recognizes AWS S3 `s3_bucket_posture` and
`s3_external_principal_grant` facts for one scope generation and builds
reducer intents for the `LOGS_TO` access-logging edge, the
`GRANTS_ACCESS_TO` external-principal edge, and internet-exposure
derivation.

## Ownership boundary

The package owns only S3 trigger selection and reducer-intent values,
including the one logging-target-bucket presence check the LOGS_TO builder
needs. The root `internal/projector` package validates scope-generation
boundaries, constructs and owns the immutable fact lookup, preserves family
order, and owns projection lifecycle, queue writes, retries, and telemetry.
Reducer handlers own payload validation, bucket-policy/ACL joins, graph
materialization, and readiness publication.

## Exported surface

- `BuildLogsToMaterializationReducerIntent` builds the `LOGS_TO`
  access-logging edge intent, anchored to the first posture fact with a
  non-blank `logging_target_bucket`.
- `BuildExternalPrincipalGrantMaterializationReducerIntent` builds the
  `GRANTS_ACCESS_TO` external-principal edge intent from
  `s3_external_principal_grant` facts.
- `BuildInternetExposureMaterializationReducerIntent` builds the
  internet-exposure derivation intent from `s3_bucket_posture` facts.

See `doc.go` for the full godoc contract.

## Dependencies

Builders depend on `internal/projector/intent.FactLookup` and
`internal/projector/intent.ReducerIntent`; this package must not import the
root projector package. `BuildLogsToMaterializationReducerIntent` also
decodes the `s3_bucket_posture` payload through this package's own
`factschema_decode_aws.go` (`sdk/go/factschema` plus `internal/factenvelope`
directly), rather than importing root's classified decode wrapper
(`projectorDecodeError` in root's `factschema_quarantine.go`), because
importing root would create an import cycle (root already imports this
package to dispatch to it). The single caller here only checks `err != nil`,
discarding the decode error's classification, so the direct
`factschema.DecodeS3BucketPosture` call plus fact-kind-labeled wrapping is
behavior-identical for that check. This mirrors the `ec2` child package's own
independent `decodeEC2InstancePosture` copy and `go/internal/reducer`'s own
independent AWS decode wrappers — the repo already keeps decode wrappers per
package rather than shared. The file is named `factschema_decode_aws.go` to
match that repo-wide convention so `scripts/verify-payload-usage-manifest.sh`
(issue #4573), which globs `factschema_decode*.go` files and AST-scans each
function for a `factschema.FactKindXxx` reference, discovers it as a decode
seam.

Two of the three builders — external-principal-grant and internet-exposure —
trigger on the mere presence of a fact and do not decode its payload.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; reducer
handlers retain execution, reconciliation, and graph-write telemetry. Moving
the pure builders adds no queue, storage, graph, span, metric, or log
boundary.

## Gotchas / invariants

- All three builders key their intent's `EntityKey` to
  `aws_resource_materialization:<scope>` — NOT a family-distinct key — because
  they gate on the same generic AWS `CloudResource` canonical-nodes phase the
  S3 bucket nodes commit under, unlike EC2's family-distinct entity keys.
- `BuildLogsToMaterializationReducerIntent` only enqueues when at least one
  posture fact has a non-blank, trimmed `logging_target_bucket`; a
  logging-disabled-only generation enqueues nothing, and an undecodable
  posture fact is treated as no match, not an error.
- `BuildExternalPrincipalGrantMaterializationReducerIntent` and
  `BuildInternetExposureMaterializationReducerIntent` trigger on the mere
  presence of their respective fact kind and anchor to the earliest matching
  fact so the reducer claim is stable across reprojections of the same
  generation.

## Verification

Run the package contract tests, root S3 assembly tests, ordered fan-out
parity and probe-count tests, the projector package tree, package-doc and
path mirrors, dirgate, telemetry coverage, and the golden-corpus gates
selected by the changed paths.

No-Regression Evidence: this extraction moves three builders and their typed
decode wrapper without changing a trigger, a value, or the order they run in.
Reducer intent domains emitted by all three builders are identical to the base
commit, and the dispatcher's ordered fan-out is unchanged at 44 builder probes with
each swap at its original position. `awsCloudRuntimeDriftSourceSystem` and
`codegraphDerefString` were compared body-for-body against their
`projectorintent` and local replacements rather than by name. Focused proof:
`go test ./internal/projector/... -count=1` green, whole-module `go build` and
`go vet` clean, and the B-7 golden corpus gate reports 561 pass /
0 required-fail with the B-12 snapshot byte-identical to main.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
