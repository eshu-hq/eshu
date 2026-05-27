// Command collector-aws-cloud runs the claim-aware AWS cloud collector.
//
// The command reads declarative AWS collector instances, claims bounded
// `(account, region, service_kind)` work items from the workflow store, obtains
// claim-scoped AWS credentials, and commits reported AWS facts through the
// shared ingestion store. ECS, Lambda, Security Hub, and Organizations targets
// require a redaction key before scans can emit sensitive-derived metadata
// safely; metadata-only services such as SQS, SNS, EventBridge, S3, Athena,
// Glue, ElastiCache, MSK, Step Functions, and Access Analyzer do not require
// that key.
package main
