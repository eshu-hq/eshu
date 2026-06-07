# Search Retrieval Contract

Search retrieval is an internal evaluation path for issue #417. It is not a
public HTTP API route, MCP tool, or default runtime search feature.

The contract lives in `go/internal/searchretrieval`. It validates bounded
semantic-evaluation requests and normalizes ranked `EshuSearchDocument`
candidates before later adapters call Postgres, NornicDB, or any other backend.

## Request Contract

Every request must include:

| Field | Meaning |
| --- | --- |
| `query` | User or eval-suite query text. |
| `scope` | At least one service, workload, repository, or environment anchor. |
| `mode` | `keyword`, `semantic`, or `hybrid`. |
| `limit` | Explicit top-K limit. |
| `timeout_ns` | Explicit timeout in nanoseconds. |

Scope selection prefers the smallest available anchor:

1. service;
2. workload;
3. repository;
4. environment.

Requests without a scope, limit, timeout, query, or valid mode are rejected
before any backend can run.

## Candidate Contract

Backends must return curated `EshuSearchDocument` records. They must not return
raw graph nodes, arbitrary graph properties, raw provider payloads, log lines,
trace spans, dashboard JSON, query bodies, or other excluded projection data.

Each candidate carries:

- the search document;
- finite backend score;
- optional failure classes;
- optional low-cardinality metadata.

Candidate search documents must have a stable `document.id` and at least one
non-empty graph handle. Candidates with missing document identity, missing graph
handles, `NaN` scores, or infinite scores are rejected before ranking because
they cannot produce stable top-K ordering or bounded graph expansion.

## Runner Contract

`Runner.Retrieve` executes one bounded request through a narrow backend port. It
does not know whether the backend is Postgres content search, NornicDB BM25,
NornicDB vector search, or a fixture adapter.

The runner:

- validates the request before backend use;
- creates a timeout-bound context from `timeout_ns`;
- calls `Backend.Search` for curated candidates only;
- normalizes candidates with `BuildResponse`;
- records one `Observation` when an observer is configured.

Observations carry:

- query id;
- scope anchor;
- mode;
- limit;
- duration;
- candidate count;
- result count;
- truncation state;
- timeout state;
- failure classes;
- candidate truth-level counts;
- error class.

The observation shape is an internal summary, not an OTEL exporter. Later live
adapters must bridge it to metrics, spans, or structured logs without using
high-cardinality anchor ids as metric labels.

## NornicDB Hybrid Prototype

`go/internal/searchnornicdb` implements the issue #417 prototype adapter for the
pinned NornicDB gRPC `SearchText` API. It is internal-only and admits explicit
`SemanticContext` labels.

The adapter:

- requires `hybrid` mode;
- sends the `SemanticContext` label filter to NornicDB;
- overfetches by one result within the internal maximum so truncation can be
  observed;
- rejects NornicDB responses that report non-hybrid `search_method` values;
- rejects `fallback_triggered=true`;
- rejects hits that do not carry the `SemanticContext` label;
- rejects hits outside the request's smallest scope anchor;
- requires per-item `derived` / `read_model` truth labels and `fresh`
  freshness;
- returns only candidate graph handles for later bounded expansion.

The pinned NornicDB gRPC request shape exposes labels but not property filters.
This branch therefore post-checks the returned candidate scope and does not
claim full #417 measured acceptance until live evidence proves pre-search
scoping, or a documented design exception is accepted. There is still no
whole-graph fallback and no public API/MCP exposure.

## Postgres Keyword Baseline Adapter

`go/internal/searchpostgres` implements the Postgres content-search baseline for
internal benchmark runs. It wraps the existing Postgres content-store search
methods, projects rows through `searchdocs`, and returns
`postgres_content_search` candidates for `keyword` mode only.

The adapter:

- requires repository scope before touching the content store;
- rejects service, workload, and environment-only scopes because the current
  content store cannot pre-filter those anchors safely;
- overfetches by one row per content lane so truncation is observed after
  normalization;
- skips rows that `searchdocs` excludes for missing stable handles, excluded
  source kind, or sensitive context;
- emits derived content-search documents, not canonical graph truth.

This adapter is benchmark plumbing. It does not add a public route, MCP tool,
runtime flag, graph write, or NornicDB search enablement.

## Response Contract

`BuildResponse` sorts candidates by score descending and document id ascending,
then returns deterministic top-K results with:

- rank;
- score;
- document;
- truth scope;
- freshness;
- graph handles for bounded graph expansion;
- truncation state;
- false canonical claim count.

The false canonical claim count increments when any result claims a truth level
other than `derived`. Search score, semantic similarity, and link prediction do
not become canonical graph truth.

## Benchmark Link

`Response.SearchbenchResults` converts normalized results into
`go/internal/searchbench` scoring input. This lets issue #417 use the same
recall, precision, nDCG, and false-canonical-claim metrics defined for issue
#1264.

## What This Does Not Do

This contract does not:

- read or write graph state;
- expose HTTP or MCP routes;
- add OpenAPI or MCP tool contracts;
- enable default runtime search.

The internal Postgres and NornicDB adapters can call their backends when
explicitly constructed by a benchmark or proof harness. The contract still does
not enable default runtime search or public retrieval.

Those steps require later PRs with telemetry, capability envelopes, backend
proof, and semantic-evaluation evidence.

## Verification Gate

Focused package gate:

```bash
cd go && go test ./internal/searchretrieval ./internal/searchdocs ./internal/searchbench ./internal/searchnornicdb ./internal/searchpostgres -count=1
```

Docs changes must also pass:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

## Related Docs

- [Search Document Projection](search-document-projection.md)
- [Search Benchmark Evidence](search-benchmark-evidence.md)
- [Truth Label Protocol](truth-label-protocol.md)
