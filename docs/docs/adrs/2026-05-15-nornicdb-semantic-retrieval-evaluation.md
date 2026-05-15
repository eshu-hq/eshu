# ADR: NornicDB Semantic Retrieval, Freshness Scoring, And Candidate Evidence Evaluation

**Date:** 2026-05-15
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

**Related:**

- `2026-04-22-nornicdb-graph-backend-candidate.md`
- `2026-04-20-embedded-local-backends-implementation-plan.md`
- `2026-05-14-mcp-tool-contract-performance-audit.md`
- `2026-05-14-service-story-dossier-contract.md`
- `../reference/truth-label-protocol.md`
- `../reference/mcp-tool-contract-matrix.md`
- `../reference/telemetry/index.md`
- NornicDB capability docs at `orneryd/NornicDB@210c4c6765f3af8f2cb9b8e0c6ab71ddf5017e2b`:
  `https://github.com/orneryd/NornicDB/tree/210c4c6765f3af8f2cb9b8e0c6ab71ddf5017e2b/docs/user-guides`
  and
  `https://github.com/orneryd/NornicDB/tree/210c4c6765f3af8f2cb9b8e0c6ab71ddf5017e2b/docs/features`
- Issue: `#396` - evaluate NornicDB semantic retrieval, decay, and candidate
  evidence for MCP/API recall

---

## Context

Eshu already uses NornicDB as the default graph backend behind the
backend-neutral graph ports. That use is intentionally narrow: Eshu writes
canonical graph truth through the reducer/projector path, then API and MCP read
the canonical graph through bounded query handlers.

NornicDB now exposes more than property-graph storage:

- vector search through `db.index.vector.queryNodes`
- managed and client-managed embeddings
- BM25 plus vector hybrid search with reciprocal-rank fusion
- optional cross-encoder reranking
- declarative decay and promotion policies
- visibility suppression and deindex cleanup
- GDS-style link-prediction procedures
- optional automatic relationship inference
- Qdrant-compatible gRPC and additive Nornic search gRPC
- GraphQL and APOC/plugin surfaces
- MVCC historical reads and multi-database namespaces

Those capabilities can improve Eshu's read-side product experience, but they
also create a truth-boundary risk. Eshu's product contract is not "whatever the
database can infer." Eshu's contract is facts-first admission, reducer-owned
materialization, explicit truth labels, bounded MCP/API calls, and observable
runtime behavior.

The question for this ADR is therefore not "should Eshu turn on all NornicDB AI
features?" The question is: which NornicDB capabilities can improve Eshu's
recall, ranking, freshness, and relationship-gap discovery without weakening
canonical truth?

## Research Summary

NornicDB's docs describe vector search as a first-class search path with
automatic indexing for managed embeddings and Cypher procedure support through
`db.index.vector.queryNodes`. Embedding generation is disabled by default in
current releases and must be enabled explicitly or supplied by clients.
User-defined vector indexes are metadata that specify label/property scope; the
query path can resolve vectors from named embeddings, vector properties, or
managed chunk embeddings.

NornicDB hybrid search combines vector similarity, BM25 full-text ranking, and
reciprocal-rank fusion. By default the embedding side and BM25 side can see all
node properties. That default is useful for general knowledge graphs but too
broad for Eshu unless Eshu controls which semantic context records are indexed.

NornicDB cross-encoder reranking is an optional stage after initial retrieval.
It can improve ranking quality but adds model loading, latency, and operational
surface. It should be evaluated after baseline hybrid retrieval proves value.

NornicDB knowledge-layer policies provide decay profiles, promotion policies,
visibility suppression, `decayScore`, `policy`, `reveal`, and
`nornicdb.knowledgepolicy.*` diagnostics. Suppressed entities are hidden from
normal MATCH and search results while primary data remains intact. This is a
ranking and visibility feature, not a replacement for Eshu's durable fact
store or deletion model.

NornicDB link prediction exposes topological algorithms such as common
neighbors, Jaccard, Adamic-Adar, resource allocation, preferential attachment,
and hybrid topology plus semantic scoring. The stream procedures return
suggestions and do not need to mutate the graph. Auto-TLP, by contrast, can
automatically create edges on store or access events. That materialization
behavior conflicts with Eshu's reducer-owned graph admission unless it is
strictly isolated from canonical writes.

