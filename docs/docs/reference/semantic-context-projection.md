# Semantic Context Projection

This page defines the Phase 1 design contract for issue #415 and ADR
`2026-05-15-nornicdb-semantic-retrieval-evaluation`.

The goal is to add a bounded semantic retrieval overlay without changing
canonical Eshu truth. Postgres remains the durable owner of facts, queues,
content, projection decisions, checkpoints, retries, and retraction state.
NornicDB stores only Eshu-authored semantic context nodes and their search
indexes.

## Backend Contracts Used

The design is grounded in the pinned NornicDB `v1.1.0` source tag
`f7a64c7`:

- `db.index.vector.queryNodes` can search a named vector index by query text or
  direct vector.
- NornicDB hybrid search combines vector search, BM25, and reciprocal-rank
  fusion.
- Embedding generation is disabled by default and must be enabled explicitly or
  supplied by the client.
- Managed embeddings can use all node properties unless include/exclude
  settings are configured.
- BM25 indexes all node properties, so semantic context nodes must not carry
  metadata that is unsafe to search.

## Runtime Flow

Semantic context projection is a new reducer-owned read-model stage after
content and semantic-node projection:

```text
collector -> facts/content -> projector -> semantic nodes
  -> semantic context projection -> NornicDB semantic context nodes
  -> bounded semantic retrieval -> graph/content expansion -> answer packet
```

The stage must not parse repositories, claim canonical graph truth, or infer
new relationships. It reads already-admitted Eshu state and writes an additive
context surface for search.

## Context Record Types

Initial records are deliberately small. A record is eligible only when its
source has stable handles, a bounded text body, and a clear truth label.

| Kind | Source | Embedded text | Metadata only |
| --- | --- | --- | --- |
| `file_summary` | `content_files` | deterministic summary, language, role, bounded path hint | full path, commit, line count, content hash |
| `entity_summary` | `content_entities` | entity name, type, language, deterministic snippet summary | byte ranges, exact source cache, metadata JSON |
| `service_dossier` | resolved workload/service read models | summary, owning repo, admitted relationships | raw relationship rows, confidence ledger |
| `deployment_evidence` | reducer deployment/package/container decisions | evidence summary and outcome | raw payload, digests, provider IDs |
| `runtime_evidence` | CI, vulnerability, cloud, SBOM facts | bounded observation summary | provider run IDs, account IDs, raw reports |
| `documentation_snippet` | documentation findings and evidence packets | scrubbed section summary | source ACL, packet payload, exact source URL |

Raw full files are not embedded by default. If a later PR wants file chunks, it
must add a separate gate with measured size, redaction, and recall evidence.

## Record Shape

Every projected record must have this logical shape before it reaches NornicDB:

```text
context_id        stable uid, e.g. semantic_context:<kind>:<source_handle>
source_handle     file://, entity://, workload://, evidence://, or doc:// handle
source_kind       one of the record kinds above
repo_id           optional but required for repository-scoped records
scope_id          ingestion scope that produced the source state
generation_id     active generation used for replay and retraction
source_run_id     collector run when available
truth_level       exact, derived, semantic_candidate, stale_evidence, unsupported
freshness_state   current, stale, superseded, or unknown
text_version      semantic text schema version, starting at 1
text_hash         sha256 of normalized embedded text and title
content_hash      source content hash or source payload hash
model_provider    local, nornicdb-managed, openai, ollama, or none
model_name        embedding model identifier
model_dimensions  vector dimensions when known
```

Only `title`, `context_text`, `source_kind`, `repo_id`, `truth_level`, and
bounded display handles belong on NornicDB semantic context nodes. Everything
else stays in Postgres unless a query needs it for filtering and the field is
safe for BM25 search.

This restriction matters because NornicDB v1.1.0 hybrid search indexes all node
properties for BM25, and managed embeddings can also use all node properties
unless include/exclude settings are configured.

## Text Rules

The first implementation must build embedding text through an allowlist:

- `title`: at most 160 characters.
- `context_text`: at most 4 KiB after normalization.
- `source_kind`, `language`, and short repo display name may be included when
  they help ranking.
- Secrets, raw credentials, provider tokens, account IDs, private URLs, and
  raw vulnerability reports are excluded.
- High-cardinality exact identifiers stay metadata-only unless they are the
  handle being returned.

Normalization must trim whitespace, collapse repeated blank lines, preserve
code identifiers, and reject empty text. Empty or redacted-to-empty records are
marked `skipped_empty` in Postgres and are not written to NornicDB.

## Postgres Checkpoint Contract

Postgres must own a projection status table before runtime promotion. The
implementation can choose exact table names, but the durable key must include:

```text
context_id
source_handle
source_kind
repo_id
scope_id
generation_id
source_run_id
text_version
text_hash
model_provider
model_name
nornicdb_database
```

Required status values:

| Status | Meaning |
| --- | --- |
| `pending` | source state changed and needs projection |
| `indexed` | matching NornicDB node and search index state exists |
| `skipped_empty` | record had no safe searchable text |
| `retracting` | old context hash or source handle is being removed |
| `retracted` | old NornicDB node has been removed or hidden |
| `failed_retryable` | bounded retry may repair the record |
| `failed_terminal` | operator or code change is required |

Checkpoint rows must also store attempt count, failure class, failure message,
last attempted time, indexed time, retracted time, and projected byte count.
NornicDB state is never the source of truth for replay decisions.

## NornicDB Labels And Index Names

Semantic context nodes use labels that cannot be confused with canonical graph
labels:

```text
EshuSemanticContext
EshuSemanticFileContext
EshuSemanticEntityContext
EshuSemanticServiceContext
EshuSemanticEvidenceContext
EshuSemanticDocumentationContext
```

Required node properties:

```text
uid
title
context_text
source_handle
source_kind
repo_id
truth_level
freshness_state
text_hash
text_version
model_name
```

Recommended index names:

```text
eshu_semantic_context_embedding_v1
eshu_semantic_context_hybrid_v1
```

For NornicDB-managed embeddings, Eshu must configure embedding text to use only
`title` and `context_text` through `NORNICDB_EMBEDDING_PROPERTIES_INCLUDE`.
External embedding providers require an explicit Eshu opt-in and must fail
closed when not configured. Client-managed embeddings may be added later if
they give better checkpoint control.

## Replay And Retraction

Projection is idempotent by `context_id`, `text_hash`, `text_version`, and
model identity.

- Unchanged text and model identity keep the existing `indexed` row.
- Changed text writes a replacement context node and marks older hashes for
  retraction.
- Source tombstones or missing active source state retract matching context
  nodes.
- Failed NornicDB writes leave Postgres rows retryable until the configured
  attempt budget is exhausted.
- Retries must be safe under concurrent reducer workers and must not depend on
  lowering worker counts.

Retraction should remove or hide only `EshuSemanticContext` nodes for the
specific `context_id` and stale hash. It must not touch canonical graph labels.

## Query Contract For Later Phases

Phase 2 retrieval must use this projection through bounded calls:

- require `limit`, timeout, search mode, and truncation signal
- resolve repository, workload, service, or environment scope before search
- search only `EshuSemanticContext` labels
- return handles and truth labels, not raw NornicDB nodes
- expand graph/content details only after top-K candidates are selected
- keep semantic candidates separate from canonical facts in the answer envelope

## Edge Cases

The implementation must cover these before runtime promotion:

- invalid or unknown `source_kind`
- empty, redacted, or oversized text
- duplicate handles with different generations
- stale generation replay after a newer generation indexed
- partial NornicDB write after Postgres checkpoint update failure
- Postgres checkpoint update after NornicDB write failure
- concurrent workers projecting the same `context_id`
- model change requiring re-embedding
- source tombstone while a prior projection is retrying
- rollback after a failed batch

## Performance Impact Declaration

Affected stages are reducer semantic context selection, Postgres checkpoint
writes, optional embedding generation, NornicDB semantic-node writes, and
NornicDB search index maintenance.

Expected cardinality is bounded by selected context records, not all graph
nodes. The first implementation should start with `entity_summary`,
`file_summary`, and `documentation_snippet` records on the 15-case eval corpus,
then measure one small repo, one medium repo set, and only then a larger corpus.

Stop thresholds:

- false canonical claims above `0`
- p95 retrieval latency above `2s` without an accepted reason
- semantic projection creates retry or dead-letter backlog
- projection overhead exceeds `10%` of comparable indexing wall time without a
  bounded follow-up design
- NornicDB memory or disk growth lacks a corpus-size explanation

## Observability Requirements

The runtime PR that implements this design must add or explicitly reuse signals
that answer: is it stuck, slow, failing, too large, or finished?

Required signals:

- semantic context projection queue depth and oldest age
- records selected, skipped, indexed, retracted, and failed by bounded kind
- projection duration split into select, text build, checkpoint, embed, graph
  write, and mark-complete phases
- projected text bytes and vector dimensions
- embedding/index operation duration and error counts
- retrieval request duration by search mode
- result count, truncation, and candidate truth-level summary
- structured logs with scope, source kind, bounded rank count, status, and
  failure class

High-cardinality handles, paths, raw query text, provider IDs, and account IDs
belong in bounded logs or spans only when needed for diagnosis. They must not
be metric labels.

## Validation Gates

Before implementation work can be accepted:

- unit tests prove text shaping, redaction, hashing, checkpoint status
  transitions, idempotent replay, and retraction selection
- a focused fixture proves only `EshuSemanticContext` labels are written
- semantic eval records current-path versus semantic-path metrics
- performance evidence records input cardinality, fact count, context record
  count, queue state, latency, backend version, and image digest
- observability evidence names the exact metrics, spans, logs, and status
  fields used to debug the path
