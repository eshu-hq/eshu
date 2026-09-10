# Chain Leaf — Evidence (#6060 naming follow-up)

Moves call-chain traversal out of `codequery/` into the new `chain/`
leaf per the #6618 naming rules (rule 3: split glued compounds into
nested directories): request, validation, and repository resolution to
`chain/request.go`; both shortestPath builders and hop predicates to
`chain/cypher.go`; node projection and endpoint candidacy to
`chain/nodes.go`. The thin `*CodeHandler` methods collect in
`codequery/callers.go` (a `chain.go` there trips the dirgate file/dir
stutter rule), with `callChainRequest` kept as an alias so pinned
signatures resolve unchanged, plus one documented forwarder
(`callChainAllowedTraversalRepoIDs`) that exists only because two
grandfathered digests name the bare identifier. Response shaping and
the shared-helper-tethered normalizers stay in `callers.go`.

## Performance and observability

No-Regression Evidence: behavior-preserving by construction. All moved
functions keep identical logic; the shipped-text pins caught and fixed
two transcription slips during the move (single-repo `$repo_id` pair
in both builders), proving the pins read the moved text. Anchors
(`Function|...|File`, single-sourced as `chain.AnchorLabelDisjunction`
for the routes family), cardinality (`LIMIT 5`, depth band 1..10),
bounds (request scope + caller grant before `LIMIT`), and indexes are
unchanged. Digests: two grandfathered bodies reproduce exactly (file
keys repathed, values frozen); one typed digest (`handleCallChain`,
`keyed_support`) recomputed with the repo go/parser extraction and
validated by `go test ./internal/queryplan/`. Package tests green:
query, codequery, deadcode, chain (no test files), queryplan, entity,
golden-corpus-gate unit contract.

No-Observability-Change: no span, metric, tracer, or pprof identifier
is added or renamed; route, capability, and response shapes unchanged.