NornicDB MVCC historical reads can answer snapshot questions, but current search
indexes are current-state focused. Eshu already stores fact generations,
projection decisions, queue history, content, and status in Postgres, so MVCC is
useful for future comparison/read-model work but is not the first semantic
retrieval dependency.

NornicDB exposes GraphQL, APOC, Qdrant-compatible gRPC, and Nornic search gRPC.
Those protocols are valuable integration options, but Eshu's public API and MCP
contracts should continue to route through Eshu handlers so truth labels,
freshness, limits, and scope checks remain consistent.

## Decision

Evaluate NornicDB semantic and scoring capabilities as an additive read-side
retrieval layer. Do not change canonical graph admission in this ADR.

Eshu will treat the first semantic path as a ranked context overlay:

```text
question -> bounded semantic/hybrid retrieval -> graph/content expansion ->
truth-labeled answer packet
```

The overlay may improve discovery and ranking, but it must not claim canonical
truth. The reducer remains the only owner of canonical graph truth.

The initial accepted direction is:

- build an evaluation harness before changing runtime behavior
- index only bounded Eshu-authored semantic context records, not arbitrary graph
  node properties
- keep Postgres as the durable owner of facts, queues, content, projection
  state, evidence, and checkpoints
- use NornicDB hybrid search for top-K candidate retrieval after explicit
  scoping and limits
- use decay only for selected non-canonical evidence ranking and visibility
- use link prediction only for candidate evidence or diagnostics unless a later
  ADR proves a reducer-owned admission flow
- keep direct GraphQL, APOC, Auto-TLP mutation, and model inference out of the
  first public MCP/API contract

## What Changes

The current Eshu read model mostly answers from exact indexed truth. The
proposed model keeps that authority and adds a second read layer for semantic or
decayed candidates. The product gain is not automatic canonical accuracy. The
gain is recall, ranking, and operator context:

- ask "what handles token validation?" and retrieve semantically related code,
  docs, config, services, and evidence even when names differ
- assemble service and repository dossiers from exact graph facts plus ranked
  nearby context
- rank recent CI, vulnerability, deployment, or cloud evidence above stale
  evidence without deleting the stale facts
- expose likely missing relationships as candidates for investigation or future
  reducer work

## Postgres Ownership

Postgres remains the durable control plane and evidence store.

Postgres continues to own collector facts, queue state, projection/reducer
status, content rows, projection decisions, semantic indexing checkpoints, and
candidate evidence emitted by semantic retrieval, decay evaluation, or link
prediction.

NornicDB owns canonical graph nodes and relationships, optional semantic context
nodes or indexed vectors created by Eshu, BM25/vector/hybrid indexes for the
semantic overlay, decay scoring for configured non-canonical labels or
relationship types, and link-prediction procedure execution for candidate
discovery.

The practical schema impact is expected to be additive:

- semantic projection status keyed by repo, run/generation, source handle,
  content hash, embedding model, NornicDB database, and indexed-at timestamp
- semantic projection work items if embedding/indexing cost becomes large
  enough to need a durable queue
- candidate evidence rows with score, rank, algorithm, query id, source handles,
  truth level, and expiration/freshness metadata

The first implementation must not move fact storage, queue ownership, reducer
leases, content truth, or admission decisions from Postgres into NornicDB.

## Evaluation Plan

Build the evaluation harness before enabling semantic retrieval in production
paths.

The eval suite should contain 50-100 real operator and developer questions, for
example:

- what owns auth token validation?
- what services use this Terraform module?
- what deploys this workload?
- what changed that could break checkout?
- where is this vulnerability relevant?
- why is this service failing in CI?
- what files explain how this service is configured?

Each eval case records question text, allowed scope, must-find handles,
acceptable supporting handles, must-not-include handles when known, and expected
truth class.

Run each case through two paths:

1. current exact Eshu MCP/API behavior
2. candidate NornicDB-backed semantic/hybrid behavior

Measure:

