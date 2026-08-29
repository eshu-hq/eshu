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

The chart uses external Neo4j for its render-safe defaults. NornicDB remains the
canonical production graph lane when an operator supplies a verified immutable
build and existing Bolt endpoint:

```yaml
env:
  ESHU_GRAPH_BACKEND: nornicdb
  DEFAULT_DATABASE: nornic
  NEO4J_DATABASE: nornic
nornicdb:
  capabilities:
    relationshipMergePropertyIdentity: true
neo4j:
  uri: bolt://nornicdb.platform.svc.cluster.local:7687
```

The capability acknowledgement is required for external NornicDB endpoints as
well as the bundled deployment. Set it only after verifying the immutable
backend build preserves relationship MERGE identity properties.

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
  image:
    repository: registry.example.com/platform/nornicdb
    tag: "relationship-identity-verified@sha256:<immutable-digest>"
  capabilities:
    relationshipMergePropertyIdentity: true
  bindAddress: 0.0.0.0

neo4j:
  uri: bolt://eshu-nornicdb:7687
  auth:
    secretName: ""
    password: "NornicDbSecret1"

schemaBootstrap:
  useHelmHooks: false
```

The chart's default bundled image (`timothyswt/nornicdb-cpu-bge:v1.2.3`, pinned
by digest) is still rejected when enabled, because nobody has measured whether
it preserves the relationship identity properties the provenance writers need.
The version it replaced, `v1.1.11`, was measured and did not. Replace the
example repository, tag, and digest with an immutable build containing
orneryd/NornicDB#290 (or a later verified equivalent), or measure the default,
before setting the capability acknowledgement to `true`.

Replace `password` with your own strong password (min 12 chars, mixed case +
digit) or set `neo4j.auth.secretName` to an existing Kubernetes Secret instead;
the chart requires one or the other and fails the render otherwise.

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

The `workspace-setup` init container runs as the Eshu UID/GID, drops all Linux
capabilities, and relies on the pod `fsGroup`/`fsGroupChangePolicy` contract
for supported persistent volumes. It prepares `/data/.eshu`, `/data/repos`, and
an idempotently replaced `/data/repos/.eshuignore` before the ingester starts.
