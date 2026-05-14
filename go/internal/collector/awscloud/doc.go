// Package awscloud defines AWS cloud scanner fact identity and runtime-neutral
// observation contracts.
//
// The package owns AWS source observations up to durable fact envelopes and
// bounded scan-status accounting contracts. It does not call AWS APIs directly,
// schedule workflow claims, or write graph truth. Service-specific scanners,
// including IAM, EC2, ECR, ECS, EKS, ELBv2, Lambda, Route 53, SQS, SNS, and
// EventBridge slices, convert AWS API data into these contracts before the
// shared collector and reducer paths persist and materialize them.
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
package awscloud
