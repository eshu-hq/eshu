# Container-image-identity projector intents

## Purpose

This package recognizes container-image identity evidence in one scope
generation and builds the reducer intent that asks the reducer to run its
cross-source, digest-first `container_image_identity` join. The trigger set
is deliberately wide because identity evidence arrives from many collector
scopes: OCI registry manifests/indexes/tags/referrers, AWS/Azure/GCP
image-reference facts, an AWS relationship targeting a container image, a
CI/CD container-image artifact, static CI/CD workflow-image evidence, a Git
content-entity carrying image references, a repository Dockerfile (added,
edited, or removed), a signed SLSA provenance statement, and a
signature-verification result. Only the earliest accepted fact per generation
matters; the reducer reloads the full generation itself once triggered.

## Ownership boundary

The package owns only trigger selection and the reducer-intent value. The
root `internal/projector` package validates scope-generation boundaries,
constructs and owns the immutable fact lookup, preserves family order, and
owns projection lifecycle, queue writes, retries, and telemetry. The
reducer's `DomainContainerImageIdentity` handler
(`go/internal/reducer/container_image_identity.go` and its sibling files)
owns the cross-source join, tier ranking, decision classification
(exact/derived/ambiguous/unresolved/stale), retirement, and the
`BUILT_FROM`/`DERIVED_FROM` graph writes; none of that happens here.

## Exported surface

- `BuildContainerImageIdentityReducerIntent` builds the
  `container_image_identity` intent, anchored to the earliest accepted fact
  across the candidate kinds in original input order.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package — root already imports this package to dispatch to
it, so the reverse import would cycle. It reads `internal/facts` for the
fact-kind constants and `internal/reducer` for the domain constant.

This family carries one decode seam: `factschema_decode_aws.go` decodes an
`aws_relationship` envelope through `sdk/go/factschema.DecodeAWSRelationship`
to read the optional `TargetType` field. Root's own classified wrapper of the
same seam had this trigger as its only caller, so it moved out entirely
(the `iamcanassume` extraction precedent) rather than staying as dead code;
the payload-usage manifest gate requires a unique function name per
`factschema_decode*.go` file, so this package's copy carries a
family-prefixed name instead of root's original one. Every other
trigger branch reads only envelope-level fields (`FactKind`, `FactID`,
`SourceRef`, `CollectorKind`, `IsTombstone`) or raw payload keys through a
package-local `payloadString` copy (`payload.go`, mirroring root's
`payload.go` helper of the same name) — never a second typed decode.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains `eshu_dp_reducer_executions_total`,
`eshu_dp_reducer_run_duration_seconds`,
`eshu_dp_container_image_identity_decisions_total`, and
`eshu_dp_container_image_identity_retirements_total` for its own execution
and decisions. Moving the pure builder adds no queue, storage, graph, span,
metric, or log boundary.

## Gotchas / invariants

- The candidate-kind list (`candidateFactKinds`) must stay in sync with
  `triggerFact`'s switch — it exists so `FirstAcrossKinds` can skip kinds the
  generation does not carry before evaluating the predicate, not to change
  admission. Every kind `triggerFact` returns true for must appear in the
  list.
- The `aws_relationship` branch triggers only when the decoded `TargetType`
  equals `"container_image"`; an undecodable envelope or a nil/absent
  `TargetType` is not a trigger and not an error — the decode error is
  discarded, matching the pre-extraction root behavior of comparing a
  possibly-empty dereferenced string.
- The `file` branch is narrow by design: every repository generation carries
  `file` facts, so only a Dockerfile (by parsed `dockerfile_stages`,
  declared `language`, or file name) or a tombstoned
  `.github/workflows/*.yml|yaml` file may trigger — never an arbitrary
  source file. A tombstoned Dockerfile with no `parsed_file_data` still
  triggers through the name/language fallback so the reducer's
  retract-first pass can clear a stale `DERIVED_FROM` edge (#5460).
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`
  (trimmed `SourceRef.SourceSystem`, else trimmed `CollectorKind`). The
  pre-extraction root helper (`containerImageIdentitySourceSystem`) had the
  identical two-tier body, so this is NOT a behavior change — do not
  reintroduce a package-local copy.
- `containerImageIdentityFileFactKind` duplicates root's
  `FactKindFileObserved` (`"file"`, `go/internal/projector/stage_facts.go`)
  as a literal rather than an import, because root imports this package to
  dispatch and the reverse direction would cycle.
- Workflow evidence uses this identity intent because static
  `ci.workflow_image_evidence` is emitted in the Git repository scope; the
  durable identity-completion chain then reopens `ci_cd_run_correlation`
  after the workflow generation becomes active
  (`cicdruncorrelation.BuildCICDRunCorrelationReducerIntent`).
- Do not decode a second payload field beyond `aws_relationship.TargetType`,
  and do not check a schema version here; the reducer handler owns typed
  decode for its own evidence loads and schema-version admission stays with
  root projection.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Run the package contract tests, the root dispatcher projection tests, ordered
fan-out parity and probe-count tests, the projector package tree, the focused
`TestRouteServesDataRegistry` run, package-doc and path mirrors, dirgate,
telemetry coverage, and the golden-corpus gates selected by the changed
paths.

No-Regression Evidence: this extraction moves one builder without changing
its trigger, value, or fan-out position. The reducer intent domain, entity
key, reason string, candidate-kind set, and source-system derivation are
identical to the base commit, and the dispatcher's ordered fan-out is
unchanged at 44 builder probes with this probe still running immediately
after `projectors3.BuildInternetExposureMaterializationReducerIntent` and
immediately before `cicdruncorrelation.BuildCICDRunCorrelationReducerIntent`.
The family carried one private helper,
`containerImageIdentitySourceSystem`, checked body-for-body against
`projectorintent.SourceSystem` and found identical (trim
`SourceRef.SourceSystem`, else trim `CollectorKind`, no third tier), so the
substitution is behavior-identical by construction and it was dropped rather
than moved; the child test
`TestBuildContainerImageIdentityReducerIntentSourceSystemFallsBackToCollectorKind`
pins both tiers. The `aws_relationship` decode call was moved to a local
`decodeContainerImageIdentityAWSRelationship` wrapper
(`factschema_decode_aws.go`) that calls the same
`factschema.DecodeAWSRelationship` seam root's dropped
`decodeAWSRelationship` called, with the same error-discarding caller
behavior, matching the `ec2`/`observabilitycoverage` per-package decode
pattern; the child tests
`TestTriggerFactAWSRelationshipTargetingContainerImage`,
`TestTriggerFactAWSRelationshipNotTargetingContainerImage`, and
`TestTriggerFactAWSRelationshipUndecodable` pin that substitution directly.
Focused proof, run from the `go/` module root:
`go test ./internal/projector/containerimageidentity ./internal/projector -count=1`
green, whole-module `go build` and `go vet` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
