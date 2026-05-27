# AGENTS.md - internal/collector/awscloud/services/msk guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned MSK domain types.
3. `scanner.go` - cluster, configuration, and replicator emission.
4. `relationships.go` - cluster/replicator relationship selection rules.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep MSK API access behind `Client`; do not import the AWS SDK into this
  package.
- Never call CreateClusterV2, UpdateClusterKafkaVersion, DeleteCluster,
  RebootBroker, UpdateBrokerCount, UpdateBrokerStorage, UpdateBrokerType,
  UpdateConfiguration, CreateConfiguration, CreateReplicator, DeleteReplicator,
  DeleteConfiguration, PutClusterPolicy, TagResource, UntagResource,
  CreateTopic, DeleteTopic, BatchAssociateScramSecret,
  BatchDisassociateScramSecret, or any other MSK mutation API.
- Never persist raw broker server.properties bodies, configuration revision
  bodies, broker log contents, Kafka topic data, Kafka message contents,
  bootstrap broker endpoints, or SCRAM secret material.
- Persist configuration identifiers and the latest revision summary, never the
  revision body that DescribeConfigurationRevision would return.
- Persist replicator topic and consumer-group filter patterns only as include
  and exclude counts; do not store the raw regex lists, target inputs,
  HTTP headers, or query parameters.
- Emit cluster-to-KMS-key, cluster-to-IAM-role, and cluster-to-configuration
  relationships only when AWS reports the ARN form for the target identity.
- Emit cluster-to-subnet and cluster-to-security-group relationships using the
  AWS subnet IDs and security group IDs reported by the provisioned broker
  node group or by serverless VPC configs.
- Preserve stable cluster, configuration, and replicator identities across
  repeated observations in the same AWS generation.
- Keep cluster ARNs, configuration ARNs, replicator ARNs, KMS key ARNs, IAM
  role ARNs, subnet IDs, security group IDs, tags, kafka versions, broker
  instance types, and storage sizes out of metric labels.

## Common Changes

- Add a new MSK metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders.
- Add new relationship evidence only when the MSK API reports both sides
  directly and the target identity is not a secret-shaped payload.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not mutate MSK clusters, configurations, replicators, or topics.
- Do not read or persist Kafka topic message contents, broker server.properties
  bodies, broker log contents, SCRAM secret material, or bootstrap broker
  endpoints.
- Do not resolve cluster names, configuration names, replicator names, tags, or
  alias values into workload, deployment, environment, or ownership truth here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
