# Storage

Use this page to choose the Kubernetes storage shape. Value names and render
details live in [Routing and Storage Values](helm-routing-and-storage-values.md).

## Postgres

Postgres is required. It stores facts, durable queues, status, content, and
recovery data. Helm writes `contentStore.dsn` to both `ESHU_CONTENT_STORE_DSN`
and `ESHU_POSTGRES_DSN`.

```yaml
contentStore:
  dsn: postgresql://eshu:secret@postgres.platform.svc.cluster.local:5432/eshu
```

The database must support `pg_trgm`; Eshu creates trigram indexes for file and
entity content search.

## Graph Backend

NornicDB is the default graph backend. The normal production shape is an
existing Bolt endpoint:

```yaml
env:
  ESHU_GRAPH_BACKEND: nornicdb
  DEFAULT_DATABASE: nornic
  NEO4J_DATABASE: nornic
neo4j:
  uri: bolt://nornicdb.platform.svc.cluster.local:7687
```

Neo4j is the explicit compatibility backend:

```yaml
env:
  ESHU_GRAPH_BACKEND: neo4j
  DEFAULT_DATABASE: neo4j
  NEO4J_DATABASE: neo4j
neo4j:
  uri: bolt://neo4j.platform.svc.cluster.local:7687
```

The value key remains `neo4j.uri` for both backends because the runtime uses the
Neo4j Bolt driver shape.

## Bundled NornicDB

The chart can render one bundled NornicDB deployment for test or small
single-cluster installs:

```yaml
nornicdb:
  enabled: true

neo4j:
  uri: bolt://eshu-nornicdb:7687
  auth:
    secretName: ""

schemaBootstrap:
  useHelmHooks: false
```

Do not use Helm hooks for schema bootstrap in this shape. Hooks run before the
bundled NornicDB Service exists.

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
Keep the default StatefulSet `volumeClaimTemplates` shape when
`ingester.replicas` is greater than one so each sharded ingester owns a distinct
workspace claim. The chart rejects `ingester.persistence.existingClaim` for
horizontal ingesters because a shared PVC would let multiple shards mutate the
same checkout tree.
