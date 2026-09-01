# Secrets/IAM trust-chain projector intents

## Purpose

This package recognizes secrets/IAM posture facts for one scope generation and
builds the reducer intent that asks the reducer to expand the trust chain —
AWS IAM principals and policies, GCP IAM, Kubernetes service accounts and
RBAC, EKS identity associations, Vault mounts and policies, and coverage
warnings — into durable read-model facts.

## Ownership boundary

The package owns only the trigger selection and the reducer-intent value. The
root `internal/projector` package validates scope-generation boundaries,
rejects unsupported secrets/IAM schema versions before any builder runs,
constructs and owns the immutable fact lookup, preserves family order, and
owns projection lifecycle, queue writes, retries, and telemetry. The reducer's
`DomainSecretsIAMTrustChain` handler owns every trust-chain join and write;
the projector never joins IAM, ServiceAccount, or Vault evidence and never
derives an access path.

## Exported surface

- `BuildSecretsIAMTrustChainReducerIntent` builds the
  `secrets_iam_trust_chain` intent, anchored to the earliest fact in the
  generation whose kind the `facts.SecretsIAMSchemaVersion` registry
  recognizes.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup` and
`internal/projector/intent.ReducerIntent`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. It reads `internal/facts` for the
secrets/IAM posture schema registry and `internal/reducer` for the domain
constant. There is no decode seam: like `packagesource`, this builder reads
only envelope metadata and never a payload field.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains the `reducer.secrets_iam_graph_projection` span and
the `SecretsIAMGraphNodesWritten`/`SecretsIAMGraphEdgesWritten` counters.
Moving the pure builder adds no queue, storage, graph, span, metric, or log
boundary.

## Gotchas / invariants

- Every registry-recognized posture kind is a trigger and none outranks
  another: the anchor is the earliest recognized fact in original input order
  across all kinds, via `FirstMatchingKindPredicate`. Replacing the predicate
  with an explicit kind list would silently drop a kind added to the registry
  later; adding a per-kind priority would change `FactID` for any generation
  that carries more than one kind.
- The `Reason` is always `secrets/IAM source facts observed` and the
  `EntityKey` is `secrets_iam_trust_chain:<scope>`, one intent per scope
  generation regardless of how many posture facts it carries. The root
  fan-out parity fixture pins both; the reducer reloads the generation's
  facts itself.
- `SourceSystem` falls back in three tiers: `SourceRef.SourceSystem`, then
  `CollectorKind`, each trimmed, then the literal `secrets_iam_posture`.
  `projectorintent.SourceSystem` stops after the second tier and returns an
  empty string, so it is not a drop-in replacement here; the focused test
  pins the literal third tier.
- The payload is never decoded here, and the schema version is not checked
  here either. Root projection's `validateFactSchemaVersion` rejects an
  unsupported secrets/IAM `schema_version` before this builder runs, and its
  regression test stays at root in `schema_version_admission_test.go` because
  it asserts root behavior.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
the projector package tree, package-doc and path mirrors, dirgate, telemetry
coverage, and the golden-corpus gates selected by the changed paths.

No-Regression Evidence: this extraction moves one builder without changing
the trigger, value, or fan-out position. The reducer intent domain, entity
key, reason string, input-order anchor selection across all recognized
posture kinds, and three-tier source-system derivation are identical to the
base commit, and the dispatcher's ordered fan-out is unchanged at 44 builder
probes with this probe still running immediately after
`servicecatalog.BuildServiceCatalogCorrelationReducerIntent` and before
`buildSupplyChainImpactReducerIntent`. The private `secretsIAMSourceSystem`
helper the root file owned was compared body-for-body against
`projectorintent.SourceSystem` and found to carry a literal third fallback
(`secrets_iam_posture`) the shared helper lacks, so it moved with the family
unchanged rather than being swapped; the root `firstMatchingKindPredicate`
forwarder was a direct delegate to
`projectorintent.FactLookup.FirstMatchingKindPredicate`, so that substitution
is behavior-identical by construction, and the forwarder stays at root for
the two root probes that still call it. Focused proof, run from the `go/`
module root:
`../scripts/go-test-run-guard.sh 1 'TestBuildSecretsIAMTrustChainReducerIntent' -- ./internal/projector/secretsiam -count=1`
and `go test ./internal/projector/... -count=1` green, whole-module `go build`
and `go vet` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Reducer domain catalog](../../reducer/domain-catalog.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
