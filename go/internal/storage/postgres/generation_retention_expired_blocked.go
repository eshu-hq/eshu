// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "time"

// This file holds the #7585 PR1 expired-but-blocked report: a pure-Go
// evaluation of one superseded generation against the policy's hard history
// ceiling. It reports only. It never authorizes or performs deletion: the
// candidate SQL's guards (status, active-generation id, live-work anti-join)
// stay the authoritative deletion bound, and the 10-year hold is untouched.
// Later PRs map live store signals (fact_work_items statuses, dependency
// references) onto GenerationRetentionEvaluationInput and surface the
// evaluations to operators; this PR fixes the reason vocabulary and the
// ceiling-edge semantics those PRs will share.

// GenerationRetentionBlockedReason names why hard-ceiling-expired history
// is retained instead of pruned. The set is closed so it can label operator
// telemetry. It mirrors the prune's safety exceptions: the candidate query
// never frees the active generation, generations with live work, or
// dependency-pinned generations, at any age.
type GenerationRetentionBlockedReason string

const (
	// GenerationRetentionBlockedActiveCurrent marks history that is still
	// its scope's active (current) generation. It is not actually
	// superseded-eligible; the report keeps it visible so a stuck active
	// pointer past the ceiling reads as blocked, never compliant.
	GenerationRetentionBlockedActiveCurrent GenerationRetentionBlockedReason = "active_current"
	// GenerationRetentionBlockedLiveWork marks history with in-flight
	// projection work (claimed or running): pruning it would delete facts
	// a worker is still writing.
	GenerationRetentionBlockedLiveWork GenerationRetentionBlockedReason = "live_work"
	// GenerationRetentionBlockedRetrying marks history with only retrying
	// work: nothing is writing now, but a retry may resume, so the prune
	// still holds it. It stays distinct from live work because it clears
	// on its own when the retries drain or dead-letter.
	GenerationRetentionBlockedRetrying GenerationRetentionBlockedReason = "retrying"
	// GenerationRetentionBlockedDependencyPin marks history another
	// generation or projection still references (content, ledger, or
	// shared-projection links): pruning it would orphan that reference.
	GenerationRetentionBlockedDependencyPin GenerationRetentionBlockedReason = "dependency_pin"
)

// GenerationRetentionEvaluationInput carries one generation's retention
// signals to the evaluation. Callers map their store reads onto these
// flags; the evaluation itself takes no database handle and performs no
// I/O, so the report path cannot lock, wait, or change prune behavior.
type GenerationRetentionEvaluationInput struct {
	// SupersededAt is the generation's original superseded_at, the same
	// instant the hard ceiling measures from.
	SupersededAt time.Time
	// IsActiveGeneration is true when the generation is still its scope's
	// active (current) generation.
	IsActiveGeneration bool
	// HasLiveWork is true when claimed or running work still references
	// the generation.
	HasLiveWork bool
	// HasRetryingWork is true when retrying work still references the
	// generation.
	HasRetryingWork bool
	// DependencyPinned is true when another generation or projection
	// still references the generation.
	DependencyPinned bool
}

// GenerationRetentionEvaluation is the retention posture of one superseded
// generation under the hard history ceiling: compliant history inside the
// ceiling, eligible history past it with no block, or expired-but-blocked
// history past it with the reason and age that keep it retained.
type GenerationRetentionEvaluation struct {
	// SupersededAge is now minus the original superseded_at, clamped at
	// zero for timestamps ahead of now.
	SupersededAge time.Duration
	// Expired is true when the history is older than the hard ceiling.
	Expired bool
	// BlockedReason is empty unless the history is expired and a safety
	// exception retains it.
	BlockedReason GenerationRetentionBlockedReason
}

// ExpiredBlocked reports whether the history is past the hard ceiling and
// retained by a safety exception: the generations PR1 surfaces to
// operators instead of silently calling compliant.
func (e GenerationRetentionEvaluation) ExpiredBlocked() bool {
	return e.Expired && e.BlockedReason != ""
}

// Compliant reports whether the history needs no operator attention: it is
// inside the hard ceiling. Expired-but-blocked history is not compliant,
// even though the prune retains it.
func (e GenerationRetentionEvaluation) Compliant() bool {
	return !e.Expired
}

// EvaluateGenerationRetention classifies one superseded generation against
// the policy's hard history ceiling (#7585). An unset ceiling resolves
// through normalize, the same 90-day default (or longer soft window) the
// prune uses, so the report and the deletion bound cannot drift apart.
// Expiry mirrors the candidate SQL exactly: history is expired only when
// strictly older than the ceiling (`superseded_at < now - ceiling`), so
// history exactly at the ceiling stays compliant. Past the ceiling the
// first matching safety exception wins, active/current before live work
// before retrying before a dependency pin; with no block the history is
// expired and eligible, carrying an empty reason.
func (p GenerationRetentionPolicy) EvaluateGenerationRetention(
	now time.Time,
	input GenerationRetentionEvaluationInput,
) GenerationRetentionEvaluation {
	policy := p.normalize()
	age := now.Sub(input.SupersededAt)
	if age < 0 {
		age = 0
	}
	evaluation := GenerationRetentionEvaluation{SupersededAge: age}
	if age <= policy.HardMaxSupersededAge {
		return evaluation
	}
	evaluation.Expired = true
	switch {
	case input.IsActiveGeneration:
		evaluation.BlockedReason = GenerationRetentionBlockedActiveCurrent
	case input.HasLiveWork:
		evaluation.BlockedReason = GenerationRetentionBlockedLiveWork
	case input.HasRetryingWork:
		evaluation.BlockedReason = GenerationRetentionBlockedRetrying
	case input.DependencyPinned:
		evaluation.BlockedReason = GenerationRetentionBlockedDependencyPin
	}
	return evaluation
}
