// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package queuestore holds the failure-classification and retry-backoff
// helpers shared by the Postgres projector and reducer work queues. It is
// the `queue/` leaf of the storage/postgres split (#6693); the queue types
// themselves (ProjectorQueue, ReducerQueue) stay in the parent postgres
// package until their own later #6693 moves (queue/projector/,
// queue/reducer/).
//
// QueueFailureMetadata and DeadLetterTriageMetadata reconcile a failure
// cause into the durable failure_class, message, and details values written
// to a work item's row: a self-classifying error (one implementing an
// unexported FailureClass()/FailureDetails() pair, e.g.
// GraphWriteTimeoutError) keeps its own curated class and details, and
// DeadLetterTriageMetadata otherwise falls back to the operator-facing
// triage class and details from internal/projector/failure.TriageFailure.
//
// ComputeRetryDelay returns the exponential-backoff-with-jitter delay to add
// to "now" when scheduling a retry (issue #4450), replacing the historical
// fixed-delay behavior that let many simultaneously-failing work items
// reconverge on the exact same visible_at and self-reinforce into a retry
// storm. DefaultRetryMaxDelayFallback and DefaultJitterSource are the
// production defaults a queue falls back to when its caller leaves
// MaxRetryDelay/JitterSource unset.
//
// Every function here is pure: none read rows, write rows, or hold locks.
// This package must not import the parent postgres package.
package queuestore
