// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codeguru maps Amazon CodeGuru Reviewer repository associations and
// CodeGuru Profiler profiling groups into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for CodeGuru Reviewer
// repository associations and CodeGuru Profiler profiling groups, plus the
// association-reviews-CodeCommit-repository relationship (emitted only for
// CodeCommit-provider associations, keyed to the partition-aware CodeCommit
// repository ARN the CodeCommit scanner publishes). Code-review findings,
// recommendation text, analyzed source content, profiling samples, aggregated
// profiles, flame graphs, recommendation reports, and agent telemetry stay
// outside this package contract: the scanner is metadata-only.
package codeguru
