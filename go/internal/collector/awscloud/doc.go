// Package awscloud defines AWS cloud scanner fact identity and runtime-neutral
// observation contracts.
//
// The package owns AWS source observations up to durable fact envelopes and
// bounded scan-status accounting contracts. It does not call AWS APIs directly,
// schedule workflow claims, or write graph truth. Service-specific scanners,
// including IAM, EC2, ECR, ECS, EKS, ELBv2, Lambda, Route 53, SQS, SNS,
// EventBridge, S3, RDS, DynamoDB, CloudWatch Logs, CloudFront, and API Gateway
// slices, convert AWS API data into these contracts before the shared collector
// and reducer paths persist and materialize them.
// Sensitive service fields, including ECS and Lambda environment values, must be
// redacted before callers build envelopes.
// SQS scans are limited to queue metadata and reported dead-letter queue
// relationships; message bodies and queue policy JSON stay outside the
// contract.
// SNS scans are limited to topic metadata and ARN-addressable subscription
// relationships; message payloads, topic policy JSON, data-protection-policy
// JSON, and raw non-ARN subscription endpoints stay outside the contract.
// EventBridge scans are limited to event bus and rule metadata plus
// ARN-addressable target relationships; event payloads, event bus policy JSON,
// target input payloads, input transformers, and HTTP parameters stay outside
// the contract.
// S3 scans are limited to bucket metadata and server-access-log target
// relationships; object inventory, bucket policy JSON, ACL grants,
// replication, lifecycle, notifications, inventory, analytics, and metrics
// configuration stay outside the contract.
// RDS scans are limited to metadata and reported dependency relationships;
// database connections, database names, master usernames, snapshots, log
// contents, Performance Insights samples, schemas, tables, and row data stay
// outside the contract.
// DynamoDB scans are limited to table metadata and reported KMS relationships;
// item values, table scans, table queries, stream records, backup/export
// payloads, resource policies, PartiQL output, and mutations stay outside the
// contract.
// CloudWatch Logs scans are limited to log group metadata and reported KMS
// relationships; log events, log stream payloads, Insights query results,
// export payloads, resource policies, subscription payloads, and mutations stay
// outside the contract.
// CloudFront scans are limited to distribution metadata and reported ACM
// certificate and WAF web ACL relationships; object contents, origin payloads,
// distribution config payloads, policy documents, certificate bodies, private
// keys, origin custom header values, and mutations stay outside the contract.
// API Gateway scans are limited to API, stage, custom-domain, mapping, and
// ARN-addressable integration metadata; API keys, authorizer secrets, policy
// JSON, integration credentials, stage variable values, templates, payloads,
// exports, and mutations stay outside the contract.
package awscloud