| Metric | Meaning | Acceptance direction |
| --- | --- | --- |
| `recall@10` | whether the expected handles appear in the first page | improve at least 25% before product adoption |
| `precision@10` | whether top results are actually relevant | must not materially regress |
| `nDCG@10` | whether better results rank earlier | primary ranking metric |
| false canonical claims | semantic candidates represented as exact truth | must remain 0 |
| p95 retrieval latency | MCP/API candidate retrieval and expansion latency | initial target under 2s |
| ingestion/index overhead | added wall time for semantic projection | bounded and reported |
| NornicDB index size | added disk/memory footprint | bounded by corpus size |
| queue health | retries, dead letters, stale projections | no new unbounded backlog |

The first acceptance target is:

```text
recall@10 improves by at least 25%
false canonical claims = 0
p95 MCP retrieval latency <= 2s
semantic projection overhead and index size are recorded
```

This is an evaluation target, not a promise that the first implementation will
meet it.

## Phased Implementation

### Phase 0: Baseline Eval Harness

Create the eval corpus and scoring harness against current Eshu behavior. This
phase does not require NornicDB embeddings or hybrid search.

Required evidence:

- checked-in eval cases or a checked-in fixture contract
- current-path `recall@10`, `precision@10`, `nDCG@10`, p95 latency, and false
  canonical claim count
- docs explaining how to add a new eval case without leaking private repo data

Initial implementation status:

- `go/internal/semanticeval` owns the strict JSON suite/run contract and scoring
  formulas for `recall@K`, `precision@K`, `nDCG@K`, false canonical claims,
  forbidden hits, unsupported cases, and mean latency.
- `go/internal/semanticeval/testdata` provides a checked-in fixture contract for
  shaping current-path and future semantic/hybrid runs without touching runtime
  query behavior.
- `go/internal/semanticeval/README.md` documents how to add eval cases while
  avoiding private code, secrets, customer names, and unredacted production
  incident text.

No-Regression Evidence: `cd go && go test ./internal/semanticeval -count=1`
passes for the scoring package and checked-in fixture contract.

