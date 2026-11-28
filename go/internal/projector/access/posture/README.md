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
  generation whose kind the `factsidentity.SecretsIAMSchemaVersion`
  registry recognizes.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup` and
`internal/projector/intent.ReducerIntent`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. It reads `internal/facts` for the
secrets/IAM posture schema registry and `internal/reducer` for the domain
constant. There is no decode seam: like `package/source`, this builder reads
only envelope metadata and never a payload field.

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

## Related docs

- [Projector architecture](../../README.md)
- [Intent contract](../../intent/README.md)
- [Reducer domain catalog](../../../reducer/domain-catalog.md)
- [Package restructure](../../../../../docs/internal/design/package-restructure.md)
