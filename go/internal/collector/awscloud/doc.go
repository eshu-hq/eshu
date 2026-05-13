// Package awscloud defines AWS cloud scanner fact identity and runtime-neutral
// observation contracts.
//
// The package owns AWS source observations up to durable fact envelopes and
// bounded scan-status accounting contracts. It does not call AWS APIs directly,
// schedule workflow claims, or write graph truth. Service-specific scanners,
// including IAM, EC2, ECR, ECS, EKS, ELBv2, Lambda, and Route 53 slices,
// convert AWS API data into these contracts before the shared collector and
// reducer paths persist and materialize them.
// Sensitive service fields, including ECS and Lambda environment values, must be
// redacted before callers build envelopes.
package awscloud
