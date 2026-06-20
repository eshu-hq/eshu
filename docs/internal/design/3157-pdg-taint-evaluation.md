# Evaluation: GitNexus-Style PDG And Taint Query Layer vs Eshu Value-Flow (issue #3157)

Status: Proposed (research outcome). Gate for epic #3154. Records a go/no-go on
adopting a classical Program Dependence Graph (PDG) and PDG-backed query layer.
Issue: #3157. Parent epic: #3154.

## 1. Decision (go/no-go)

**NO-GO on a persisted, classical whole-program PDG and a PDG slicing query
layer.** **GO on a bounded set of targeted deterministic enhancements** to the
value-flow stack Eshu already has, plus the config/IaC sink proof corpora the
epic calls out.

Reason in one line: Eshu already implements the load-bearing two-thirds of a PDG
— data-dependence (reaching definitions / def-use) and an interprocedural taint
solver with a kind-set sanitizer model — as a deterministic, gated, evidence-only
layer. A full classical PDG adds **explicit control-dependence edges and program
slicing** at materially higher cardinality and complexity, for value that, for
Eshu's code-to-cloud truth mission, is largely already captured by the taint
solver. The minimum viable deterministic subset worth adding is **intraprocedural
control-dependence used only to sharpen sanitizer-guard precision** (kept inside
the CFG, not persisted as graph edges), and **new sink corpora** for config and
IaC. Everything else is a recorded non-goal.

## 2. What A Classical PDG Is (baseline for comparison)

A Program Dependence Graph (Ferrante, Ottenstein & Warren, 1987) unifies two
edge families over program statements:

- **Data dependence (DD):** statement `u` uses a value defined at `v` with no
  intervening redefinition on some path (reaching definitions).
- **Control dependence (CD):** whether statement `u` executes is governed by the
  branch outcome at predicate `p` (post-dominator frontier of the CFG).

PDG-backed analyses are then graph reachability: **program slicing** (backward:
"what affects this value"; forward: "what does this value affect") and taint
(source→sink reachability that respects sanitizers). GitNexus-style tooling, per
its public characterization and the reference already recorded in
`go/internal/parser/summary/README.md`, builds taint on Sharir-Pnueli TITO
function summaries over this substrate. This evaluation compares **concepts**
only; it imports no GitNexus source or proprietary internals.

## 3. Eshu's Current Value-Flow Surfaces

All deterministic, gated behind `ESHU_EMIT_DATAFLOW` (off by default;
`docs/public/reference/value-flow-emission.md`), and emitted as **evidence,
never canonical truth**.

| PDG concept | Eshu surface today | Status |
| --- | --- | --- |
| Control-flow graph | `go/internal/parser/cfg` — basic blocks, successors, bounded with overflow counts | **Have** |
| Data dependence (reaching defs) | `cfg/reaching.go` — monotone def-set fixpoint, def→use edges (`DefUse`) | **Have** |
| Intraprocedural taint | `go/internal/parser/taint` — kind-set sanitizers (HTML escaper ≠ SQL-safe), intersection at merges, deterministic/bounded | **Have** |
| Function summaries (TITO) | `go/internal/parser/summary` — Sharir-Pnueli abstraction, SCC-aware versioning, durable `FunctionID` | **Have** |
| Interprocedural / cross-repo taint | `go/internal/parser/interproc` — port-graph reachability; closures/fields as named-slot ports; cloud sinks terminate code-to-cloud paths; `SolvePartitioned` race-free | **Have** |
| Source/sink/sanitizer catalogs | `go/internal/exposure` — closed `SinkKind`/`SourceKind` vocab, `GraphBacked` honesty discipline, pinned catalog version | **Have** |
| Language lowerings | Go (full), Python / TS-JS (intra-file), Java, C# (gated) via `pydataflow`, `jsdataflow`, language CFG emitters | **Have (uneven)** |
| **Explicit control-dependence edges** | none — taint encodes guard effects via the kind-set/sanitizer merge, not a CD edge set | **Gap** |
| **Program slicing query** | none — no backward/forward slice API | **Gap (intentional)** |
| **Config / IaC taint sinks** | exposure has `internet_exposed_endpoint`, `iam_privileged_action`, `secret_reference`; taint has SQL + shell | **Partial** |

