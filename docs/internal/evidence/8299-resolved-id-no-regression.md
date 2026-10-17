# Resolved-ID Identity Digest No-Regression Evidence (#8299)

#8299 changes `relationships.ResolvedRelationshipID` from a
generation-plus-ordinal digest to a pure function of the
`resolved_relationships_identity_idx` identity
(generation, `COALESCE` entity/repo ids, type), and drops the ordinal
parameter at both callers (`relationship_store.go` `UpsertResolved`,
`crossrepo` intent rows). No SQL text, index, batch shape, worker count,
queue, lease, or retry path changes.

No-Regression Evidence: per-row digest work strictly shrinks. The old path
formatted the slice ordinal with `fmt.Sprintf("%d", ordinal)` on every row;
the new path hashes four parts with two branch fallbacks and no formatting,
still O(1) per row with the same row count. `BenchmarkResolvedRelationshipID`
(new, `go/internal/relationships/models_resolved_id_collision_test.go`) on
linux/arm64, go1.26.6: ~315 ns/op, 280 B/op, 8 allocs/op across 3 runs of
1M iterations. Functional suites after the change: `go test
./internal/relationships/` ok (0.9s), `./internal/reducer/crossrepo/` ok,
`./internal/reducer/servicecatalog/` ok (2.3s),
`./internal/storage/postgres/` ok (6.2s); `go build ./...` and `go vet` on
touched packages exit 0. No hot loop, Cypher, contention, or batching change,
so no corpus-scale re-measurement applies.

No-Observability-Change: no metric, span, log, status, or pprof signal is
added, removed, or renamed; the cross-repo resolution info logs keep their
fields and the activation wake keeps its instruments.
