// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package statements holds SQL text for the generation retention store in the
// parent postgres package. It has no logic and no imports: each exported
// constant is a statement the parent binds and runs in its own transaction.
//
// TargetedCandidateLock is the retention pass's targeted re-lock of the
// provisionally selected candidates. It must stay in lockstep with the
// parent's general candidate query (generationRetentionCandidateQuery): the
// same eligibility rules, re-checked at lock time. The parent forwards it as
// generationRetentionTargetedCandidateQuery.
//
// The statements live here because the parent directory is at its file-count
// pin (scripts/lib/dirgate-grandfather.tsv) and its retention files sit at
// the 500-line cap.
package statements
