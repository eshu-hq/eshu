// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package answerguardrail provides pure output guardrails for publishable
// answer text.
//
// The package owns deterministic citation-coverage, publish-safety, and
// answer-substance checks shared by live Ask Eshu responses and offline
// answer-quality scorecards. The answer-substance check rejects a circular,
// identity-only answer that only restates the question's entity and names no
// operational fact. It performs no I/O, starts no provider calls, and returns
// bounded findings that never echo rejected private or credential-like values.
package answerguardrail
