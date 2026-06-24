// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 CodePipeline calls into scanner-owned
// metadata.
//
// The adapter only calls metadata read operations: ListPipelines, GetPipeline,
// ListPipelineExecutions, ListWebhooks, ListActionTypes, and
// ListTagsForResource. It must never call any mutation API, any
// execution-control API, any webhook-management API, any custom-action
// mutation API, or any job-worker API. The job-worker plane is excluded because
// PollForJobs, GetJobDetails, and the third-party variants return action
// configuration secret values. The adapter drops every action configuration
// value (retaining configuration key names and allowlisted non-secret target
// identifiers only), never reads the webhook authentication secret token, and
// routes source-revision summaries through the redaction library before they
// reach scanner types so a pasted secret cannot persist raw.
package awssdk
