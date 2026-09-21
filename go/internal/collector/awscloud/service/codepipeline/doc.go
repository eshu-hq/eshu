// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codepipeline maps AWS CodePipeline metadata into AWS cloud collector
// facts.
//
// The package owns scanner-level normalization only. It never calls the AWS SDK
// directly and never persists an action configuration value, a webhook
// authentication secret token, or a GitHub source-action OAuth token. SDK
// adapters supply scanner-owned records that carry action configuration KEY
// names only, allowlisted non-secret build/deploy/invoke target identifiers,
// and source-revision summaries already redacted. Scanner emits aws_resource
// facts for pipelines, recent executions, webhooks, and custom action types
// plus aws_relationship facts for the pipeline, stage, and action edges
// CodePipeline reports. Scan fails closed when the redaction key is zero so a
// secret pasted into a source-revision summary cannot leak.
package codepipeline
