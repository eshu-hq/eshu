# Secrets/IAM Graph Promotion Proof Snapshot - 2026-06-07

Issue: #1381. Gate: ADR #1314 sections 11, 12, and 14.

This note records the repo-local proof state for the default-off
Secrets/IAM graph projection and the recorded ADR section 14 approval. It does
not approve production activation and it does not enable
`ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED`.

## Result

Satisfied proof and governance gates:

- Section 11 fixture truth: reducer read-model rows drive graph node and edge
  rows through `SecretsIAMGraphProjectionHandler`.
- Section 12 writer benchmark: `BenchmarkSecretsIAMGraphWriter` proves the
  UNWIND-batched, uid-anchored write shape without per-edge reads.
- Backend conformance: NornicDB and Neo4j both passed the live writer
  conformance test and the shared backend conformance script.
- Schema readback: Neo4j proof stack confirmed the four `SecretsIAM*` uid
  constraints plus scope indexes after bootstrap.
- Section 14 principal/security approval: recorded as approved on 2026-06-07.

Still blocked:

- `risk:schema` approval for target activation.
- Explicit target deployment decision and flag-on live activation proof.

## Evidence

NornicDB live writer proof against the local remote-e2e NornicDB stack:

```bash
cd go
ESHU_SECRETS_IAM_GRAPH_LIVE=1 \
ESHU_GRAPH_BACKEND=nornicdb \
ESHU_NEO4J_URI=bolt://localhost:7687 \
ESHU_NEO4J_USERNAME=neo4j \
ESHU_NEO4J_PASSWORD=change-me \
ESHU_NEO4J_DATABASE=nornic \
go test ./internal/storage/cypher -run '^TestSecretsIAMGraphWriterLiveConformance$' -count=1 -v
```

Result: `PASS`, `ok github.com/eshu-hq/eshu/go/internal/storage/cypher 14.122s`.

NornicDB shared backend conformance:

```bash
ESHU_GRAPH_BACKEND=nornicdb \
ESHU_NEO4J_URI=bolt://localhost:7687 \
ESHU_NEO4J_USERNAME=neo4j \
ESHU_NEO4J_PASSWORD=change-me \
ESHU_NEO4J_DATABASE=nornic \
./scripts/verify_backend_conformance_live.sh
```

Result: `ok github.com/eshu-hq/eshu/go/internal/backendconformance 28.907s`.

Neo4j live writer proof against isolated proof stack:

```bash
cd go
ESHU_SECRETS_IAM_GRAPH_LIVE=1 \
ESHU_GRAPH_BACKEND=neo4j \
ESHU_NEO4J_URI=bolt://localhost:17687 \
ESHU_NEO4J_USERNAME=neo4j \
ESHU_NEO4J_PASSWORD=change-me \
ESHU_NEO4J_DATABASE=neo4j \
go test ./internal/storage/cypher -run '^TestSecretsIAMGraphWriterLiveConformance$' -count=1 -v
```

Result: `PASS`, `ok github.com/eshu-hq/eshu/go/internal/storage/cypher 3.265s`.

Neo4j shared backend conformance:

```bash
ESHU_GRAPH_BACKEND=neo4j \
ESHU_NEO4J_URI=bolt://localhost:17687 \
ESHU_NEO4J_USERNAME=neo4j \
ESHU_NEO4J_PASSWORD=change-me \
ESHU_NEO4J_DATABASE=neo4j \
./scripts/verify_backend_conformance_live.sh
```

Result: `ok github.com/eshu-hq/eshu/go/internal/backendconformance 3.082s`.

Focused repo-local gate:

```bash
cd go
go test ./internal/reducer ./internal/storage/cypher ./cmd/reducer ./internal/graph \
  -run 'SecretsIAMGraph|SecretsIAM|TestSchemaStatementsContainsUIDConstraints|TestSchemaStatementsContainsExpectedConstraints' \
  -count=1
```

Result: passed all four packages.

Section 12 benchmark rerun:

```bash
cd go
go test ./internal/storage/cypher -run '^$' -bench BenchmarkSecretsIAMGraph -benchmem -benchtime=50x -count=3
```

Result: `23983 ns/op`, `47082 ns/op`, `160462 ns/op`,
about `53728-53765 B/op`, `765 allocs/op`.
