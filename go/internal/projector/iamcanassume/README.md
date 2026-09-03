# IAM CAN_ASSUME projector intents

## Purpose

This package recognizes AWS `aws_iam_permission` trust statements for one
scope generation and builds the reducer intent that asks the reducer to
project those statements into canonical `CAN_ASSUME` edges between committed
IAM `CloudResource` nodes (issue #1134, PR 2 of the trust-graph design).

## Ownership boundary

The package owns only the trust-statement trigger selection, the typed decode
that selection needs, and the reducer-intent value. The root
`internal/projector` package validates scope-generation boundaries, constructs
and owns the immutable fact lookup, preserves family order, and owns
projection lifecycle, queue writes, retries, and telemetry. The reducer's
`DomainIAMCanAssumeMaterialization` handler owns principal resolution, the
canonical-nodes readiness check, the backend-neutral edge write, and readiness
publication.

## Exported surface

- `BuildIAMCanAssumeMaterializationReducerIntent` builds the
  `iam_can_assume_materialization` intent, anchored to the first
  `aws_iam_permission` fact in the generation whose payload decodes and
  carries `policy_source == "trust"`.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. The trust predicate decodes the
`aws_iam_permission` payload through this package's own
`factschema_decode_iam.go` (`sdk/go/factschema` plus `internal/factenvelope`
directly), rather than root's classified decode wrapper
(`newProjectorDecodeError` in root's `factschema_quarantine.go`). The single
caller only checks `err != nil`, discarding the decode error's
classification, so the direct `factschema.DecodeAWSIAMPermission` call plus
fact-kind-labeled wrapping is behavior-identical for that check. This mirrors
`ec2`'s `decodeEC2InstancePosture` and `go/internal/reducer`'s own
independent `decodeAWSIAMPermission` copy: the repo keeps decode wrappers per
package rather than shared. The file keeps the `factschema_decode_*.go` name
so `scripts/verify-payload-usage-manifest.sh` (issue #4573), which globs
`factschema_decode*.go` under `go/internal/projector` and AST-scans each
function for a `factschema.FactKindXxx` reference, still discovers it as a
decode seam at its new path.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains the `reducer.iam_can_assume_materialization` span,
the `eshu_dp_iam_can_assume_edges_total` counter, and the
`iam_can_assume_nodes_not_ready` failure class. A permission fact that fails
decode is skipped as a candidate with no operator-visible dead-letter, exactly
as before the move. Moving the pure builder and its decode wrapper adds no
queue, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- The `EntityKey` is `aws_resource_materialization:<scope>` — NOT a
  family-distinct key. The edge handler's readiness gate resolves the exact
  `GraphProjectionPhaseCanonicalNodesCommitted` row the AWS resource node
  builders publish for the same scope generation, so trust edges never
  project before the IAM role and user nodes commit. The S3, RDS,
  workload-cloud, AWS relationship, and AWS cloud-image builders share the
  same key for the same reason.
- The builder anchors to the earliest decodable trust statement in original
  input order so the reducer claim is stable across reprojections of the same
  generation. An identity-policy statement ahead of it is skipped; a
  trust-source fact whose payload fails decode (for example a missing
  `principal_arn`) is skipped too, and the scan continues rather than failing
  the build. An identity-only generation enqueues nothing.
- `iamCanAssumePolicySourceTrust` duplicates the collector's
  `IAMPolicySourceTrust` string so the projector does not import the collector
  package for one constant; change both together.
- `SourceSystem` is `SourceRef.SourceSystem` trimmed, falling back to a
  trimmed `CollectorKind`; a blank source ref does not drop the intent.
- The projector never writes a `CAN_ASSUME` edge itself; a trust statement
  whose principals are not yet canonical nodes is deferred by the reducer's
  readiness gate, not fabricated here.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
the projector package tree, package-doc and path mirrors, dirgate, telemetry
coverage, the payload-usage manifest gate, and the golden-corpus gates
selected by the changed paths.

No-Regression Evidence: this extraction moves one builder and its decode
wrapper without changing the trigger, value, or fan-out position. The reducer
intent domain, entity key, reason, anchor selection, decode-skip behavior, and
source-system derivation are identical to the base commit, and the
dispatcher's ordered fan-out is unchanged at 44 builder probes with this probe
still running immediately after `codefunctionsummary.BuildCodeFunctionSummaryReducerIntent` and
immediately before `s3.BuildLogsToMaterializationReducerIntent`. The root
`awsCloudRuntimeDriftSourceSystem` helper it called was compared body-for-body
against its `projectorintent.SourceSystem` replacement (both trim
`SourceRef.SourceSystem` and fall back to a trimmed `CollectorKind`), and the
root `firstOfKindMatching` forwarder was a direct delegate to
`projectorintent.FactLookup.FirstOfKindMatching`, so both substitutions are
behavior-identical by construction; the root helper stays at root because
five other root builders still call it. The root `decodeAWSIAMPermission`
wrapper had this builder as its only caller, so it moved along and root keeps
no copy. Focused proof:
`../scripts/go-test-run-guard.sh 1 'TestBuildIAMCanAssumeMaterializationReducerIntent' -- ./internal/projector/iamcanassume -count=1`
(run from the `go/` module root, which is what both the `../scripts/` prefix
and the `./internal/...` package path assume; the guard is used rather than a
bare `go test -run` because a pattern matching nothing exits 0)
and `go test ./internal/projector/... -count=1` green, whole-module `go build`
and `go vet` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [IAM CAN_ASSUME trust graph design](../../../../docs/internal/design/1134-iam-can-assume-trust-graph.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
