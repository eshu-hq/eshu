# Deploy

This directory contains deployment assets, not application source code.

- `helm/` contains the supported Kubernetes chart.
- `manifests/` contains small raw Kubernetes examples.
- `argocd/` contains GitOps examples.
- `observability/` and `grafana/` contain local and operator-facing telemetry
  assets.

Runtime behavior still comes from the Go binaries and Docker image. Keep this
directory focused on how those binaries are deployed.
