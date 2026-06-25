# collector/cassette

Credential-free collector replay from a pre-recorded JSON cassette file.

## Purpose

Collector cassettes let the e2e test suite run every credentialed collector
without live cloud credentials. A cassette captures the collector output (scope
identity, generation metadata, and fact envelopes) of one production-equivalent
collection run. The `Source` type replays that output as if the collector ran
live, satisfying `collector.Source` without network calls or SDK dependencies.

## Cassette file format

```json
{
  "collector": "kubernetes_live",
  "schema_version": "1",
  "scopes": [
    {
      "scope_id": "kubernetes_live:cluster:supply-chain-demo",
      "source_system": "kubernetes_live",
      "scope_kind": "cluster",
      "collector_kind": "kubernetes_live",
      "partition_key": "kubernetes_live:cluster:supply-chain-demo",
      "metadata": { "cluster_name": "supply-chain-demo" },
      "generation_id": "cassette-k8s-scd-gen1",
      "observed_at": "2026-06-25T00:00:00Z",
      "trigger_kind": "snapshot",
      "facts": [
        {
          "fact_kind": "kubernetes_live.pod_template",
          "stable_fact_key": "kubernetes_live:cluster:supply-chain-demo:deployment:default:supply-chain-demo",
          "schema_version": "1.0.0",
          "collector_kind": "kubernetes_live",
          "fencing_token": 1,
          "source_confidence": "observed",
          "payload": {
            "cluster_id": "supply-chain-demo",
            "namespace": "default",
            "name": "supply-chain-demo",
            "kind": "Deployment"
          }
        }
      ]
    }
  ]
}
```

Required fields per scope: `scope_id`, `source_system`, `scope_kind`,
`collector_kind`, `generation_id`, `observed_at`.

Required fields per fact: `fact_kind`, `stable_fact_key`, `schema_version`,
`payload` (use `{}` for an empty payload).

## Canonical cassette locations

Cassettes shipped with the repository live at:

```
testdata/cassettes/<collector>/<recording>.json
```

For example:
- `testdata/cassettes/kuberneteslive/supply-chain-demo.json`
- `testdata/cassettes/awscloud/supply-chain-demo.json`

The `supply-chain-demo` recording is seeded from
`examples/supply-chain-demo/` and captures the synthetic supply-chain-demo
infrastructure: a Node.js app deployed to Kubernetes, backed by AWS/GCP/Azure
resources, with Vault secrets and OCI images.

## Wiring in a collector binary

All credentialed collector binaries expose `-mode=cassette -cassette-file=<path>`.
Example:

```bash
collector-kubernetes-live \
  -mode=cassette \
  -cassette-file=testdata/cassettes/kuberneteslive/supply-chain-demo.json
```

The `buildCassetteService` function in each binary's `service.go` wires
`cassette.NewSource(cassettePath)` as the source.

## No-Regression Evidence:

No-Regression Evidence: `Source` performs no network calls, acquires no
credentials, and holds no shared mutable state beyond its scope index (which
advances single-threaded per `collector.Service`). Verified by
`go test ./internal/collector/cassette/... -count=1`.

The `// #nosec G304` annotation on `LoadFile`'s `os.ReadFile` is comment-only: it
documents that the cassette path is operator-supplied (the `-cassette-file` flag
or repo-shipped testdata), not user- or request-derived input. No code path,
allocation, or query changed, so there is no performance impact to measure.

## No-Observability-Change:

No-Observability-Change: no new metrics, spans, or log lines are emitted by the
cassette package itself, including the `#nosec G304` documentation change on
`LoadFile`. Collector-level telemetry (collection cycle duration, fact count)
records normally because `Source` is wired through the standard
`collector.Service` poll loop.
