// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package multicloudruntimedrift builds the multi_cloud_runtime_drift
// reducer intent from one immutable scope generation. The trigger fires on
// the earliest gcp_cloud_resource or azure_cloud_resource fact (issue #5759,
// closing the "registered but never enqueued" gap left since #1997/#1998).
// aws_resource facts alone never trigger this intent: DomainAWSCloudRuntimeDrift
// already publishes AWS runtime-drift findings end-to-end, so an AWS-only
// scope generation must not enqueue this domain at all. A scope carrying both
// AWS and GCP/Azure facts still enqueues here for its GCP/Azure coverage; the
// reducer's MultiCloudRuntimeDriftHandler.Handle drops any AWS-provider row
// its shared evidence loader also resolves before publishing, so the two
// domains never disagree about the same AWS resource. The intent's
// source-system label is the shared two-tier projectorintent.SourceSystem
// fallback (trimmed SourceRef.SourceSystem, else trimmed CollectorKind) —
// the pre-extraction local helper had the identical body, so this is a
// behavior-preserving substitution, not a change. Root projector assembly
// owns lookup construction and lifetime, invocation order, queue writes,
// retries, and telemetry.
package multicloudruntimedrift
