# Resolution-Provenance Schema For Code Call And Reference Edges (issue #2222)

Status: Proposed. Gate for #2218. Must merge before implementation children
#2223 (reducer emission), #2224 (edge-writer persistence), #2225 (API/MCP
surfacing), #2226 (accuracy goldens), #2227 (docs).
Issue: #2222. Parent epic: #2218.

## 1. Decision

Code call and reference edges record **how** the callee/target entity was
resolved, as a closed `resolution_method` enum, and derive their numeric
`confidence` from that method through a fixed, documented table. This replaces
the hard-coded `confidence = 0.95` and single fixed `reason` string written on
every `CALLS`, `REFERENCES`, and `USES_METACLASS` edge in
`go/internal/storage/cypher/edge_writer_code_call_labels.go` and the
`batchCanonical*` templates in `go/internal/storage/cypher/canonical.go`.

Provenance is **descriptive, not admissive**. It records the resolver branch
that produced an edge that already passed admission. It does not gate edge
creation, does not change which edges are written, and does not promote a
heuristic score to canonical truth. The envelope truth label on an answer is
unchanged; per-edge `resolution_method`/`confidence` are additive fields beneath
that envelope.

## 2. Current State

Resolution does **not** happen in the parser. Parsers emit raw call rows
(`name`, `full_name`, `line_number`, `call_kind`) plus resolution *hints*
(`receiver_is_import_alias`, `inferred_obj_type`), and the SCIP indexer emits a
separate `function_calls_scip` bucket of symbol-resolved edges. The callee
entity is bound to a `uid` later, in the reducer:

- SCIP path: `extractSCIPCodeCallRows`
  (`go/internal/reducer/code_call_materialization_index.go`) — both endpoints
  resolved by symbol → file/line lookup. Highest certainty.
- Heuristic path: `resolveGenericCallee`
  (`go/internal/reducer/code_call_materialization_imports.go`) — an **ordered**
  fallback dispatch over same-file lexical scope, type inference, import
  bindings, package/directory scope, and repository-wide unique-name match.
- Declared path: Python metaclass rows arrive with `source_entity_id` /
  `target_entity_id` already bound by the parser (`USES_METACLASS`), no
  heuristic resolution.

The branch that succeeds is discarded before `appendCodeCallRow` builds the
materialization row, so the edge writer cannot tell a SCIP-proven call from a
repository-wide name guess. Both ship as `confidence = 0.95`. Eshu therefore
cannot compute per-language call-resolution accuracy, which competitors publish.

`confidence` already exists in the OpenAPI `Relationship` schema
(`go/internal/query/openapi_components.go`) but is not populated from edge
properties; `resolution_method` does not exist anywhere yet.

## 3. Non-Goals

- No change to admission: provenance never adds, drops, or filters an edge.
- No promotion of heuristic scores to canonical truth.
- No embeddings, link prediction, or semantic-similarity edges.
- No new unbounded payload on API/MCP answers; provenance is two scalar fields
  per already-returned edge.
