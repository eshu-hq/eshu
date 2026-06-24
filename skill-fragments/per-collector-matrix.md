---
id: per-collector-matrix
version: 1.0.0
byte_citation: go/internal/capabilitycatalog/catalog.go#1-80
description: |
  The active collector set is whatever is enabled in the active
  runtime profile. Per-collector MCP tools are enumerated from the
  live capability catalog, not from a static prose list. The
  fragment renders differently per deployment.
---

# Eshu Per-Collector Matrix

The active collector set is whatever is enabled in the active runtime
profile. Per-collector MCP tools are enumerated from the live
capability catalog (`go/internal/capabilitycatalog/catalog.go`), not
from a static prose list.

This means the same fragment renders differently per deployment:

- A code-only deployment lists code-only collectors.
- A full-stack deployment with Terraform plus AWS plus Kubernetes
  lists the cloud and platform collectors too.
- A specialized security deployment may list only the security
  collectors.

The active collector set on the deployment where this skill was
rendered is enumerated by the generator in the
`Active Collectors on This Deployment` section that follows the
rendered fragment body.
