// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semanticstore persists the semantic-extraction work queue: planned
// extraction records move through queued, claimed, succeeded, retried, and
// dead-lettered states with lease fencing on every mutation.
//
// SemanticExtractionQueueStore applies ingestion plans as metadata-only
// upserts, claims the next due row with FOR UPDATE SKIP LOCKED in priority
// order, retries and dead-letters with lease-owner checks, skips rows by
// policy, and reports redacted status snapshots for the status page. The
// DDL and statement text live beside the store and move byte-identically
// with it.
//
// The ReadSemanticExtractionObservability reader and the
// SemanticExtractionObservabilityQuery statement stay exported because the
// status family (still in the postgres root until its own #6693 leaf) and
// the root proof-domain harness read through them. Shared nothing else
// leaves this package: every helper is family-private. This package must
// not import the parent postgres package.
package semanticstore