The honesty contract is already enforced: `exposure.MatchSink` refuses non
`GraphBacked` specs (`sink_catalog.go`), and `BuildExposureFinding` returns an
`unresolved` finding with a reason rather than fabricating a path
(`path_trace.go`). Findings carry confidence and provenance and never raise the
answer-level `TruthEnvelope` (ADR #2222 §7).

## 4. Gap Analysis: PDG Concepts Not Yet In Eshu

1. **Explicit control-dependence edges (CD).** A classical PDG persists CD edges
   so a slice can answer "which predicate gates this statement". Eshu's taint
   model already accounts for guards *operationally*: a sanitizer on one branch
   neutralizes a kind only on that path, and the merge intersects neutralized
   sets, so a sink guarded by a sanitizer-on-all-paths is correctly cleared. The
   thing CD edges would add beyond this is **slice explainability** ("this sink
   is reachable only when predicate P is true"), not a different reachability
   verdict.

2. **Program slicing as a query.** A PDG slicing API (backward/forward) is a
   general code-comprehension feature. It is high-cardinality (a slice can touch
   most of a function/module) and overlaps Eshu's existing bounded call-chain and
   relationship-story reads. It is not required for code-to-cloud taint truth.

3. **Broader sink coverage.** The epic explicitly names command injection, SQL,
   shell, **config**, and **IaC** sinks. SQL and shell exist (taint + exposure);
   command injection ≈ shell_exec; **config and IaC sinks are genuinely missing**
   from the deterministic taint path.

## 5. Minimum Viable Deterministic PDG Subset (the GO scope)

Admit only what strengthens evidence-backed code-to-cloud truth at bounded cost:

- **MVP-1: Intraprocedural control-dependence inside the CFG.** Compute CD from
  the existing CFG (post-dominator frontier) and use it **only** to (a) attach a
  guard reason to a taint finding ("neutralized on the `if isAdmin` branch only")
  and (b) tighten sanitizer-on-all-paths precision. **Not persisted as graph
  edges.** It lives in `cfg`/`taint` as in-memory analysis and surfaces as
  finding provenance text. Bounded by existing CFG block limits.
- **MVP-2: Config + IaC sink corpora and catalog entries.** Extend the closed
  `exposure` sink vocabulary with config-secret and IaC-misconfig sinks under the
  same `GraphBacked` discipline (no fabricated matches), with proof corpora (§7).
  This is the highest-value, lowest-risk addition because it directly widens the
  evidence base the existing solver already knows how to traverse.
- **MVP-3: Finding-level slice explanation (read-only).** Where a taint finding
  exists, expose a bounded, ordered "why" trail (source port → intermediate
  ports → sink) already present in `interproc.Finding`/`exposure.ExposureFinding`,
  surfaced through the API/MCP provenance work in child #3158. No new graph
  structure; this reuses what the solver already produces.

## 6. Non-Goals (the NO-GO scope, recorded with reasons)

- **Persisted whole-program PDG.** Rejected: CD+DD over every statement is the
  highest-cardinality structure in the design space (def/use and member access
  already measured as ~10–20/file in #2228), and it would compete with the
  graph-write hot path for marginal verdict value over the existing taint solver.
- **General program-slicing query API.** Rejected for this epic: high
  cardinality, overlaps existing bounded reads, not needed for taint truth.
  Reconsider only with a concrete agent question and its own throughput proof.
- **Default-on value-flow.** Rejected: value-flow stays gated behind
  `ESHU_EMIT_DATAFLOW` (off by default) per the launch decision in
  `value-flow-emission.md`. Any config/IaC sink work inherits the same gate.
- **LLM- or similarity-derived dependence edges.** Rejected: deterministic
  extraction only (epic #2705 non-goal).
- **Promoting taint findings to canonical edges.** Rejected: findings remain
  evidence under the honesty contract; the `TruthEnvelope` is unchanged.

## 7. Proof Corpora (named, per the acceptance criteria)

Each corpus is a deterministic fixture set with positive and negative cases and
expected confidence/provenance. They extend the existing
`go_cfg_taint_test.go` / `*_cfg_dataflow_test.go` and `exposure/*_test.go`
patterns; no new harness.

| Sink class | Corpus intent | Status / where |
| --- | --- | --- |
| Command injection | user input → `Process.Start`/`exec`/shell with and without sanitizer; negative: constant/allow-listed arg | extend `taint` shell sink fixtures; C# `Process.Start` exists |
| SQL | tainted param → `SqlCommand`/query builder; negative: parameterized query (sanitized) | exists (`go_cfg_taint_test.go`, C# ADO.NET); add Python/JS |
| Shell | env/arg → shell exec; negative: fixed command | exists; extend negatives |
| **Config** (new) | untrusted value written to a security-relevant config key (e.g. disabling TLS verify, world-readable perms); negative: constant safe value | **new** corpus under `exposure` + taint sink entry |
| **IaC** (new) | tainted/templated value into an IaC misconfig sink (public bucket ACL, `0.0.0.0/0` ingress, plaintext secret); negative: restricted value | **new** corpus, cross-referenced with `relationships` (Terraform/Helm) extraction |

Negative cases are mandatory: a corpus that only proves positives would let a
future change silently over-report. The `GraphBacked`/`MatchSink` discipline and
the pinned `SinkCatalogVersion` golden guard against catalog drift.

## 8. Storage, Reducer, And Query Implications

- **Storage:** MVP-1 and MVP-3 add **zero** persisted structure (in-memory CD,
  reuse of existing finding fields). MVP-2 adds bounded `exposure` catalog
  entries and the same evidence rows the gated buckets already emit; no new edge
  family beyond the existing exposure materializers.
- **Reducer:** no new projection stage. Config/IaC sinks reuse the existing
  exposure materialization path and the `resolved_relationships` consumers
  (post-Phase-3 reopen already exists for relationship evidence). Any new sink
  edge stays on the batched `UNWIND`/`MERGE` write path.
- **Query / MCP:** no new endpoint. Finding-level slice explanation (MVP-3) is
  delivered by child #3158's confidence/missing-edge provenance surfacing; it
  reads fields the solver already produces and must not upgrade evidence to
  canonical truth.
- **Backend:** backend-neutral; no DDL, no new index. Honors NornicDB/Neo4j
  shared Cypher contract.

## 9. Evidence Plan (owned by follow-up children)

Gate issues spun off from this ADR:

- **#3191 (MVP-2, highest value):** config + IaC sink catalog + proof corpora.
  Failing fixtures first (positive + negative), `SinkCatalogVersion` golden
  updated deliberately, exposure honesty tests extended. Gated by
  `ESHU_EMIT_DATAFLOW`.
- **#3193 (MVP-1):** intraprocedural control-dependence in `cfg`/`taint` for
  guard-reason provenance and sanitizer-all-paths precision. Failing fixture
  proving a guarded sink reports the gating predicate; determinism + CFG-bound
  proof.
- **#3194 (MVP-3):** finding-level slice "why" trail surfaced via #3158's
  provenance fields; API/MCP parity test.

No-Regression Evidence on the parser benchmark is required for any change that
adds analysis to the parse hot path (MVP-1).

## 10. Summary

Eshu does not need a classical PDG to reach code-precision parity; it needs to
**finish and widen** the deterministic value-flow layer it already has. Adopt the
bounded MVP subset (intraprocedural CD for explainability, config/IaC sinks,
finding-level slice trails), keep value-flow gated and evidence-only, and reject
the persisted whole-program PDG and the general slicing query as out-of-mission
cardinality. Go on the three MVP children; no-go on the full PDG.
