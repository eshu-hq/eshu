# AGENTS.md - internal/collector/awscloud/services/detective guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Detective domain types.
3. `scanner.go`, `relationships.go`, and `helpers.go` - resource and
   relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud.md` - AWS collector runtime
   requirements.
6. `docs/public/services/collector-aws-cloud-scanners.md` - scanner coverage
   and data boundary.

## Invariants

- Keep Detective API access behind `Client`; do not import the AWS SDK into
  this package.
- Emit reported evidence only. Do not infer environment, deployment, workload,
  ownership, attacker identity, or deployable-unit truth from graph ARNs, tags,
  accounts, or data-source packages.
- Never read or persist investigations, finding groups, indicators, or
  per-member usage volume. Never read a member's contact email.
- The behavior graph ARN is both the graph node ARN and its `resource_id`.
  Source every outgoing edge on that ARN so the graph's edges join the graph
  node it publishes.
- The graph-to-member-account edge targets `aws_organizations_account` by the
  bare 12-digit account id (the organizations scanner's published
  `resource_id`). The graph-to-GuardDuty-detector edge targets
  `aws_guardduty_detector` by the bare detector id (the guardduty scanner's
  published `resource_id`).
- Do not fabricate a GuardDuty detector id. Detective's metadata APIs do not
  report one, so emit the detector edge only when a resolver supplies a real id
  on `Graph.GuardDutyDetectorID`; otherwise omit it. A dangling or guessed edge
  is a correctness failure.
- Pass graph ARNs through unchanged so synthesized identities inherit the
  graph's partition. Never hardcode `arn:aws:`.
- Key member-account `resource_id` on the graph ARN and account id, never on
  list order or index.
- Keep graph ARNs, detector ids, account ids, and tags out of metric labels.

## Common Changes

- Add a new safe Detective metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when Detective directly reports both sides
  and the target matches an existing scanner's published `resource_id`.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not call GetInvestigation, ListInvestigations, StartInvestigation,
  ListIndicators, BatchGetMembershipDatasources, GetMembers, or any mutation
  API.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
