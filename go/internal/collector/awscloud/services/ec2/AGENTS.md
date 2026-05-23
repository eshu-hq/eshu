# AGENTS.md - services/ec2

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep EC2 AWS access behind `Client`; the scanner package must not import the
  AWS SDK.
- Emit network-topology evidence for VPCs, subnets, security groups, security
  group rules, network interfaces, and direct placement or attachment links.
- Do not add EC2 instance inventory, graph writes, reducer logic, or query
  behavior here.
- Do not infer public exposure, workload, environment, ownership, repository,
  or deployable-unit truth from names, tags, descriptions, ARNs, or accounts.
- Keep descriptions and tags as fact payload evidence only; never use them as
  metric labels.
- Keep VPC IDs, subnet IDs, security group IDs, ENI IDs, ARNs, names, tags,
  descriptions, raw AWS errors, and page tokens out of metric labels.
