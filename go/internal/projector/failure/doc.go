// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package failure classifies a projection failure and decides what the queue
// does with it.
//
// [ClassifyFailure] maps a causal error onto a [FailureClass] and its
// [RetryDisposition]; [IsRetryable] answers the same question for a single
// error; [TriageFailure] turns a terminal failure into the dead-letter record
// an operator triages, and [ManualReviewTriageClasses] names the classes that
// need a human rather than a redrive.
//
// The classes are deliberately coarse and bounded: they are reported as a
// metric dimension, so an unbounded failure vocabulary would make the metric
// unusable at repo scale. An error the package cannot place is classified as a
// projection bug, not as a transient — an unknown failure must be visible, not
// retried forever.
//
// The package is a leaf: it imports no other projector package.
package failure
