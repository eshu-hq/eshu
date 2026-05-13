// Package awssdk adapts the AWS SDK for Go v2 ECR client to the ECR scanner
// contract.
//
// The package owns ECR repository and image pagination, lifecycle policy reads,
// repository tag reads, AWS API telemetry, and throttle detection. Scanner
// packages own fact selection and do not import the AWS SDK directly.
package awssdk
