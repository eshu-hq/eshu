# AGENTS.md - services/eks

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep EKS and IAM AWS access behind `Client`; the scanner package must not
  import the AWS SDK.
- Emit reported cluster, nodegroup, addon, OIDC provider, subnet, security
  group, and IAM relationship evidence only.
- Do not connect to Kubernetes APIs, read workloads, fetch tokens, or infer
  workload, environment, repository, ownership, or deployable-unit truth.
- Treat IAM OIDC provider data as control-plane join evidence, not credential
  material.
- Keep cluster names, ARNs, endpoint URLs, OIDC URLs, tags, subnet IDs,
  security group IDs, raw AWS errors, and page tokens out of metric labels.
