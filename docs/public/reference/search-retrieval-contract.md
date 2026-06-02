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

Candidates with `NaN` or infinite scores are rejected before ranking because
they cannot produce stable top-K ordering.

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

- call NornicDB;
- call Postgres;
- read or write graph state;
- expose HTTP or MCP routes;
- add OpenAPI or MCP tool contracts;
- enable default runtime search.

Those steps require later PRs with telemetry, capability envelopes, backend
proof, and semantic-evaluation evidence.

## Verification Gate

Focused package gate:

```bash
cd go && go test ./internal/searchretrieval ./internal/searchdocs ./internal/searchbench -count=1
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
