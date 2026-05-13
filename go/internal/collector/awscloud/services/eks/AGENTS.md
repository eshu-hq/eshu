# AGENTS.md - internal/collector/awscloud/services/eks guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned EKS records and client seam.
3. `scanner.go` - cluster, OIDC provider, node group, and add-on fact-envelope
   construction.
4. `relationships.go` - IAM role, OIDC provider, subnet, and security group
   relationship evidence.
5. `awssdk/README.md` - AWS SDK adapter contract.

## Invariants

- EKS facts are reported AWS evidence. Do not materialize graph truth here.
- Preserve OIDC issuer URL, thumbprints, and client IDs for IRSA trust-chain
  analysis.
- Preserve cluster service roles, node group roles, and add-on service-account
  roles as IAM join evidence only.
- Preserve subnet and security group IDs as EC2 topology join evidence only.
- Deduplicate duplicate cluster security group reports before envelope
  emission.
- Keep AWS SDK calls out of this package. Runtime adapters own SDK pagination,
  retries, throttling, and credential loading.

## Common Changes

- Add a new EKS attribute in `types.go` and map it in `scanner.go`.
- Add a new relationship in `relationships.go` only when the downstream reducer
  can use the evidence without collector-side inference.
- Extend SDK mapping in `awssdk/client.go`; keep AWS SDK types out of this
  package.

## What Not To Change Without An ADR

- Do not call AWS APIs from this package.
- Do not infer source repository, deployment environment, Kubernetes workload,
  or ownership from cluster names, tags, namespaces, add-on names, or node group
  names.
- Do not collect Kubernetes API objects here; this scanner is AWS control-plane
  evidence only.
