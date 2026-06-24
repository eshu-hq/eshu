// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package answerquality scores captured API, MCP, CLI, and hosted answer runs
// against the publish-safe dogfood criteria for representative Eshu answer
// families.
//
// The package is intentionally offline: callers capture redacted answers from
// real surfaces, then pass the evidence to Score. This keeps private endpoints,
// repository paths, credentials, and source excerpts out of committed scorecard
// artifacts while preserving a rerunnable pass/fail contract. Optional
// narration rows are judged against deterministic fallback metadata and the
// governed narration validator; the fallback remains canonical when narration
// is unavailable, rejected, or unsafe.
//
// ScoreReport extends the same offline gate to composed service intelligence
// reports: it rejects a report that carries a confident unsupported claim, a
// citation gap, a hidden truncation, a missing limitation, an upgraded truth
// class, or an unexecutable next call. ReportCorpus ships the share-safe fixture
// corpus that backs the report gate.
package answerquality
