# Evidence: rationale edges read the collector's path key (#5998)

## What changed

`ExtractRationaleEdgeRows` read `payload["path"]` for `target_path`. No
content-entity fact carries that key. `contentEntityFactEnvelope`
(`go/internal/collector/git_content_fact_envelopes.go`) emits `relative_path`,
and every sibling extractor reads that one — `semantic_entity_materialization`,
`sql_relationship_embedded_query`, and `sql_relationship_materialization` (twice).

So `target_path` was the empty string on every rationale edge projected from real
collector output. The extractor now reads `relative_path`.

## Why it matters

`target_path` is not provenance decoration. `rationaleFilePartitionKey` hashes
it as the durable file-scoped anchor, and the delta retract keys on
`target.path` because the EXPLAINS edge precedes the code entity the comment
annotates. With the value blank, that anchor carried no file scope: every edge
in a repository hashed against the same empty path component, and only the
per-edge identity kept the keys distinct.

## How it survived

Every test exercising the extractor supplied `path` — the key the extractor
wanted, not the key the collector sends. Two of them asserted `target_path`
came back populated, which it did, because the fixture handed it the key it was
already reading. A proof built from the same misunderstanding as the code cannot
find the bug.

Those two fixtures (`rationale_edge_materialization_test.go`) now carry the
production shape and fail against the old read instead of passing beside it.

## No-Regression

No-Regression Evidence: the change is one map key in one lookup —
`semanticPayloadString(env.Payload, "path")` becomes
`semanticPayloadString(env.Payload, "relative_path")`. Same call, same cost, one
lookup per content-entity envelope either way. No loop, allocation, query, or
batch boundary moves, and no Cypher, lease, claim, or worker knob is touched.

Measured on this branch, exit codes captured directly:

- `go test ./internal/reducer -count=1` — exit 0, `ok ... 3.062s`
  (was `ok ... 2.912s` before the change; inside run-to-run noise for that
  package, and the delta is not attributable to a single map key)
- `go test ./internal/ifa -count=1` — exit 0, `ok ... 11.045s`

Backend/version: no graph backend is involved. The extractor is a pure function
over `[]facts.Envelope`; the fixtures are in-memory and the assertion is an
exact edge-set comparison. Terminal row counts: the rationale Odù derives 3
EXPLAINS edges from 5 content-entity envelopes and 1 repository envelope, with 7
inputs correctly deriving none.

## Behavior change worth naming

This populates a field that was empty in production, so it is not invisible:

- `target_path` on projected rationale edges goes from `""` to the entity's
  repo-relative path.
- `rationaleFilePartitionKey(repoID, targetPath, edgeIdentity)` therefore hashes
  a different value, so existing rationale intents redistribute across the
  partition ring on the next projection.
- The redistribution is a one-time reshuffle, not a correctness risk: the key
  still mixes the repo first and the per-edge identity last, so distinct edges
  stay collision-free and two repositories still cannot collide. What changes is
  that the key finally varies by file, which is what the doc comment on
  `rationaleFilePartitionKey` already claimed it did.

The delta retract keyed on `target.path` starts matching real paths rather than
the empty string, which is the defect being fixed rather than a new risk.

## Observability

No-Observability-Change: no metric, span, log field, or telemetry-contract entry
is added, removed, or renamed. `scripts/verify-telemetry-coverage.sh` reports no
untracked stages. The reducer's existing per-domain logs already carry
`domain=rationale_edges` and the partition id; their values change only in that
`target_path` is now populated, which is the point.

## Reproduce

```
cd go && go test ./internal/ifa -run TestRationaleFamily -count=1
cd go && go test ./internal/reducer -run 'TestExtractRationaleEdgeRows|TestRationaleHandler' -count=1
```

The guard bites, in this order:

1. Switch `odu:ifa-rationale-family` to the collector's `relative_path` shape
   while the extractor still reads `path` — RED, every expected edge `MISSING`,
   because `target_path` comes back empty.
2. Apply the extractor fix — GREEN.
3. Revert the extractor fix — RED again, same failure.
