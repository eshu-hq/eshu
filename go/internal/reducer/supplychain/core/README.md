# core

The supplychain family: supply-chain impact correlation plus vulnerability
suppression (issue #6061, step 2 of
`docs/internal/design/reducer-target-tree.md`).

## Ownership

This package owns the join from vulnerability-intelligence facts to impacted
repositories and workloads: it reads CVE, affected package/product, OS
package, EPSS, KEV, and suppression facts, package-registry and
manifest/lockfile dependency evidence, SBOM and container-image identity
evidence, repository facts, CI run correlation output, and provider security
alerts, and writes reducer-derived supply-chain impact findings plus
suppression decisions. The reducer runtime drives `SupplyChainImpactHandler`
through the default domain catalog; `go/cmd/reducer` wires the Postgres
writer. Query surfaces read the durable facts; they never import this
package.

## Files

70 non-test files: 63 `impact*`/`*.go` short names (impact builders,
ecosystem matchers, reachability, anchoring, writers), 4 suppression-story
files (`evaluation.go`, `decode.go`, `reasons.go`, `scope.go`), 3
`go_reachability*` (Go
module/call reachability classifier), plus 80 test files. No file repeats
the directory name.

| File | Covers |
| --- | --- |
| `impact.go` | `SupplyChainImpactHandler`, finding pipeline |
| `finding.go` | `SupplyChainImpactFinding` (carries the `Suppression` field: impact + suppression are one unit) |
| `writer.go` | `PostgresSupplyChainImpactWriter`, batched versioned inserts |
| `winners_maintainer.go` | `SupplyChainImpactWinnersMaintainer` read-model resweep |
| `evaluation.go`, `decode.go`, `reasons.go`, `scope.go` | Suppression story: Decision + Evaluate, decode, reasons, scope match |
| `go_reachability*.go` | Go module/call reachability classification |
| `cross_scope_test_doubles_test.go` | Family-local test doubles (each side keeps its own copy) |

## Seams other families use

- Builders: `BuildSupplyChainImpactFindings`,
  `BuildSupplyChainImpactRemediation`, `BuildVulnerabilitySuppressions`,
  `EvaluateSupplyChainSuppression`, `ClassifyGoVulnerabilityReachability`.
- Handler/writers: `SupplyChainImpactHandler`, `SupplyChainImpactWriter`,
  `PostgresSupplyChainImpactWriter`, `SupplyChainImpactWinnersMaintainer`.
- Types: `SupplyChainImpactFinding`, `SupplyChainImpactFactFilter`,
  `SupplyChainImpactWrite`, `SupplyChainImpactWriteResult`,
  `SupplyChainSuppressionDecision`, `SupplyChainSuppressionState`,
  `GoVulnerabilityFinding`, `JVMReachabilityFactFilter`.

## Dependency rule

One-way imports only: this package reads the shared tier (`contract`,
`factload`, `factdecode`, `factwrite`, `schemadecode`, `crossscope`,
`payloadcore`, `supplychainmodel`), the already-extracted `cicdrun`,
`containerimage`, `servicecatalog`, `correlation`, `source`, and
`securityalert` subpackages (exported consts, fact kinds, and
alert/consumption types only), plus `facts`, `packageidentity`,
`telemetry`, `truth`, `environment`, and the SDK `factschema`. It never
imports the parent reducer package. The parent references it only through
`supplychaincore`-qualified names; external callers keep their `reducer.X`
spelling through the supply_chain_impact stanza in the parent's
`compat_correlation.go`.
