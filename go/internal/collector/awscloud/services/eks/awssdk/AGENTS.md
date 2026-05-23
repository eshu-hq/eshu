# AGENTS.md - services/eks/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are EKS list/describe calls for clusters, nodegroups, and
  addons plus IAM OIDC provider list/get calls.
- Wrap every page and point read in `recordAPICall`.
- Do not request Kubernetes credentials, connect to Kubernetes APIs, read live
  workloads, mutate EKS/IAM resources, or add graph/reducer behavior here.
- Return stable reported metadata only; never expose credential material or
  bearer tokens.
- Keep cluster names, ARNs, endpoint URLs, OIDC URLs, tags, page tokens, and
  raw AWS errors out of metric labels.
