# Typed Parser Artifact Contract For Symbols, Calls, Scopes, And Resolution Provenance (issue #3155)

Status: Proposed. Gate for epic #3154. Must record a decision before any
implementation that replaces loose parser-to-graph payload keys with typed
artifacts.
Issue: #3155. Parent epic: #3154.

## 1. Decision

Introduce an **optional, additive, typed parser-artifact layer** for the
precision-critical code surfaces — symbols, scopes, imports, calls, references,
and receiver/type hints — plus the resolution **confidence** and **provenance**
that today is computed downstream. The typed layer is a Go contract in a new
leaf package (`go/internal/parser/codeartifact`) that language adapters MAY
populate and that serializes **into the existing `map[string]any` payload
buckets** with no change to the wire shape consumed by `content/shape` or the
reducer.

The decision is **contract-only**: this document defines the types, the
back-compat mapping, the unsupported-language fallback, and the
performance/storage envelope. It does **not** change runtime behavior. No edge
is added, dropped, or re-typed by this slice. Implementation that adopts the
contract per-language is owned by follow-up children, each carrying its own
failing-fixture-first proof.

Rationale: GitNexus-class precision (typed phases, scope resolution, cross-file
binding propagation, receiver constraints) is held back in Eshu by the loose
`map[string]any` parser payload. Resolution and provenance are reconstructed in
the reducer from string hint keys (`receiver_is_import_alias`,
`inferred_obj_type`, `enclosing_class_contexts`) that have no compile-time
shape, no exhaustiveness guarantee, and no single owner. A typed layer makes the
precision contract explicit and testable without a risky big-bang rewrite of the
payload wire format.

## 2. Current State

Parsers emit a `map[string]any` payload (`parser/shared.BasePayload`,
`go/internal/parser/shared/shared.go:66`) with documented buckets —
`functions`, `classes`, `imports`, `function_calls`, `variables`, and ~70 more
consumed by `content/shape.Materialize` via `contentEntityBuckets`
(`go/internal/content/shape/materialize.go:54`). Each bucket entry is itself a
`map[string]any`.

