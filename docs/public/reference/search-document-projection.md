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
| `source_kind` | Source lane such as `code_entity`, `repository_file`, or `runtime_summary`. |
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

Future lanes may add vulnerability, incident, work-item, observability, and
package evidence only after they define source-specific privacy rules and
positive, negative, and ambiguous fixtures.

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

## Fixture Gate

The focused gate is:

```bash
cd go && go test ./internal/searchdocs -count=1
```

The tests prove:

- positive code entity, repository file, and runtime summary projection;
- stable document ids and graph handles for bounded expansion;
- sensitive context, dashboard payloads, log-like metadata, and missing handles
  are rejected;
- labels are deterministic and low-cardinality.

## Telemetry Requirements

Future persistence or benchmark callers must expose:

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

- [Search Benchmark Evidence](search-benchmark-evidence.md)
- [NornicDB Tuning](nornicdb-tuning.md)
- [Truth Label Protocol](truth-label-protocol.md)
- [Telemetry Overview](telemetry/index.md)
