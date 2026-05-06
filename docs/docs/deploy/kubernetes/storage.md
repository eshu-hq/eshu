# Storage

Eshu uses two external data stores and one Kubernetes workspace volume.

## Postgres

Postgres is required. It stores facts, durable queues, status, content, and
recovery data. The chart writes the same DSN to `ESHU_CONTENT_STORE_DSN` and
`ESHU_POSTGRES_DSN` from `contentStore.dsn`.

```yaml
contentStore:
  dsn: postgresql://eshu:secret@postgres.platform.svc.cluster.local:5432/eshu
```

The Postgres instance must support the `pg_trgm` extension because Eshu creates
trigram indexes for file and entity content search.

## Graph backend

NornicDB is the default graph backend:

```yaml
env:
  ESHU_GRAPH_BACKEND: nornicdb
  DEFAULT_DATABASE: nornic
  NEO4J_DATABASE: nornic
neo4j:
  uri: bolt://nornicdb.platform.svc.cluster.local:7687
```

That default reflects the current Eshu runtime direction. The active ADRs still
track the Neo4j support decision separately: either Neo4j proves it can run the
shared Eshu Cypher contract fast enough, or it remains compatibility-only.

Neo4j is the explicit official alternative backend:

```yaml
env:
  ESHU_GRAPH_BACKEND: neo4j
  DEFAULT_DATABASE: neo4j
  NEO4J_DATABASE: neo4j
neo4j:
  uri: bolt://neo4j.platform.svc.cluster.local:7687
```

The value key is `neo4j.uri` for both backends because the runtime uses the
Neo4j Bolt driver shape. Unsupported graph backends are not official.

## Workspace PVC

The ingester is the only long-running Kubernetes workload that should mount the
repository workspace.

```yaml
ingester:
  persistence:
    enabled: true
    size: 100Gi
    storageClass: ""
```

Set `ingester.persistence.existingClaim` when your platform owns the PVC. Set
`ingester.persistence.enabled=false` only for short-lived experiments.