Resolution does **not** happen in the parser (confirmed by ADR #2222 §2). The
parser emits raw call rows plus untyped resolution *hints*; the callee `uid` is
bound later in the reducer's ordered fallback dispatch
(`go/internal/reducer/code_call_language_resolver.go` and the
`code_call_language_*_resolver.go` family). The succeeding branch is mapped to a
closed `resolution_method` and a derived `confidence` by `codeprovenance`
(`go/internal/codeprovenance/codeprovenance.go`), per ADR #2222.

Consequences of the loose layer:

- Receiver/type hints are free-form string keys per language. There is no
  compile-time guarantee that an adapter populates the keys the reducer reads,
  and no exhaustiveness check when a key is renamed.
- Symbol identity, scope spans, and import bindings are reconstructed in the
  reducer index per call, rather than asserted once by the parser that has the
  AST.
- Confidence/provenance is reducer-derived. The parser, which holds the actual
  resolution evidence (lexical scope, import alias, inferred receiver type),
  cannot record *why* it believes a binding, only leave a hint for the reducer
  to re-derive.

`Envelope.SourceConfidence` (`go/internal/facts/models.go:34`) is a coarse,
envelope-level string (`observed`/`reported`/`inferred`/...), not a per-artifact
resolution confidence; it is orthogonal to the per-edge `confidence` of ADR
#2222.

## 3. Non-Goals

- No change to the payload wire format. Typed artifacts serialize into the
  existing buckets; `content/shape` and reducer consumers are untouched by this
  slice.
- No change to admission, edge creation, or the `resolution_method`/`confidence`
  vocabulary of ADR #2222. The typed layer *carries* that vocabulary earlier; it
  does not redefine it.
- No promotion of heuristic hints to canonical truth, and no new unbounded
  payload. Typed artifacts hold the same fields the reducer already reads.
- No LLM-derived artifacts; deterministic extraction only (epic #2219 / #2705
  non-goal).
- No move of resolution into the parser in this slice. The contract makes the
  parser *able* to record stronger resolution evidence; whether and when the
  reducer trusts parser-resolved bindings over its own index is a separate,
  measured decision owned by a follow-up.

## 4. Typed Artifact Contract

A new leaf package `go/internal/parser/codeartifact` defines the types below.
The package is dependency-safe (stdlib only, like `parser/shared` and
`exposure`) so language adapters under `parser/<lang>/` may import it without a
cycle. Each type carries a `ToBucketRow() map[string]any` method producing the
**exact** map the corresponding existing bucket already accepts, so adoption is
field-for-field and reviewable against current fixtures.

### 4.1 Identity and location

```go
// SymbolID is a stable, generation-independent symbol identity. For SCIP-backed
// symbols it is the SCIP symbol string; otherwise it is the parser's existing
// derived name/path key. It mirrors the today-untyped "scip_symbol" hint.
type SymbolID string

// Span is a half-open source range. Lines are 1-based, columns 0-based, matching
// the existing start_line/end_line/start_col bucket fields.
type Span struct {
    StartLine, EndLine int
    StartCol, EndCol   int
}
```

### 4.2 Symbols and scopes

```go
// SymbolKind is a distinct type (not a bare string) aligned with content/shape
// entity labels (Function, Class, Interface, Struct, Variable, ...). The closed
// set is enforced by a validator + golden, not by the Go type system, so an
// unrecognized kind fails a test rather than silently shipping.
type SymbolKind string

// Symbol is a declared entity the parser found in one file.
type Symbol struct {
    ID       SymbolID
    Kind     SymbolKind
    Name     string
    Span     Span
    ScopeID  ScopeID     // enclosing scope, ScopeFile if top-level
    Exported bool        // language-defined visibility (Go caps, TS export, __all__)
}

// ScopeID identifies a lexical scope within a file. ScopeFile is the file root.
type ScopeID string

// Scope is a lexical region used to bound same-file unique-name resolution.
type Scope struct {
    ID       ScopeID
    Parent   ScopeID
    Span     Span
    Kind     ScopeKind // file, function, block, class
}
```

### 4.3 Imports and references

```go
// Import records one import/require/use binding with its alias and resolution
// target, replacing the untyped import bucket keys (alias, import_type,
// resolved_source, namespace).
type Import struct {
    Module       string   // imported module/package path as written
    Alias        string   // local alias, empty if none
    Kind         ImportKind // direct, namespace, reexport, dynamic
    ResolvedPath string   // tsconfig/go.mod-resolved file, empty if unresolved
    Symbols      []string // named imports, empty for whole-module
}

// Reference is a non-call use of a symbol (type reference, composite literal,
// implements/instantiates signal) that materializes as REFERENCES, not CALLS.
type Reference struct {
    Name     string
    Span     Span
    Kind     ReferenceKind // type_reference, composite_literal, class_reference, ...
}
```

### 4.4 Calls and receiver/type hints

```go
// Call is one call site with the typed receiver/type evidence the reducer's
// receiver-constrained resolvers consume. Every field maps to an existing
// function_calls bucket key (name, full_name, line_number, call_kind,
// inferred_obj_type, enclosing_class_contexts, argument_count, argument_types).
type Call struct {
    Name        string
    QualifiedName string
    Span        Span
    CallKind    CallKind     // method, function, constructor, dynamic, ...
    Receiver    *ReceiverHint // nil when no receiver evidence
    ArgCount    int
    ArgTypes    []string     // best-effort, may be empty
}

// ReceiverHint is the typed form of today's inferred_obj_type +
// enclosing_class_contexts + receiver_is_import_alias hints. It is the single
// place receiver-constrained resolution evidence lives.
type ReceiverHint struct {
    InferredType   string   // receiver/return-type inference result
    EnclosingTypes []string // nested class/receiver context chain, outer-first
    FromImportAlias bool    // receiver bound to an import alias
}
```

### 4.5 Resolution provenance carried by the parser

```go
// ResolutionEvidence is the typed, optional record of how the *parser* believes
// a call/reference binds, when the parser has stronger evidence than a name.
// It reuses the ADR #2222 closed vocabulary (codeprovenance.Method) so the
// reducer can adopt it without inventing a tier. nil means "no parser-level
// resolution; reducer resolves as today".
type ResolutionEvidence struct {
    Method   codeprovenance.Method // scip, declared, same_file, import_binding, ...
    TargetID SymbolID              // resolved target when Method is binding-grade
    ScopeID  ScopeID              // bounding scope for same_file/scope_unique_name
}
```

`confidence` is **not** stored on the artifact: it remains a presentation-tier
derivation of `Method` (ADR #2222 §4), recomputed in one place. The artifact
carries the *method* (the source of truth); confidence is derived on write.

## 5. Backward Compatibility With Reducer And Read-Model Consumers

The contract is additive and serialized through the unchanged buckets:

| Consumer | Today reads | After this contract |
| --- | --- | --- |
| `content/shape.Materialize` (`materialize.go:206`) | bucket `map[string]any` rows | identical rows via `ToBucketRow()`; no key added or removed |
| reducer call resolvers (`code_call_language_*_resolver.go`) | string hint keys (`inferred_obj_type`, `enclosing_class_contexts`) | same keys; adapters that adopt typed `ReceiverHint` emit byte-identical keys |
| `codeprovenance` (`codeprovenance.go`) | reducer-selected branch → method | unchanged; may later read parser `ResolutionEvidence.Method` when present |
| API/MCP relationship rows (`code_relationship_story_provenance.go`) | edge `resolution_method`/`confidence` | unchanged |

Mapping rules (normative for adopters):

- A `ToBucketRow()` method MUST reproduce the current bucket keys exactly. A
  golden test per adopted language asserts the typed path and the legacy path
  produce identical payloads (the only safe adoption gate).
- Adoption is **per language, per bucket**, behind no flag: because the output
  is identical, a converted adapter is indistinguishable downstream. The win is
  compile-time exhaustiveness in the adapter, not a behavior change.
- `ResolutionEvidence` is the one genuinely new optional field. It serializes to
  an additive key (`parser_resolution_method`) that **no current consumer
  reads**, so emitting it is inert until a follow-up wires reducer adoption.
  Readers treat its absence as "no parser resolution" (today's behavior).

## 6. Unsupported-Language Fallback Semantics

Eshu parses 34+ languages; only a subset has the AST depth for typed
receiver/scope artifacts. Fallback is explicit and tiered. The load-bearing
rule is the **hint-preservation invariant** below: a tier describes how *much* a
language can populate, never permission to drop a hint it emits today.

- **Tier A — receiver-capable**: languages whose adapters already emit
  receiver/type or resolution hints that a reducer resolver consumes today —
  Go (`full_name`, method-return chains), TypeScript/JavaScript
  (`inferred_obj_type`, import aliases), Python (`class_context`, qualified
  `full_name`), Java (`inferred_obj_type`, `enclosing_class_contexts`), Rust
  (`inferred_obj_type` via `go/internal/parser/rust/helpers.go`, consumed by
  `resolveRustTraitBoundReceiverCallee`), Groovy (`inferred_obj_type`), and
  Kotlin/C# (constructor/return-chain and explicit-type receiver inference).
  These MAY adopt the typed `ReceiverHint` and MUST populate it from the same
  evidence they emit today.
- **Tier B — symbol-only**: languages that emit no receiver/type hint today
  (e.g. C/C++ and plain-scripting adapters). Their typed rows carry
  `Receiver == nil` and `ResolutionEvidence == nil` **because the parser has no
  such evidence**, not as a downgrade. The reducer's name-and-scope resolution
  is unchanged.
- **Tier C — non-AST / declarative** (YAML, HCL, Dockerfile, SQL, raw text):
  out of scope for the call/scope contract. They keep their current buckets.
  `codeartifact` types are never required for them.

**Hint-preservation invariant (normative):** adopting the typed layer for any
language MUST preserve every receiver/type/resolution hint key that language
emits today — `ToBucketRow()` is byte-identical to the current payload, proven
per language by the adoption golden (§5). `Receiver`/`ResolutionEvidence` may be
nil **only** where the language emits no such hint today. A receiver-capable
language is never reclassified as symbol-only; doing so would regress the
reducer's `MethodTypeInferred` resolution (e.g. Rust trait-bound receivers via
`resolveRustTraitBoundReceiverCallee`, or Groovy/Java `inferred_obj_type`). No
tier is invented to make an edge look more certain than the evidence supports
(honesty contract, ADR #2222 §3).

## 7. Performance And Storage Cost Estimate

Estimated before implementation, per the epic proof expectation.

**CPU / allocation (parse hot path).** Typed artifacts are constructed in the
adapter and immediately lowered to the existing maps via `ToBucketRow()`. The
marginal cost is one struct allocation + one map write per artifact that already
exists — i.e. the same map row, built through a typed value first. Expected
overhead: a single extra short-lived allocation per bucket row, dominated by the
existing tree-sitter walk. Adopters MUST include a `Benchmark Evidence:` marker
comparing `BenchmarkParse<Lang>` before/after on a representative fixture; the
stop threshold is **no measurable regression beyond allocation noise** on the
parser benchmark.

**Storage / wire.** Zero by construction for Tier A/B symbols, calls, and
imports: the serialized buckets are byte-identical. The only additive bytes are
the optional `parser_resolution_method` string on call rows that carry parser
resolution — one short enum string per resolved call, bounded by call count.
Estimated from the taxonomy fixture corpus (`tests/fixtures/sample_projects/`,
~6–10 `function_calls` per OO file): ≤ ~10 extra short strings per file, only
when a follow-up enables emission. Until then, zero.

**Graph write.** Unchanged: this slice writes no new edges or properties. When a
follow-up adopts `ResolutionEvidence.Method` as the reducer's method source, the
edge `SET` shape is identical to ADR #2222 (a parameterized `resolution_method`
already exists). No new UNWIND/MERGE cardinality.

## 8. Design Review: Cross-Surface Implications

**Parser** (`go/internal/parser/`, owner per agent-guide): gains a leaf
`codeartifact` package and optional typed construction in Tier A adapters. Must
preserve the determinism invariant (same bytes → same output) and the
`ToBucketRow()` identity property. New package needs `doc.go` + `README.md` +
`AGENTS.md`.

**Reducer** (`go/internal/reducer/`): no change in this slice. The contract is
designed so a later child can let receiver-constrained resolvers prefer parser
`ResolutionEvidence` when present, falling back to the index otherwise — strictly
additive, gated by goldens (#3156).

**API / MCP** (`go/internal/query`, `go/internal/mcp`): no change in this slice.
The contract feeds the per-edge `resolution_method`/`confidence` already
surfaced (#2222 / epic child #3158). No envelope change.

**Graph backend** (`go/internal/storage/cypher`, NornicDB/Neo4j): no DDL, no new
index, no new property in this slice. The only future property
(`resolution_method`) already exists in the schema. Backend-neutral; no dialect
work.

## 9. Evidence Plan (owned by children)

- Contract adoption per language: failing golden first asserting typed-path and
  legacy-path payloads are byte-identical (`ToBucketRow()` identity), then the
  adapter conversion.
- Receiver-constraint precision: covered by #3156 goldens (positive + negative,
  per-edge provenance/confidence), which assert against this contract's
  `ReceiverHint`/`ResolutionEvidence` vocabulary.
- `parser_resolution_method` wiring (future child): failing fixture proving the
  reducer adopts the parser method only when present and stronger, with a
  No-Regression Evidence marker on edge-write throughput.
- New `codeartifact` package: `scripts/verify-package-docs.sh` green;
  `Benchmark Evidence:` on the parser benchmark when an adapter adopts the types.

## 10. Non-Goals / Recorded Deferrals

- Moving resolution wholesale into the parser — deferred; the contract enables
  it incrementally, gated by measured precision.
- Typed artifacts for Tier C declarative languages — out of scope; they keep
  current buckets.
- A new envelope-level or per-artifact `confidence` field — rejected; confidence
  stays a derivation of `resolution_method` (ADR #2222) to prevent drift.
- Replacing the `map[string]any` wire format — explicitly not done; this slice
  is a typed *layer above* the unchanged wire, the only low-risk adoption path.
