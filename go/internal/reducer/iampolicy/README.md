# iampolicy

Owns the shared vocabulary for evaluating decoded `aws_iam_permission`
statements: the statement and grant shapes, the conservative action matchers,
and the target-resolution outcome.

This package was carved out of the flat `internal/reducer` root under issue
#6061. It is a shared-core leaf, not a domain family: it registers no handler,
owns no domain, performs no I/O, and holds only plain data and pure functions.

## What it owns

| piece | file | what it does |
|---|---|---|
| `Statement` | `grant.go` | a decoded permission statement paired with its source `FactID` for dedup |
| `PrincipalGrant` | `grant.go` | one principal's conservatively-trusted effective grant |
| `PrincipalGrant.Allows` / `.Denied` | `grant.go` | wildcard-aware membership over the trusted and deny action sets |
| `PrincipalGrant.StatementsCovering` | `grant.go` | resolves a carrier action back to every statement that grants it |
| `PrincipalStatements` | `grant.go` | one principal's statements plus its resolved node uid |
| `EdgeKey` | `grant.go` | the `(principal_uid, target_uid)` dedup identity for an IAM edge |
| `TargetStatus` + constants | `grant.go` | resolved / ambiguous / unresolved |
| `AllowStatementTouches`, `StatementTouchesCatalog` | `grant.go` | the action matchers the grant folds use |
| `CollectTrustedResources`, `GlobMatch`, `ResourceTypeOfARN` | `grant.go` | the target-resolution primitives |
| `ResourceTypeRole` / `User` / `Policy` / `Group` | `grant.go` | the IAM `resource_type` tokens a matched node must carry |

Only the two unambiguous wildcard shapes are honoured: `*` and `service:*`. A
partial wildcard such as `iam:Create*` is deliberately not expanded. Expanding
it would over-approximate the grant, and an over-approximated grant becomes a
graph edge asserting access that may not exist.

`GlobMatch` is an iterative matcher rather than a compiled regexp: it avoids a
per-call compile and the catastrophic backtracking of a naive recursive matcher
by tracking the last `*` position. Its greediness is safe only because every
caller additionally requires the resolved node be a scanned node of the expected
type, so a single-segment over-match cannot fabricate a cross-type edge.

## Why this is a shared leaf

The IAM privilege-escalation slice at the reducer root and the `reducer/iamcan`
family evaluate the same decoded statements. They count into different tallies
and check against different catalogs, so the folds stay separate — but the
statement and grant shapes, the matchers, and the resolution outcome are one
vocabulary. A family package may never import the reducer root, so the shared
half lives below both. The root keeps its spelling through aliases and
forwarders in `iam_permission_grant_compat.go`.

Because `PrincipalGrant` now lives here, the root cannot attach methods to it.
The escalation-specific `armStatus` became the free function `grantArmStatus` in
`iam_escalation_grant.go`; it reads root-owned primitive vocabulary and did not
belong here.

Imports point strictly downward. This package reaches only the standard library
and the factschema SDK, and it never imports the parent `internal/reducer`
package.

## Telemetry

This package registers no instrument and performs no I/O.

Every refusal it classifies — a Deny, a condition, an ambiguous target — is
counted by the caller against that caller's own skip counter
(`eshu_dp_iam_escalation_skipped_total` at the root,
`eshu_dp_iam_can_perform_skipped_total` in `reducer/iamcan`). Keeping the
counting at the caller is what lets one shared matcher serve two domains with
different skip taxonomies.

No-Regression Evidence: #6061 relocates this code from `iam_escalation.go`,
`iam_escalation_grant.go` and `iam_escalation_target.go` without changing it.
The bodies are unchanged; the diff is the package clause, the identifiers and
struct fields becoming exported, and root aliases and forwarders replacing the
original declarations. Behavior stays covered by the existing root escalation
suites (`iam_escalation_test.go`, `iam_escalation_skips_test.go`,
`iam_escalation_materialization_test.go`) and by `internal/reducer/iamcan`.
Measured on this branch: `go build ./...` exits 0, `go vet
./internal/reducer/...` exits 0, and `go test ./internal/reducer/... -count=1`
passes. Binary output was not compared and no such claim is made here.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, or
log field. This package emitted no signal before the move and emits none after.

## Gotchas / invariants

- **Do not import the reducer root from here.** This is the bottom of the IAM
  stack; reaching upward would recreate the cycle it exists to break.
- **`TargetAmbiguous` and `TargetUnresolved` are not interchangeable.** One
  means "the pattern named many or all nodes", the other "it named none". They
  are different operator stories and are counted under different skip reasons;
  collapsing them hides which half of the resolution ladder failed.
- **Do not expand partial wildcards.** `iam:Create*` granting nothing is the
  invariant, not an omission.
- **`Statement.FactID` is the dedup key.** A statement registered under several
  lookup keys must be returned at most once by `StatementsCovering`; a statement
  with a blank `FactID` is intentionally never deduped rather than colliding
  with every other blank one.
- **The grant maps are exported and callers build them directly.** A caller that
  forgets to initialize `StatementsByAction` gets a nil-map write panic on the
  first trusted action, not a quiet empty grant.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
