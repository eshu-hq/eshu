# AGENTS.md - internal/collector/awscloud/services/ses guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned SES domain types.
3. `scanner.go` - identity, configuration set, event destination, and pool
   resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, partition-aware ARN synthesis,
   destination-class derivation, and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep SES API access behind `Client`; do not import the AWS SDK into this
  package.
- Never send email, never read message or template bodies, and never persist
  DKIM private keys, DKIM signing tokens, identity policy documents, or SMTP
  credentials. Record only identity, verification status, DKIM enabled/origin
  enums, and resolvable references.
- The email-identity node publishes its resource_id as the trimmed identity
  name. The configuration-set node and dedicated-IP-pool node publish theirs as
  the trimmed set name and pool name. Key edges on those exact values.
- The event-destination node publishes its resource_id as
  `<configuration-set>/<destination>`.
- Key the identity-to-default-configuration-set edge on the set name, the
  configuration-set-to-dedicated-IP-pool edge on the pool name, the
  event-destination-to-SNS-topic edge on the reported topic ARN, and the
  event-destination-to-Firehose edge on the reported delivery stream ARN. These
  match how the SNS and Firehose scanners publish their resource_ids.
- Emit the identity-DKIM-to-KMS-key edge only when AWS reports a key identifier
  on the DKIM attributes. Set `target_arn` only when ARN-shaped.
- Synthesize identity, configuration-set, and dedicated-IP-pool ARNs with
  `awscloud.PartitionForBoundary`; never hardcode `arn:aws:`.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from SES names or tags.
- Preserve stable identity, set, destination, and pool identities across
  repeated observations in the same AWS generation.
- Keep SES ARNs, names, domains, tags, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new SES metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry message content, a DKIM token, a
  policy document, or a credential, leave it out of the scanner contract.
- Add new relationship evidence only when the SES API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not send email, read message or template bodies, read DKIM tokens or
  signing keys, read identity policy documents, or call any SES mutation API.
- Do not resolve SES names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
