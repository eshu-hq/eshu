# Supply-chain-impact projector intents

## Purpose

This package recognizes vulnerability, security-alert, package-identity,
SBOM, and OCI evidence in one scope generation and builds the reducer intent
that asks the reducer to recompute `supply_chain_impact`: the
vulnerability-to-package-to-deployment join that tells an operator which
running workloads are affected by a CVE, an EPSS score change, a known-
exploited flag, a suppression decision, or a newly observed package/image
identity. Twelve fact kinds trigger it because any one of them can be the
side of the join that arrives last: a CVE can land before the package that
carries it is known, and a package identity or OCI manifest can land after
the vulnerability intelligence that already names it.

## Ownership boundary

The package owns only the trigger selection, the reason string, and the
reducer-intent value. The root `internal/projector` package validates
scope-generation boundaries, constructs and owns the immutable fact lookup,
preserves family order, and owns projection lifecycle, queue writes,
retries, and telemetry. The reducer's `DomainSupplyChainImpact` handler
(`go/internal/reducer/supply_chain_impact.go`) owns the cross-source join
that resolves affected packages, deployment/environment evidence read
through `crossscope.dependencyCatalog`'s `ci_cd_run_correlation` link, and
the durable finding write; none of that happens here.

## Exported surface

- `BuildSupplyChainImpactReducerIntent` builds the `supply_chain_impact`
  intent, anchored to the earliest accepted fact across the twelve candidate
  kinds in original generation order (`FirstAcrossKinds`, not the earliest
  fact of the first-checked kind).

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package — root already imports this package to dispatch to
it, so the reverse import would cycle. It reads `internal/facts` for the
twelve fact-kind constants and `internal/reducer` for the domain constant.
There is no decode seam: like `cicdruncorrelation` and `packagesource`, this
builder reads only envelope fields and never a payload key.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains `eshu_dp_reducer_executions_total`,
`eshu_dp_reducer_run_duration_seconds`, and
`eshu_dp_supply_chain_impact_findings_total` for its cross-source join.
Moving the pure builder adds no queue, storage, graph, span, metric, or log
boundary.

## Gotchas / invariants

- The trigger fires on the earliest fact across all twelve candidate kinds
  in original generation order, via `FirstAcrossKinds` — not "earliest fact
  of the first-checked kind." A vulnerability fact earlier in
  `candidateFactKinds` does not outrank an SBOM or package-identity fact
  that arrived first in the actual generation.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`: a
  trimmed `SourceRef.SourceSystem`, falling back to a trimmed
  `CollectorKind`. The pre-extraction root helper
  (`supplyChainImpactSourceSystem`) had the identical two-tier body — this
  package's own tests pin both tiers with the two set to DIFFERENT values,
  so a regression that swapped the tiers would fail the test rather than
  pass on a same-value fixture.
- The `Reason` string varies by trigger fact kind (security alert, package
  identity, SBOM component, suppression, OCI subject, or the generic
  vulnerability fallback) and the `supply_chain_impact:<scope>` entity key is
  fixed; both are pinned by this package's own tests. Unlike the
  `cicdruncorrelation` family, the root fan-out parity fixture
  (`../scope_generation_intents_fanout_parity_test.go`) DOES cover this
  domain: it carries a `package-registry.package` fact ahead of a
  `security_alert.repository_alert` fact and asserts `factID:
  "package-identity-1"`, `entityKey:
  "supply_chain_impact:mixed:fanout:demo"`, `reason: "package registry
  identity observed"`, `sourceSystem: "package_registry"` in
  `fanOutParityExpectations[reducer.DomainSupplyChainImpact]`, and this
  domain's position in `fanOutParityExpectedOrder` (immediately after
  `DomainSecretsIAMTrustChain`, immediately before
  `DomainSecurityAlertReconciliation`) — do not assume the parity fixture is
  a safe net for a family without checking it first; here it genuinely is
  one.
- The payload is never decoded here and no schema version is checked here;
  the reducer handler owns that, plus the cross-source join over
  vulnerability, package, SBOM, and OCI evidence, and the deployment/
  environment evidence read through `crossscope.dependencyCatalog`.
- The root dispatcher tests that go through `buildProjection` stay at root in
  `../supply_chain_impact_projection_test.go`.

## Verification

Run the package contract tests, the root dispatcher tests, the ordered
fan-out parity and probe-count tests, the projector package tree,
package-doc and path mirrors, dirgate, telemetry coverage, and the
golden-corpus gates selected by the changed paths.

No-Regression Evidence: this extraction moves one builder without changing
its trigger, value, or fan-out position. The reducer intent domain, entity
key, per-kind reason strings, earliest-across-kinds anchor rule, and
source-system derivation are identical to the base commit, and the
dispatcher's ordered fan-out is unchanged at 44 builder probes with this
probe still running immediately after
`secretsiam.BuildSecretsIAMTrustChainReducerIntent` and immediately before
`security.BuildSecurityAlertReconciliationReducerIntent`. The family carried
a private `supplyChainImpactSourceSystem` helper that was checked
body-for-body against `projectorintent.SourceSystem` and found identical
(trim `SourceRef.SourceSystem`, else trim `CollectorKind`, no third tier),
so the substitution is behavior-identical by construction and the child
tests pin both tiers with the two set to different values. Focused proof,
run from the `go/` module root: `go test ./internal/projector/supplychainimpact
./internal/projector -count=1` green, whole-module `go build` and `go vet`
clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
