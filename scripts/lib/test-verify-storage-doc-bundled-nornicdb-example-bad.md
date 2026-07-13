# Storage

## Bundled NornicDB

The chart can render one bundled NornicDB deployment for test or small
single-cluster installs:

```yaml
nornicdb:
  enabled: true
  bindAddress: 0.0.0.0

neo4j:
  uri: bolt://eshu-nornicdb:7687
  auth:
    secretName: ""

schemaBootstrap:
  useHelmHooks: false
```

Do not use Helm hooks for schema bootstrap in this shape. Hooks run before the
bundled NornicDB Service exists.
