// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command collector-aws-cloud runs the AWS cloud collector.
//
// The -mode flag selects the runtime. The default, claimed-live, runs the
// claim-aware collector described below and preserves the behavior of existing
// live deployments byte-for-byte. The opt-in fixture mode runs a fully offline
// replay: it loads a declarative fixture estate from -config, constructs an
// awsruntime.FixtureSource, and commits the same aws_resource / aws_relationship
// facts as the live scanners through the shared ingestion store, with no AWS
// credentials and no network calls. Fixture mode requires no redaction key
// because AWS resource and relationship envelopes carry no fingerprinted
// material. The -config flag is required in fixture mode and rejected in
// claimed-live mode.
//
// In claimed-live mode the command reads declarative AWS collector instances,
// claims bounded
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