No-Observability-Change: this slice adds an offline scoring package only. It
does not add runtime projection, retrieval, graph writes, queue work, HTTP, MCP,
Postgres, or NornicDB calls. The first runtime-backed retrieval slice must add
the semantic projection and retrieval telemetry listed in
[Observability Requirements](#observability-requirements).

### Phase 1: Semantic Context Projection Design

Define bounded semantic context records from existing Eshu truth, such as file
summaries, entity summaries, service/workload dossier summaries, deployment
evidence snippets, CI/vulnerability/package/cloud observations, and
documentation/runbook snippets.

The design must specify which fields are embedded, which fields stay metadata
only, max text length, chunking/deduplication, content hash and model/version
checkpointing, replay/retraction behavior, Postgres projection status shape, and
NornicDB label/index naming.

### Phase 2: NornicDB Hybrid Retrieval Prototype

Enable NornicDB embeddings/search only for explicit semantic context labels.

The query flow must:

- require scope first when available
- require limit and timeout
- return deterministic top-K ordering
- report truncation and search mode
- carry per-item truth labels
- graph-expand only from bounded candidate handles
- never fall back to unbounded whole-graph search

### Phase 3: Decay For Non-Canonical Evidence

Add decay policies only for evidence labels or relationship types that are
explicitly non-canonical, such as:

- CI run evidence
- vulnerability observations
- deployment events
- cloud observations
- weak inferred/candidate relationships

Canonical graph labels and admitted durable relationships must use no-decay or
must not be bound to decay profiles.

### Phase 4: Candidate Relationship Suggestions

Evaluate link-prediction procedures as a diagnostic candidate source.

Candidate relationships must include algorithm, score, source/target handles,
evidence context, freshness, reason, and an explicit `candidate` or
`semantic_candidate` truth level.

Candidate suggestions must not create canonical Eshu relationships unless a
future reducer-owned admission design accepts and materializes them.

### Phase 5: Reranking And Protocol Expansion

Evaluate cross-encoder reranking after hybrid retrieval shows value. GraphQL,
Qdrant gRPC, and Nornic native gRPC may be considered for internal integration
or performance work, but public MCP/API contracts should still route through
Eshu handlers.

## Performance Impact Declaration

Affected stages are semantic context projection from Postgres content and
reducer decisions, NornicDB embedding generation or client-managed vector
upload, NornicDB search index maintenance, and MCP/API retrieval plus
graph-expansion answer assembly.

Expected cardinality is bounded by selected semantic context records, not all
graph nodes. Start on fixture/eval corpora, then measure small, medium, and full
Eshu corpora before promotion.

Known baselines are current exact MCP/API behavior from Phase 0 and the current
NornicDB graph backend full-corpus evidence governed by the NornicDB backend ADR.

Proof ladder:

1. focused fixture/eval corpus
2. one small real repo
3. medium representative repo set
4. full corpus only after focused and medium lanes meet queue and latency gates

Stop thresholds:

- false canonical claims above 0
- p95 retrieval latency above 2s on the eval suite without an accepted reason
- semantic projection creates retry/dead-letter backlog
- embedding/index overhead exceeds 10% of comparable indexing wall time without
  a bounded follow-up design
- NornicDB memory or disk growth lacks a corpus-size explanation

## Observability Requirements

Any implementation PR must expose:

- semantic projection queue depth, age, processing duration, and failures
- embedding/index operation duration and result counters
- NornicDB search request duration by mode: keyword, vector, hybrid, rerank
- retrieval result count, truncation, and candidate truth-level summaries
- decay policy application counts for non-canonical evidence
- candidate relationship generation counts by algorithm and decision
- structured logs with scope, capability, search mode, rank count, truncation,
  and failure class

High-cardinality values such as paths, entity ids, and raw query text must stay
out of metric labels. They may appear in bounded logs or spans when needed for
diagnostics.

## Security And Privacy

Semantic projection can leak more information into embeddings and indexes than
exact graph writes do. The first implementation must:

- exclude secrets and redacted values from embedding text
- avoid embedding full raw files by default
- keep raw query text out of metric labels
- document model provider, model version, and whether embeddings are local or
  external
- fail closed when an external embedding provider is configured without an
  explicit opt-in
- keep public-facing console/demo modes fixture-backed until auth exists

## Rejected Options

### Use NornicDB Auto-TLP As Canonical Relationship Builder

Rejected. Auto-TLP can materialize edges based on store/access events. That
conflicts with Eshu's reducer-owned admission model and would make canonical
truth depend on access patterns and feature flags.

### Embed Every Canonical Graph Node

Rejected. NornicDB's default embedding text can include all node properties.
Eshu graph nodes contain identifiers, metadata, operational state, and other
fields that are not all useful or safe as semantic text. Eshu must project a
bounded semantic context surface instead.

### Replace Postgres Content Search Immediately

Rejected. NornicDB hybrid search may reduce pressure on Postgres content search
for AI-context retrieval, but exact file/entity reads and content-store truth
remain Postgres-owned. Any replacement needs measured parity and a separate
ADR.

### Expose NornicDB GraphQL Directly As The Product API

Rejected for the first phase. Direct GraphQL bypasses Eshu's envelope, truth
labels, freshness, and MCP capability contract. It may be useful internally,
but public product paths should stay behind Eshu handlers.

### Turn On LLM Inference In The Database Path

Rejected for the first phase. Retrieval and ranking are enough to evaluate the
core value. In-database inference adds model lifecycle, latency, cost, and
security concerns before the retrieval layer proves value.

## Consequences

Positive:

- Eshu can answer with better recall when exact graph relationships are
  incomplete
- MCP/API service and repository dossiers can combine exact truth with ranked
  context
- freshness ranking becomes policy-driven for selected non-canonical evidence
- relationship gaps can become measurable candidate evidence instead of hidden
  misses

Tradeoffs:

- embedding and search indexing add write-path cost and operational state
- semantic results are relevance-ranked and must be truth-labeled carefully
- new eval, telemetry, and checkpointing infrastructure is required before
  product adoption
- NornicDB optional capabilities increase backend-specific surface area, so
  handlers need narrow ports rather than brand checks

## Acceptance

This ADR is accepted when:

- a GitHub issue tracks the evaluation work with metrics, phases, and gates
- the eval harness baseline is implemented and records current Eshu behavior
- an implementation plan defines semantic context projection, Postgres
  checkpoints, NornicDB labels/indexes, and retraction behavior
- focused semantic retrieval proof improves recall without false canonical
  claims
- performance and observability evidence are recorded in versioned repo files
  before runtime behavior is promoted

No runtime behavior is accepted by this ADR alone.