- No reducer-throughput regression without a measured, documented bound
  (owned by #2224 evidence).

## 4. `resolution_method` Vocabulary

The enum is **closed**. Every resolver branch maps to exactly one value, and
every value maps to one derived confidence. Values are language-neutral and
ordered strongest → weakest.

| `resolution_method` | Meaning | Derived `confidence` |
| --- | --- | --- |
| `scip` | SCIP semantic symbol resolution; both endpoints bound by symbol → file/line. | 0.99 |
| `declared` | Relationship explicitly declared in source and bound by the parser (Python `USES_METACLASS`); no heuristic resolution. | 0.95 |
| `same_file` | Callee resolved inside the caller's file by lexical-scope span or same-file unique name. | 0.95 |
| `import_binding` | Callee resolved by following an explicit import / Go package-qualified import / re-export barrel. | 0.90 |
| `type_inferred` | Callee resolved through receiver/return-type inference, dynamic-alias inference, or constructor binding. | 0.80 |
| `scope_unique_name` | Callee resolved by a unique name within a bounded directory/package scope, with no import binding (e.g. Go same-directory). | 0.70 |
| `cross_repo_export_package` | Callee resolved across repositories by matching a Go package import path (anchored on the defining repo's declared `go.mod` module path) to the single exported top-level function with that name. | 0.70 |
| `repo_unique_name` | Callee resolved by a repository-wide unique-name match with no scope or import evidence; the global fallback. | 0.50 |
| `unspecified` | Method not recorded (legacy edge written before this ADR, or a future branch not yet classified). Readers must tolerate this; writers must not emit it for any classified branch. | preserves prior value (see §6) |

`confidence` is a presentation-tier derivation of `resolution_method`, not an
independent signal. It is recomputed from the method on every write; the method
is the source of truth. Tier numbers are chosen to keep `same_file` at the
historical 0.95 so the dominant case is unchanged, while separating the global
fallback (0.50) and the strong SCIP tier (0.99).

### 4.1 Branch → method mapping (normative)

Implementation (#2223) MUST map resolver branches as follows. The mapping is the
contract the accuracy goldens (#2226) assert against.

| Resolver branch (function) | `resolution_method` |
| --- | --- |
| `extractSCIPCodeCallRows` | `scip` |
| `USES_METACLASS` declared rows | `declared` |
| `resolveSameFileScopedCalleeEntityID` | `same_file` |
| `resolveSameFileCalleeEntityID` | `same_file` |
| `resolveDynamicJavaScriptCalleeEntityID` | `type_inferred` |
| `resolveGoMethodReturnChainCalleeEntityID` | `type_inferred` |
| `resolveConstructorMethodCalleeID` | `type_inferred` |
| `resolveImportedCrossFileCallee` | `import_binding` |
| `resolveGoPackageQualifiedCalleeEntityID` | `import_binding` |
| `resolveReexportedCrossFileCallee` | `import_binding` |
| `resolveGoSameDirectoryCalleeEntityID` | `scope_unique_name` |
| `resolveGoCrossRepoExportCalleeEntityID` | `cross_repo_export_package` |
| `index.uniqueNameByRepo` exact-name match | `repo_unique_name` |
| `index.uniqueNameByRepo` broad-name match | `repo_unique_name` |

If a new resolver branch is added later, this table and the goldens MUST be
updated in the same change; an unmapped branch defaults to `unspecified` and
fails the parity gate rather than silently inheriting a tier.

## 5. Edge Property Contract

On every `CALLS`, `REFERENCES`, and `USES_METACLASS` edge:

- `resolution_method` (string): one closed value from §4.
- `confidence` (float): derived from `resolution_method` per the §4 table.
- `reason` (string): retained, but selected per method instead of one fixed
  string, so an operator reading the raw edge sees the mechanism.
- `evidence_source`, `call_kind`, existing `relationship_type` on metaclass:
  unchanged.

Confidence derivation lives in one place (a single Go function consumed by the
edge writer) so the table cannot drift between methods. The Cypher `confidence`
becomes a row parameter rather than a literal, matching the existing
`DEPENDS_ON` repo-dependency pattern in `canonical.go`.

## 6. Backward Compatibility

- Both fields are **additive**. `resolution_method` is a new property; readers
  must treat its absence as `unspecified`.
- Edges written before this change keep their stored `confidence = 0.95` and
  carry no `resolution_method` until the owning repository is re-projected.
  Re-projection is the normal reducer path; no migration job rewrites historical
  edges. The accuracy goldens (#2226) run on freshly projected fixtures, so they
  are unaffected by un-reprojected legacy edges.
- API/MCP responses omit `resolution_method` when the underlying edge has none,
  rather than inventing a tier. `confidence` falls back to the stored property.
- The `MERGE` shape and UNWIND batching are unchanged; only the `SET` clause
  gains parameterized properties, so the graph-write hot path keeps its batch
  semantics. #2224 carries the No-Regression Evidence.

## 7. Composition With Envelope Truth Labels

The answer-level `TruthEnvelope` (`go/internal/query/contract.go`) describes the
**provenance of the answer** (graph vs content index, freshness, backend).
Per-edge `resolution_method` describes the **provenance of one edge inside that
answer**. They are orthogonal and both are reported:

- The envelope is unchanged by this ADR. An `exact` / `authoritative_graph`
  answer stays `exact`; it can still contain a `repo_unique_name` edge.
- Per-edge provenance never raises or lowers the envelope truth level. A
  low-confidence edge does not make the answer a fallback answer; it makes one
  edge inside an exact answer explicitly uncertain.
- Agents consume both: the envelope to trust the answer's basis, the per-edge
  method/confidence to weight an individual relationship.

## 8. Evidence Plan (owned by children)

- #2223: failing fixture first; per-tier, per-language fixtures asserting the
  emitted `resolution_method` matches the §4.1 mapping for SCIP, import-scoped,
  same-file, type-inferred, scope-unique, and repo-unique branches.
- #2224: regression test proving differentiated `confidence` lands per tier;
  No-Regression Evidence on edge-write throughput (cypher-performance ladder).
- #2225: handler tests covering the additive fields; envelope truth labels
  preserved.
- #2226: per-language resolution-tier distribution goldens and a CI parity gate
  that fails on the §4.1 mapping drifting.
- #2227: graph-model reference documents the closed vocabulary and tiers;
  cypher-performance note on the edge-write `SET` shape.
