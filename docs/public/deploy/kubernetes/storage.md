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

The chart's default bundled image (`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-490-a427a468`, pinned
by digest, self-built from upstream main at the conjunct index-seek fix) is the verified default. The capability acknowledgement still stays
explicit because it also covers operator-selected external endpoints the chart
cannot identify. Confirm the selected endpoint uses the verified digest, or
independently prove an override, before setting it to `true`.

The bundled backend defaults to `kubernetes.io/arch: amd64`, the platform with
live v1.3.3 proof. Global `nodeSelector` values are merged with
`nornicdb.nodeSelector`, and the component value wins on duplicate keys. Do not
select arm64 for production evidence until that image child passes the same
live backend contract.

Replace `password` with your own strong password (min 12 chars, mixed case +
digit) or set `neo4j.auth.secretName` to an existing Kubernetes Secret instead;
the chart requires one or the other and fails the render otherwise.

Managed installs use the versioned
`<release>-nornicdb-v132-data` PVC and annotate it for Helm retention. A live
upgrade that finds the legacy `<release>-nornicdb-data` PVC fails closed until
the operator stops graph writers, snapshots or backs up that PVC, and sets
`nornicdb.persistence.allowFreshVolumeMigration=true`. The chart then keeps the
legacy PVC while mounting fresh v1.3.3 storage. Use
`nornicdb.persistence.existingClaim` only for an operator-provisioned,
v1.3.3-compatible PVC; the chart rejects the legacy claim name.

After the fresh volume is mounted, follow
[Rebuild the graph from facts](../../operate/graph-rebuild-from-facts.md) from
the preserved Postgres store. Cut over only after queues are terminal and
required API/MCP graph truth passes. Roll back with the preserved old PVC or a
fresh rebuild; never attach an older binary to a PVC modified by v1.3.3 without
proof for that exact reverse transition.

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
