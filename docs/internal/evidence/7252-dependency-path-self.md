# #7252 — equal endpoints in dependency-path explain

Root-Cause Evidence: Neo4j Community 2026.08.1 rejects `shortestPath` when both endpoint anchors identify the same node. A direct Bolt query with identical Repository IDs failed with `51N23 cyclic shortest path search disabled`; the HTTP handler previously returned 500 for equal IDs and for an ID/name alias. The new regression test reproduced both 500 responses before the handler change.

## Accuracy

The handler now returns 400 before `shortestPath` for an unscoped request with identical arguments. It resolves scoped arguments before deciding equality, so an unknown or ungranted endpoint retains the existing 404. Distinct arguments that resolve to the same canonical ID and label also return 400. The existing two-hop path still returns one path with two hops on a live Neo4j fixture. API and MCP use this same HTTP handler.

## Performance

No-Regression Evidence: on a local Neo4j Community 2026.08.1 container with a three-node, two-edge fixture, the previous handler's first equal-ID HTTP request after container restart took 1.530 s with two anchor resolutions; the one-resolution intermediate version took 1.476 s, showing that one cold anchor resolution alone exceeded the 1 s target. With the final unscoped equality guard, the first equal-ID request after container restart took 221.292 µs after fixture setup, and a subsequent 10-call warm sample had a maximum (p95 by nearest rank) of 27.833 µs. An ID/name alias still requires two anchor reads: first 299.922 ms and warm p95 29.898 ms. Each request returned 400 with no path rows. This local fixture proves the changed path but is not an ops-qa corpus measurement or a post-deploy p95 claim.

## Observability

No-Observability-Change: equal unscoped arguments no longer reach Neo4j, so there is no graph query span for that branch. The HTTP status and error detail identify the client input. Alias and scoped requests retain the existing graph-read spans and error handling. No metric, log field, or graph schema changed.

## Verification

- `go test ./internal/query/impact -run 'TestExplainDependencyPathRejectsSameResolvedEndpoint|TestScopedExplainDependencyPath' -count=1 -v` — green.
- `ESHU_OCI_PROVE_LIVE=1 ESHU_LIVE_GRAPH_DATABASE=neo4j ESHU_NEO4J_URI=bolt://127.0.0.1:27951 go test ./internal/query -run TestLiveByIdImpactAnchorReads -count=1 -v` — green on the local Neo4j container after restart; one correct two-hop path and both equal-endpoint cases returned 400.

Refs #7252
