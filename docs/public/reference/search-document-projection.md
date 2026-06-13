# Search Document Projection

Eshu search documents are curated retrieval records. They are not canonical
graph truth, and they are not a dump of every NornicDB node and property.

Use this page when defining, reviewing, or benchmarking BM25, vector, or hybrid
retrieval over Eshu evidence.

## Contract

`EshuSearchDocument` records carry:

| Field | Meaning |
| --- | --- |
| `id` | Stable document id, prefixed by the source lane. |
| `repo_id` | Smallest repository scope when available. |
| `source_kind` | Source lane such as `code_entity`, `repository_file`, `runtime_summary`, or `semantic_context`. |
| `title` | Short display title for ranking and result rendering. |
| `path` | Repo-relative path when the source is file-backed. |
| `context_text` | Bounded searchable text. |
| `entity_refs` | Content entity refs used for source drill-down. |
| `graph_handles` | Stable bounded handles for later graph expansion. |
| `labels` | Low-cardinality source labels such as language or entity type. |
| `updated_at` | Source projection timestamp when available. |
| `truth_scope` | Document authority and basis. |
| `freshness` | Freshness state for the projected source. |
| `access_scope` | Authorization scope, currently repository-first. |
| `provenance` | Source table, source ids, and projection source. |

`truth_scope.level` is `derived` for this first slice. Retrieval score,
semantic similarity, and future link prediction must not upgrade a document to
canonical graph truth.

## Source Matrix

The first projection supports these source lanes:

| Source kind | Input rows | Search value | Required stable handles |
| --- | --- | --- | --- |
| `code_entity` | `content_entities` | Symbol or IaC entity name plus bounded source cache. | repository, content entity, file |
| `repository_file` | `content_files` | Repo-relative path plus bounded file text. | repository, file |
| `runtime_summary` | reducer/query read models | Service, workload, and image summary text. | at least one service, workload, image, or source id handle |
| `semantic_context` | explicit semantic context read models | Curated semantic labels for service, workload, repository, or environment context. | semantic context plus at least one repository, service, workload, environment, or explicit expansion handle |

Future lanes may add vulnerability, incident, work-item, observability, and
package evidence only after they define source-specific privacy rules and
positive, negative, and ambiguous fixtures.

`semantic_context` is intentionally narrower than whole-graph search. It is the
only source kind currently admitted by the issue #417 NornicDB hybrid adapter,
and its records remain derived/read-model evidence. Semantic retrieval must not
promote these records to canonical graph truth.

## Excluded Data

Projection excludes these by default:

- raw provider payloads;
- raw log lines;
- trace spans;
- dashboard JSON and saved-object payloads;
- query bodies;
- security finding bodies;
- credentials, tokens, secrets, private keys, and password-shaped snippets;
- high-cardinality labels, request ids, user ids, and queue/projection ids;
- internal graph ids that are not stable user-facing handles.

If a source needs one of these values for debugging, keep it in provider facts,
logs, traces, or redacted evidence packets. Do not copy it into
`context_text`.

## Projection Rules

Every included document must:

- resolve the smallest scope first;
- carry at least one stable `graph_handle`;
- bound `context_text`;
- preserve low-cardinality labels only;
- keep source provenance visible;
- mark truth as derived;
- support deterministic ordering in benchmarks and tests.

Every excluded candidate should record a reason such as
`missing_stable_handle`, `sensitive_context`, or `excluded_source_kind` so
operators can size projection gaps without reading raw payloads.

## Reducer Read-Model

The reducer projects curated documents into the shared Postgres fact store as a
generation-scoped read model, separate from canonical graph writes:

- `reducer.ProjectSearchDocuments` curates a bounded per-generation source set
  (content entities, content files, runtime summaries) into the included
  documents plus a low-cardinality curation summary.
- `reducer.EshuSearchDocumentHandler` runs that curation for one intent and
  writes the authoritative document set for the scope and generation.
- `reducer.PostgresEshuSearchDocumentWriter` upserts each document as a derived
  fact (`fact_kind = reducer_eshu_search_document`, `truth_scope.level = derived`)
  keyed by a deterministic `fact_id` over scope, generation, and document id, so
  retries of the same generation converge.
- `postgres.EshuSearchDocumentStore.ListActiveDocuments` reads documents back,
  joining `ingestion_scopes.active_generation_id` so only the active generation
  is returned.

Retirement has two layers. Across generations, documents are retired
automatically because readers join on the active generation. Within a
generation, the writer deletes any prior document for that generation that is
not in the freshly written set, so a source row that becomes excluded or
disappears retires its document on the next projection.

Search documents are derived retrieval evidence only. The write path performs no
graph write, and retrieval score never becomes canonical graph truth.

Runtime wiring of this domain (intent emission during finalization and runner
registration) lands after the design-430 benchmark gate selects the search-lane
backing.

## Fixture Gate

The focused gate is:

```bash
cd go && go test ./internal/searchdocs -count=1
cd go && go test ./internal/reducer -run EshuSearchDocument -count=1
cd go && go test ./internal/storage/postgres -run EshuSearchDocument -count=1
```

The tests prove:

- positive code entity, repository file, runtime summary, and semantic context
  projection;
- stable document ids and graph handles for bounded expansion;
- sensitive context, dashboard payloads, log-like metadata, and missing handles
  are rejected;
- labels are deterministic and low-cardinality;
- the reducer curation core drops sensitive and excluded candidates and orders
  documents deterministically;
- the fact writer upserts idempotently and retires stale documents in the
  generation;
- the reader returns only the active generation.

## Telemetry Requirements

The reducer projection cycle emits the canonical-write counter and duration
histogram tagged by domain, plus a structured log with `considered`, `included`,
`skipped`, `written`, `retired`, per-reason skip counts, and per-source-kind
included counts. Future persistence or benchmark callers must additionally
expose:

- document count by `source_kind`;
- skipped-document count by exclusion reason;
- projection duration;
- bounded context truncation count;
- redaction/drop reason count;
- failure class.

Use spans or structured logs for high-cardinality ids. Do not put repository
paths, entity ids, image digests, request ids, or provider-native ids in metric
labels.

## Related Docs

- [Search Retrieval Contract](search-retrieval-contract.md)
- [Search Benchmark Evidence](search-benchmark-evidence.md)
- [NornicDB Tuning](nornicdb-tuning.md)
- [Truth Label Protocol](truth-label-protocol.md)
- [Telemetry Overview](telemetry/index.md)
