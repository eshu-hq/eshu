# Relationships Leaf — Evidence (#6060 naming follow-up)

Moves the relationships read family out of `codequery/` into the new
`relationships/` leaf per the #6618 naming rules (rule 3: split glued
compounds into nested directories): `enrich.go`, `filters.go`,
`identity.go`, `nornicdb.go`, with the thin `*CodeHandler` methods left in
`codequery/relationship_handlers.go` (a `relationships.go` there trips the
dirgate file/dir stutter rule). 22 family test files moved with them, plus
new leaf-level `relationships_test.go` / `relationships_test_helpers_test.go`.
Handler-seam tests that construct the root `CodeHandler` stay in `codequery/`
— a different package cannot name it — as do the grant/fake suites owned by
#5167.

## Behavior change

The capability sweep learns package-qualified calls. Root keeps thin
forwarders such as `relationshipCapability`, which delegates to
`relationships.Capability(direction, relationshipType)`; the sweep
previously resolved only bare-identifier callees, so every one of those
forwarded call sites fell through to unresolvable and failed the sweep on a
clean tree. `resolveCallResult` in
`go/internal/query/graph_read_error_capability_sweep_qualified_call_test.go`
grew a `SelectorExpr` branch plus `resolveQualifiedDir`, which binds a
`pkg.Func` qualifier through the consuming file's imports (import binding)
and the declaring directory's path base (directory binding), failing closed
unless exactly one directory qualifies — the same unanimity discipline the
sweep enforces on values. This is sweep tooling, not production: no handler,
cypher text, or grant predicate changes.

## Performance and observability

No-Regression Evidence: no Cypher text changed and no production signature
moved. The pinned shas covering relationships symbols reproduce unchanged —
`query-source-coverage.yaml` grandfather entries for
`relationshipStoryClassMethods`, `relationshipStoryInheritanceDepthRows`,
`relationshipStoryOverrideRows`, `relationshipStoryGraphRowsForDirection`,
`nornicDBRelationshipStoryClassMethods`, `nornicDBRelationshipStoryGraphRows`,
`nornicDBRelationshipStoryInheritanceDepthRows`,
`resolveNornicDBRelationshipStoryAnchorProperty`, and the inventory-only
`relationshipsGraphRow` are untouched, and the queryplan suite is green.
Proven by `TestCapabilitySweepResolvesPackageQualifiedCall` (a qualified
call resolves back into the declaring leaf) plus the full sweep green on
the nested tree. Package tests green: query, codequery, relationships,
queryplan.

No-Observability-Change: no span, metric, tracer, or pprof identifier
is added or renamed.
