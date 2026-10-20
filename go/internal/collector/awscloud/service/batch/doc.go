// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package batch maps AWS Batch observations into AWS cloud fact envelopes.
//
// The package owns scanner-level Batch fact selection for compute
// environments, job queues, job definitions, scheduling policies, recent jobs,
// and their relationships. It is metadata-only and never persists
// job-definition container command lists, container environment values in
// clear text, resolved secret values, scheduling-policy fair-share weight
// state, job parameters, or container overrides. AWS SDK pagination,
// credentials, persistence, graph projection, and reducer-owned correlation
// live outside this package.
package batch
