# Minimal manifests

Use `deploy/manifests/minimal` when you need a small raw Kubernetes example.
Use Helm for the supported split-service deployment.

The minimal kustomize example creates:

- a namespace
- a config map
- an example graph secret reference
- one API-serving `StatefulSet`
- one `ClusterIP` service

Apply it with:

```bash
kubectl apply -k deploy/manifests/minimal
```

This is not the production shape. It does not include the MCP runtime,
ingester, workflow coordinator, resolution engine, schema bootstrap Job, or
`ServiceMonitor` resources.
