// Command collector-aws-cloud runs the claim-aware AWS cloud collector.
//
// The command reads declarative AWS collector instances, claims bounded
// `(account, region, service_kind)` work items from the workflow store, obtains
// claim-scoped AWS credentials, and commits reported AWS facts through the
// shared ingestion store. Targets that allow any scanner declaring
// RequiresRedactionKey in its runtimebind registration require an
// ESHU_AWS_REDACTION_KEY before scans can emit sensitive-derived metadata
// safely; the command derives that requirement from the awsruntime registry
// (awsConfigNeedsRedactionKey reads ServiceKindsRequiringRedactionKey).
// Metadata-only services such as SQS, SNS, EventBridge, GuardDuty, S3, Athena,
// Glue, ElastiCache, MSK, Step Functions, and Access Analyzer leave the flag
// unset and do not require that key.
package main
