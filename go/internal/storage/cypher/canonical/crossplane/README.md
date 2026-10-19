# Canonical Crossplane Satisfied-By Edges

## Purpose

This package builds Crossplane Claim (`K8sResource`) -> XRD (`CrossplaneXRD`)
`SATISFIED_BY` edge rows and statement-free write parts: the batched
`MATCH`-`MATCH`-`MERGE` upsert and the scope-plus-evidence-source retract.

## Ownership boundary

The leaf owns query templates, row shaping, the write-reason registry, and
part ordering. The parent `cypher` package owns executable `Statement`
wrapping, the writer API, and `Executor` dispatch.

## Exported surface

- `BuildUpsertParts` — build one batched upsert part per row batch
- `BuildRetractPart` — build the single stale-edge retract part
- `CloneRowsWithScope` — copy rows with scope/generation/evidence injected
- `Part` — statement-free Cypher plus parameters and execution flags
- `MaterializedEdgeTypes` — registry the blast-radius coverage gate merges in
- `SatisfiedByEdgeUpsertCypher`, `RetractSatisfiedByEdgesCypher`, `Phase`,
  `EdgeLabel`, `EvidenceSource` — template and metadata constants

See `doc.go` for the package contract.

## Dependencies

None. This leaf is dependency-free by design so it never imports its parent.

## Telemetry

None. The parent writer executes parts through its existing instrumentation.

## Gotchas / invariants

- A missing Claim or XRD endpoint is a no-op (two `MATCH`es, never a
  `MERGE` on nodes), so edges self-heal once both endpoints commit.
- The retract filters on the edge's own `scope_id` and `evidence_source`;
  endpoint nodes carry no reducer `scope_id`.
- Retracts dispatch sequentially, never grouped (NornicDB DELETE pitfall).
- The `Part` metadata key values (`_eshu_phase`, `_eshu_entity_label`,
  `_eshu_statement_summary`) must stay identical to the parent `cypher`
  package's `StatementMetadata*Key` values; the root adapter copies
  parameters through untouched.

## No-Regression Evidence

No-Regression Evidence: the move changes no statement text, parameter shape,
row cloning, batching, or dispatch order — the proof below pins each one.

`go test ./internal/storage/cypher/canonical/crossplane -count=1` and the
root parity tests
(`TestCrossplaneSatisfiedByEdgeLeafParityUpsertByteIdentical`,
`TestCrossplaneSatisfiedByEdgeLeafParityRetractByteIdentical`) prove the
leaf emits byte-identical Cypher and DeepEqual parameters to the pre-move
root writer on the same inputs. Backend execution shape is unchanged
because both Cypher templates are byte-identical to the pre-move root
versions, pinned against embedded golden literals with `==`.

No-Observability-Change: the root writer retains `Executor` dispatch,
`GroupExecutor` atomicity, statement metadata, and retry wrapping; this
package adds no metric name, metric label, worker, queue domain, runtime
knob, backend branch, or new graph-write route.

## Related docs

- `docs/public/reference/cypher-performance.md`
- `docs/public/reference/nornicdb-pitfalls.md`
- `go/internal/storage/cypher/README.md`
