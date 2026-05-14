// Package awsruntime adapts AWS cloud service scanners to workflow-claimed
// collector execution.
//
// The package owns claim parsing, target authorization, claim-scoped
// credential acquisition, scanner-side status updates, and collected-generation
// construction for AWS cloud work items. Service scanners own AWS source
// observation, including EKS control-plane evidence and redaction-sensitive ECS
// and Lambda fields, while reducers own canonical graph truth. The production
// registry includes metadata-only adapters for SQS, SNS, EventBridge, S3, RDS,
// DynamoDB, CloudWatch Logs, CloudFront, and API Gateway without broadening
// those services into payload, policy, data-plane, credential, or mutation
// APIs.
package awsruntime
