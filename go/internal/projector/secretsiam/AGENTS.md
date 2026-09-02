# AGENTS.md — secrets/IAM trust-chain projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants, including
   the rule that posture facts are provenance only in the projector.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after the service-catalog correlation and before the supply-chain
   impact probe.
5. `../schema_version_admission.go` for the root gate that rejects an
   unsupported secrets/IAM schema version before this builder runs.
6. `go/internal/reducer/secretsiam/secrets_iam_trust_chain.go` for what the reducer does
   with the intent this package enqueues.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildSecretsIAMTrustChainReducerIntent` triggers on any fact kind the
  `facts.SecretsIAMSchemaVersion` registry recognizes and anchors to the
  earliest such fact in original input order across every kind, via
  `FirstMatchingKindPredicate`. Do not replace the predicate with an explicit
  kind list (a kind added to the registry later would silently stop
  triggering) or introduce a per-kind priority (it changes `FactID` for
  generations that carry several kinds and the root fan-out parity fixture
  pins the current anchor).
- Keep the `secrets/IAM source facts observed` reason and the
  `secrets_iam_trust_chain:<scope>` entity key byte-identical. The reducer
  claims one intent per scope generation and reloads the facts itself.
- Keep the three-tier source-system fallback (`SourceRef.SourceSystem`, then
  `CollectorKind`, each trimmed, then the literal `secrets_iam_posture`). Do
  not swap it for `projectorintent.SourceSystem`, which stops after the
  second tier and returns an empty string; the focused test pins the third.
- Do not decode a payload or check a schema version here. This builder reads
  only envelope metadata; root projection already rejected unsupported schema
  versions, and its regression test stays at root.
- Do not join IAM, ServiceAccount, EKS, or Vault evidence, derive an access
  path, or write to the graph here. The reducer's
  `DomainSecretsIAMTrustChain` handler owns all of that.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Use TDD. Run the focused child test, the root ordered fan-out parity and
probe-count tests, package-doc verification, the projector package tree, and
the golden-corpus gates selected by the changed paths.
