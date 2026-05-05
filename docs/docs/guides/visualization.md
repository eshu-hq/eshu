# Visualizing the Graph

The current Go platform exposes visualization through the query API and MCP
tooling rather than through a dedicated `eshu visualize` CLI command.

## Neo4j Browser via HTTP API

Use the read-only visualization endpoint when you want a Neo4j Browser URL for
an existing Cypher query:

```bash
curl -s \
  -X POST http://localhost:8080/api/v0/code/visualize \
  -H 'Content-Type: application/json' \
  -d '{"cypher_query":"MATCH (n)-[r]->(m) RETURN n, r, m LIMIT 25"}'
```

The response contains a `url` field such as
`http://localhost:7474/browser/?cmd=edit&arg=MATCH...` that opens Neo4j
Browser with the query pre-filled.

The endpoint accepts read-only Cypher only and rejects mutation keywords before
the query reaches Neo4j.

## Neo4j Browser via MCP

If you use Eshu through MCP, call `visualize_graph_query` with the same
read-only Cypher text. The tool returns the same Neo4j Browser URL that the
HTTP API exposes.

## Neo4j Bloom

If you use Neo4j Desktop, Bloom provides a richer exploration experience:

- Spatial zoom and pan across the graph
- Natural-language-style search (e.g., "Show me callers of X")
- Visual filtering by node type

Bloom is part of Neo4j Desktop and requires no additional Eshu configuration.
