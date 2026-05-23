# AGENTS.md - internal/collector/awscloud/services/elbv2 guidance

## Read First

1. `README.md` - package purpose, flow, and invariants.
2. `types.go` - scanner-owned ELBv2 records and client contract.
3. `scanner.go` - fact selection, resource envelopes, and core relationships.
4. `relationships.go` - route relationship aggregation.
5. `attributes.go` - typed action and condition attribute maps.
6. `awssdk/README.md` - AWS SDK pagination and response mapping.

## Invariants

- Do not call AWS APIs from this package. The `awssdk` adapter owns AWS SDK
  calls and telemetry.
- Preserve route topology as reported source evidence:
  `LoadBalancer -> Listener -> Rule` and `Listener -> TargetGroup`.
- Preserve target group attachment evidence so ECS service bindings can join to
  load balancers through target groups.
- Do not persist target health status. Target health is live/noisy operational
  state, not stable topology truth.
- Keep listener rule conditions typed. Do not flatten host-header,
  path-pattern, HTTP-header, query-string, source-IP, or method conditions into
  one ad hoc string.
- Do not infer application ownership, environment, service identity, or public
  exposure from names, DNS names, tags, or host-header values here.

## Common Changes

- Add a new ELBv2 relationship in `scanner.go` or `relationships.go` only when
  a durable topology join needs it.
- Add a new resource attribute in `scanner.go` only when it supports routing,
  network placement, or later correlation.
- Add new typed rule-condition fields in `types.go`, `attributes.go`, and
  `awssdk/conditions.go` together.

## What Not To Change Without An ADR

- Do not add DescribeTargetHealth or live target state to the fact stream.
- Do not make the scanner write graph rows directly.
- Do not put ARNs, DNS names, rule conditions, tags, or listener ports in metric
  labels.
