# Visualizing the Graph

The Go platform exposes graph visualization through the query API and MCP
tooling. The visualization endpoint executes a read-only Cypher query and returns
a bounded, renderable subgraph (nodes and edges) projected from the graph
entities in the result, so any graph-aware renderer can draw it without a running
Neo4j Browser.

## Graph visualization via HTTP API

Use the read-only visualization endpoint to execute a query and get back a
renderable node/edge packet:

```bash
curl -s \
  -X POST http://localhost:8080/api/v0/code/visualize \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/vnd.eshu.v1+json' \
  -d '{"cypher_query":"MATCH (n)-[r]->(m) RETURN n, r, m LIMIT 25"}'
```

RETURN whole graph entities — nodes, relationships, and paths (for example
`RETURN n, r, m` or `RETURN p` where `p` is a path) — rather than scalar
properties. Only graph entities are renderable as a subgraph; a query that
returns bare properties (counts, strings, `n.name`) yields an explicit
`supported: false` packet that names why and recommends `execute_cypher_query`
instead.

The response `data` carries a `visualization_packet` plus the row `limit` and a
`truncated` flag:

```json
{
  "visualization_packet": {
    "view": "graph_query",
    "supported": true,
    "nodes": [
      {"id": "viznode:...", "type": "Service", "label": "catalog", "category": "Service"}
    ],
    "edges": [
      {"id": "vizedge:...", "source": "viznode:...", "target": "viznode:...", "relationship": "DEPLOYED_FROM"}
    ],
    "limits": {"max_nodes": 60, "max_edges": 120, "ordering": "stable_id", "node_count": 2, "edge_count": 1},
    "truncation": {"truncated": false, "dropped_node_count": 0, "dropped_edge_count": 0}
  },
  "limit": 100,
  "truncated": false
}
```

Node IDs are derived from the graph element id, so the same node always yields
the same packet node ID regardless of row order. Edges are retained only when
both endpoints appear as nodes in the same result; dangling edges are dropped
into the `truncation` block. The packet is bounded to `max_nodes` nodes and
`max_edges` edges, and the underlying query is bounded with an injected `LIMIT`
(default 100, max 1000, override with the `limit` field) and executed under a
timeout against a read-only session.

The endpoint accepts read-only Cypher only and rejects mutation keywords before
the query reaches the graph backend. Because it executes a real graph read it
requires a graph-backed profile (`local_authoritative` or higher); the
content-only `local_lightweight` profile returns an unsupported-capability error.

## Graph visualization via MCP

If you use Eshu through MCP, call `visualize_graph_query` with the same
read-only Cypher text (and an optional `limit`). The tool returns the same
`visualization_packet` that the HTTP API exposes.

## Neo4j Bloom

If you use Neo4j Desktop, Bloom provides a richer exploration experience:

- Spatial zoom and pan across the graph
- Natural-language-style search (e.g., "Show me callers of X")
- Visual filtering by node type

Bloom is part of Neo4j Desktop and requires no additional Eshu configuration.
