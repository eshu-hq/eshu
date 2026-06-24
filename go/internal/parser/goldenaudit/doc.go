// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package goldenaudit compares parser and reducer graph observations against
// independent source-language golden fixtures.
//
// Golden fixtures must describe expected nodes and edges directly rather than
// copying Eshu output back into the expected file. The package reports missing,
// unexpected, and duplicate graph facts deterministically so language-depth
// work can prove accuracy before promoting support claims.
//
// CompareGraph reports exact node and edge drift. ScoreAccuracy adds
// precision/recall of observed edges against golden edges, per relationship
// type and overall, with a wrong-target vs missing vs extra breakdown so a
// resolver that points an edge at the wrong target is caught even when its tier
// distribution looks healthy. The Report returned by CompareGraph carries this
// AccuracyResult in its Accuracy field, and Report.Summary appends the overall
// accuracy_precision/accuracy_recall. Accuracy is surfaced additively:
// Report.Pass still gates only on structural node/edge drift, not on
// precision/recall.
//
// AccuracyResult.MeetsThreshold turns the accuracy metric into an opt-in
// regression guard: a golden test can assert precision and recall stay at or
// above a bar and, on failure, receive a bounded one-block message listing the
// offending edges (wrong-target first, then missing, then extra) by Edge.Key().
// AccuracyResult.Perfect is the MeetsThreshold(1.0, 1.0) convenience. These are
// measurement only; neither parses source nor writes graph rows.
package goldenaudit
