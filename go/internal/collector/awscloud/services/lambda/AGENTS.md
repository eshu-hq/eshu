# AGENTS.md - internal/collector/awscloud/services/lambda guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Lambda records and client seam.
3. `scanner.go` - fact-envelope construction and environment redaction.
4. `relationships.go` - alias, event-source, image, IAM, subnet, and security
   group relationship evidence.
5. `awssdk/README.md` - AWS SDK adapter contract.

## Invariants

- Lambda facts are reported AWS evidence. Do not materialize graph truth here.
- Redact every environment variable value with `redact.String` before
  persistence. Never preserve raw function environment values in facts, logs,
  spans, tests, or docs.
- Preserve container image URI and resolved image URI as join evidence for ECR.
- Preserve VPC subnet and security group IDs as join evidence for EC2 topology.
- Preserve event-source ARNs on mapping facts and mapping-to-function
  relationships.
- Do not persist AWS Lambda GetFunction package download URLs; they are
  presigned and short-lived.

## Common Changes

- Add a new Lambda attribute in `Function` and map it in `scanner.go`.
- Add a new relationship in `relationships.go` only when the downstream reducer
  can use the evidence without collector-side inference.
- Extend SDK mapping in `awssdk/client.go`; keep AWS SDK types out of this
  package.

## What Not To Change Without An ADR

- Do not call AWS APIs from this package.
- Do not infer source repository, deployment environment, or workload ownership
  from function names, aliases, tags, or event-source names.
- Do not add live invocation state, logs, or function code contents to facts.
