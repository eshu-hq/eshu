# AGENTS.md - services/ec2/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Keep AWS SDK calls here and map only scanner-owned network metadata.
- Allowed read families are EC2 VPCs, subnets, security groups, security group
  rules, and network interfaces.
- Wrap every AWS page or point read in `recordAPICall`.
- Do not add instance inventory, volume reads, snapshots, user data, console
  output, credential, STS, mutation, graph, or reducer behavior here.
- Keep IDs, ARNs, names, tags, descriptions, page tokens, and raw AWS errors
  out of metric labels.
