# AGENTS.md - services/elbv2

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep ELBv2 AWS access behind `Client`; the scanner package must not import
  the AWS SDK.
- Emit reported metadata for load balancers, listeners, listener rules, target
  groups, tags, and direct certificate or target relationships.
- Do not infer workload, environment, repository, ownership, public exposure,
  or deployable-unit truth.
- Do not add target health reads, request payload reads, access-log reads,
  policy persistence, or mutation APIs.
- Keep load balancer names, listener ARNs, target group ARNs, tags, hostnames,
  paths, raw AWS errors, and page tokens out of metric labels.
