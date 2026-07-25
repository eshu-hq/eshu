# Anchor-guard unification — one provenance signal for both image-evidence branches

Follow-up to the #5802 build break. **#5803 was the emergency compile fix** and is
already merged: it restored `supplyChainRepositoryAnchorIsReplaceable` so `main`
builds again. This change is the semantic unification that removes the underlying
inconsistency, not a second build fix.

## What the break was

`classifySupplyChainImpactPackage` guards the same invariant — a per-package
consumption anchor outranks the image-level identity — in two branches, and the
two branches ended up using two different mechanisms:

1. #5782 (#5779) added `supplyChainRepositoryAnchorIsReplaceable` (value
   inspection: blank or `oci-registry://` prefix ⇒ replaceable) and used it in the
   `os_package` branch.
2. #5781 (#5468) replaced that guard with a `repoFromConsumption` provenance flag
   and deleted the helper.
3. #5783 (#5780) had meanwhile added a second caller of the helper in the SBOM
   branch, verified green against a `main` that still contained it.

No PR touched another's lines, so git reported no conflict and the merged tree was
one that none of the three branches ever compiled (#5802). #5803 restored the
helper to unbreak the build, which left `main` carrying both mechanisms at once.

## What this change does

Hoist `repoFromConsumption` to the top of `classifySupplyChainImpactPackage` so
both image-evidence branches gate on the one provenance signal, point the SBOM
guard at it, drop the duplicate local declaration in the `os_package` branch, and
remove `supplyChainRepositoryAnchorIsReplaceable`, which then has no callers.

Behaviour is unchanged. At the SBOM guard, `finding.RepositoryID` is either the
consumption anchor (set immediately above when `consumption.factID != ""`) or
blank, so `supplyChainRepositoryAnchorIsReplaceable(finding.RepositoryID)` and
`!repoFromConsumption` agree on every reachable input:

| consumption anchor | old guard | new guard |
| --- | --- | --- |
| present (git repo id) | `false` (not replaceable) | `false` |
| absent (blank) | `true` (blank is replaceable) | `true` |

## Verification

No-Regression Evidence: a scope/declaration move plus one guard predicate swapped
for its provenance equivalent — no new Cypher, `MATCH`/`MERGE` anchor, graph round
trip, fact load, or worker/lease/queue/batch surface. `supply_chain_impact_index.go`
is content-flagged hot only because it already contains the unchanged
`scanner_worker.analysis` digest join. Commands run on this branch:

```
$ cd go && go build ./...
(no output — whole module builds)

$ go vet ./internal/reducer/
(no output — clean)

$ go test ./internal/reducer/ -count=1
ok  	github.com/eshu-hq/eshu/go/internal/reducer	3.041s

$ go test ./internal/reducer/ -run 'PreservesConsumptionRepositoryOverImageIdentity|PrefersConsumptionRepositoryOverSBOMImageIdentity' -count=1 -v
=== RUN   TestSupplyChainImpactFindingPrefersConsumptionRepositoryOverSBOMImageIdentity
=== RUN   TestSupplyChainImpactHandlerPreservesConsumptionRepositoryOverImageIdentity
--- PASS: TestSupplyChainImpactFindingPrefersConsumptionRepositoryOverSBOMImageIdentity (0.00s)
--- PASS: TestSupplyChainImpactHandlerPreservesConsumptionRepositoryOverImageIdentity (0.00s)
PASS
ok  	github.com/eshu-hq/eshu/go/internal/reducer	0.919s
```

Both precedence regressions — `TestSupplyChainImpactHandlerPreservesConsumptionRepositoryOverImageIdentity`
(#5779, os_package branch) and `TestSupplyChainImpactFindingPrefersConsumptionRepositoryOverSBOMImageIdentity`
(#5780, SBOM branch) — pass against the unified guard, which is the equivalence
proof: each branch's own regression still holds with the other branch's mechanism
removed. No golden-corpus assertion moves, because no projected value changes.

No-Observability-Change: no metrics, spans, structured logs, or status fields are
added or altered.
