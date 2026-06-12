# Code Relation Taxonomy Gap Analysis (issue #2228)

Status: Proposed. Gate for #2219. Must record a decision before implementation
children #2229 (IMPLEMENTS + INSTANTIATES), #2230 (EXPLAINS), #2231 (DOCUMENTS)
start.
Issue: #2228. Parent epic: #2219.

## 1. Decision

The first taxonomy slice is **IMPLEMENTS** and **INSTANTIATES**, scoped to
languages with an explicit implementation/instantiation signal the parser
already emits or can emit cheaply. `EXPLAINS` (#2230) and `DOCUMENTS` (#2231)
are already separately scoped under epic #2219 and are confirmed by this
analysis as the right next two. `FIELD_ACCESS`, `RE_EXPORTS`, and
`DECORATOR/ANNOTATION` edges are **deferred** with recorded reasons.

The ranking is by value-per-projected-edge: highest agent value at the lowest
added projection cardinality wins the first slice. No candidate is admitted
without a per-candidate cardinality estimate and a projection-cost note.

## 2. Method

Cardinality is estimated from the parser fixture corpus
(`tests/fixtures/sample_projects/`, 23 language projects) by counting source
signals with `rg`, then projected against the existing code-edge write path.

Projection cost is the marginal cost on the graph-write hot path. That path is a
batched `UNWIND $rows` `MERGE` (`go/internal/storage/cypher/canonical.go`,
batch size 500). Marginal cost is **linear in added edge rows**: each new edge
type adds rows to the same UNWIND batching, so cost ∝ projected edges per
repository. The relevant figure for ranking is therefore *edges added per
1,000 existing CALLS/REFERENCES edges*, not wall-clock — wall-clock proof per
admitted type is owned by each implementation child's Performance Evidence.

Baseline: a representative object-oriented source file emits ~8–12 functions,
0–2 interfaces, and ~6–10 `function_calls`. CALLS/REFERENCES is the dominant
code-edge family today.

## 3. Candidate Inventory

| Candidate | Parser signal today | Languages with signal | Est. cardinality vs CALLS | Projection cost | Agent value |
| --- | --- | --- | --- | --- | --- |
| **IMPLEMENTS** | None emitted (Kotlin captures `classInterfaces` internally for dead-code only) | Explicit keyword: Java, Kotlin, TypeScript, PHP, C#. Structural: Go (no keyword). | Low: ~0–2 per OO type; far below call volume | **Low** — one edge per (type, interface) pair; bounded by interface count | **High** — impact analysis, "who implements this contract", dead-code |
| **INSTANTIATES** | Emitted: `call_kind == "constructor_call"` already in `function_calls` | Python, Java, TypeScript/JavaScript, PHP | Medium: ~6 per file; today already becomes a CALLS edge to the constructor | **Low** — signal exists; cost is re-typing the existing constructor edge, not new traversal | **High** — "what builds this type", construction graph, dead-code |
| FIELD_ACCESS / ACCESSES | None emitted (AST `field_access`/`selector`/`member_access` used for inference only) | All | **High: ~10–20 per file**, exceeding call volume | **High** — would roughly double code-edge cardinality | Medium — disambiguating read vs write needs more work |
| RE_EXPORTS | Emitted: `import_type == "reexport"` in imports | TypeScript/JavaScript only | Medium: ~12 per barrel file, TS/JS only | Medium — already consumed by `resolveReexportedCrossFileCallee` for *resolution*; an explicit edge is additive | Medium — module-boundary navigation |
| DECORATOR / ANNOTATION | Emitted: `decorators` array on functions/classes | Python, Java, Kotlin, TypeScript | Low–medium: ~1–5 per file | Low | **Low** — metadata signal, not code control/data flow |

## 4. Ranking And Rationale

1. **INSTANTIATES (slice 1)** — best value-to-cost. The `constructor_call`
   signal is already emitted and already resolves to the constructor entity, so
   the work is to *distinguish* construction from invocation: keep the existing
   `CALLS`-to-constructor edge and additionally (or instead) project an
   `INSTANTIATES` edge from the caller to the constructed **type**. Near-zero new
   parser work; bounded extra rows.
2. **IMPLEMENTS (slice 1)** — highest agent value (contract impact, dead-code),
   low cardinality. Cheap for explicit-keyword languages (Java, Kotlin, TS, PHP,
   C#) which need only a new emitted field (`implemented_interfaces`) plus a
   reducer projection to an `IMPLEMENTS` edge `(Type)-[:IMPLEMENTS]->(Interface)`.
   **Go structural implements is explicitly out of slice 1**: it requires
   method-set matching against interface method sets (no `implements` keyword),
   which is materially more expensive and risk-prone. Recorded as a follow-up.
3. **EXPLAINS (#2230)** and **DOCUMENTS (#2231)** — already scoped; this analysis
   confirms them as the correct next investments because they unlock cross-layer
   (code↔intent, doc↔workload) questions no call-graph edge can answer, and both
   keep bodies in the Postgres content store (design 430), so graph cardinality
   stays identity-only.
4. **RE_EXPORTS** — deferred. The signal already feeds resolution; an explicit
   edge is navigation sugar, TS/JS-only, lower marginal value than slice 1.
   Reconsider after slice 1 ships.
5. **DECORATOR/ANNOTATION** — deferred. Low control/data-flow value; risk of
   high-cardinality metadata noise. Revisit only with a concrete agent question.
6. **FIELD_ACCESS** — deferred with the strongest reason: it is the **highest
   cardinality** candidate (~10–20/file, exceeding call volume) and has **no
   parser signal today**. Admitting it first would roughly double code-edge
   projection cost for medium value. It must wait for its own measured
   throughput proof and a read/write distinction design.

## 5. First-Slice Scope (handoff to #2229)

- New edges: `IMPLEMENTS` `(Class|Struct)-[:IMPLEMENTS]->(Interface)`,
  `INSTANTIATES` `(Function|File)-[:INSTANTIATES]->(Class|Struct)`.
- Languages: explicit-implements set (Java, Kotlin, TypeScript, PHP, C#) for
  IMPLEMENTS; existing `constructor_call` languages (Python, Java, TS/JS, PHP)
  for INSTANTIATES. Per-language fixtures required.
- Parser change: emit `implemented_interfaces` (names) on class/struct rows for
  the explicit-keyword languages; INSTANTIATES reuses `constructor_call`.
- Reducer change: project the two new edge types reusing the existing
  name-resolution index (interface name → Interface uid; constructed type name →
  Class/Struct uid) so no new N+1 traversal is added.
- Edges remain on the existing batched `UNWIND` `MERGE` path; Performance
  Evidence (before/after edge-write throughput on a representative fixture) is
  owned by #2229.
- Fixture intent, reducer graph truth, and API/query truth must agree per the
  epic acceptance.

## 6. Non-Goals / Recorded Deferrals

- Go structural-implements (method-set matching) — follow-up after slice 1.
- FIELD_ACCESS, RE_EXPORTS as explicit edges, DECORATOR/ANNOTATION edges —
  deferred with reasons in §4; each needs its own cardinality-and-throughput
  proof before admission.
- No LLM-derived edges; deterministic extraction only (epic #2219 non-goal).
- No bodies as graph properties; design-430 split preserved for #2230/#2231.
