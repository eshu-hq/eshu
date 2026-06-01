# AWS IAM Scanner

## Purpose

`internal/collector/awscloud/services/iam` owns the IAM scanner contract for the
AWS cloud collector. It converts roles, users, managed policies, instance
profiles, trust principals, and IAM relationships into `awscloud` observations.
It also emits derived `aws_iam_permission` facts: the normalized, metadata-only
projection of inline, attached managed, and role trust policy statements.

## Ownership boundary

This package owns scanner-level IAM fact selection and IAM relationship
mapping. It does not own AWS SDK pagination, STS credentials, workflow claims,
fact persistence, graph writes, or reducer admission.

```mermaid
flowchart LR
  A["IAM API adapter"] --> B["Client"]
  B --> C["Scanner.Scan"]
  C --> D["awscloud.ResourceObservation"]
  C --> E["awscloud.RelationshipObservation"]
  C --> G["awscloud.IAMPermissionObservation"]
  D --> F["facts.Envelope"]
  E --> F
  G --> F
```

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - minimal IAM read surface consumed by `Scanner`.
- `Scanner` - emits IAM resource, relationship, and derived permission fact
  envelopes for one boundary.
- `Role` - scanner-owned IAM role representation, including its normalized
  permission statements.
- `User` - scanner-owned IAM user principal, including its normalized permission
  statements.
- `Policy` - scanner-owned IAM managed policy representation.
- `InstanceProfile` - scanner-owned IAM instance profile representation.
- `TrustPrincipal` - normalized principal from a role trust policy.
- `PolicyStatement` - normalized, metadata-only IAM policy statement (effect,
  action set, resource pattern, condition-key summary; no raw JSON or values).

## Dependencies

- `internal/collector/awscloud` for boundaries, resource and relationship
  constants, and envelope builders.
- `internal/facts` for the emitted fact envelope type.

The package depends on a small `Client` interface rather than the AWS SDK for Go
v2 so tests can use fake clients and runtime adapters can own SDK behavior.

## Telemetry

This scanner emits no metrics, spans, or logs directly. The runtime adapter that
implements `Client` must record IAM API call counts, throttles, page latency,
scan duration, and warnings/failures.

## Gotchas / invariants

- IAM is global, but scans still carry the claim region from the AWS collector
  boundary so the scheduler can partition work consistently.
- Role trust principals become relationships to principal identities; they do
  not create canonical principal graph truth in this package.
- Inline policy names remain role attributes. Managed policies become resources
  and role-to-policy relationships.
- The scanner stops on client errors. Runtime adapters decide whether an AWS
  service error is retryable, terminal, or a warning fact.
- Trust policy JSON is payload evidence. Do not promote it to metric labels.
- Derived `aws_iam_permission` facts are metadata-only. The scanner emits only
  the normalized statement (effect, action set, resource pattern, condition
  KEYS, and trust assume-principals). It never persists the raw policy JSON body
  or condition values, which can embed source IPs, tags, or other sensitive
  selectors. The SDK adapter normalizes documents at the wiring boundary so this
  package never holds raw policy JSON.
- Per-principal managed policy document fan-out is bounded in the SDK adapter to
  avoid an N+1 against IAM (each managed document costs a GetPolicy +
  GetPolicyVersion pair). The scanner consumes already-normalized statements.
- These facts are emitted but not yet consumed. The reducer graph projection
  (CAN_ASSUME / escalation-primitive edges) is a separate principal-review PR
  under issue #1134.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
