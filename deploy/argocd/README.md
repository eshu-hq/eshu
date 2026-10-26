# Argo CD Examples

This directory contains public-safe Argo CD examples for Eshu.

- `base/` is cloud-neutral
- `overlays/aws/` shows one EKS-oriented pattern with External Secrets
- `overlays/qa/` pilots the schema-bootstrap ArgoCD PreSync hook (#9201):
  the QA sync runs pending migrations before workloads roll, while the
  chart default stays off so direct `helm install/upgrade` behavior is
  unchanged and production keeps the Helm-hook path until the QA hook
  proves green
