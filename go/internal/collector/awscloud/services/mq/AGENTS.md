# AGENTS.md - internal/collector/awscloud/services/mq guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Amazon MQ domain types.
3. `scanner.go` - broker and configuration emission.
4. `relationships.go` - broker relationship selection rules.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep Amazon MQ API access behind `Client`; do not import the AWS SDK into this
  package.
- Never call CreateBroker, UpdateBroker, DeleteBroker, RebootBroker,
  CreateConfiguration, UpdateConfiguration, DeleteConfiguration, CreateUser,
  UpdateUser, DeleteUser, CreateTag, DeleteTag, or any other Amazon MQ mutation
  API.
- Never model or persist broker user passwords. The `Broker` type carries
  usernames only. Do not add a password field and do not call DescribeUser,
  which returns the password-bearing User resource.
- Never persist the configuration XML body. Persist configuration identifiers
  and the latest revision summary only, never the body that
  DescribeConfigurationRevision would return; it can carry inline credentials
  and ACL rules.
- Never read or persist queue or topic message contents.
- Emit the broker-to-KMS-key relationship only when the broker uses a
  customer-managed key reported in ARN form (UseAwsOwnedKey is false). Do not
  emit a KMS relationship for AWS-owned keys.
- Emit broker-to-subnet and broker-to-security-group relationships using the
  AWS subnet IDs and security group IDs reported by DescribeBroker. Do not
  synthesize a broker-to-VPC edge from subnet placement; reserve
  `RelationshipMQBrokerInVPC` for evidence that reports the VPC identity.
- Emit broker-to-CloudWatch-log-group relationships from the general and audit
  log group names only; never read log contents.
- Preserve stable broker and configuration identities across repeated
  observations in the same AWS generation.
- Keep broker ARNs, configuration ARNs, KMS key ARNs, subnet IDs, security
  group IDs, log group names, usernames, tags, engine versions, and instance
  types out of metric labels.

## Common Changes

- Add a new Amazon MQ metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when the Amazon MQ API reports both sides
  directly and the target identity is not a secret-shaped payload.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not mutate Amazon MQ brokers, configurations, or users.
- Do not read or persist broker user passwords, configuration XML bodies, or
  queue/topic message contents.
- Do not resolve broker names, configuration names, tags, or usernames into
  workload, deployment, environment, or ownership truth here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
