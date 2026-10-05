// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
)

// crossRepoDeadCodeConsumerCoverageReason is the needs_evidence reason a
// producer symbol carries when a consumer repository it is judged against has
// no reachability watermark for its active generation, a truncated one, or one
// from an older verdict schema epoch (#7547).
const crossRepoDeadCodeConsumerCoverageReason = "consumer_coverage_incomplete"

// crossRepoDeadCodeCoverageStore answers whether the consumer repositories a
// request is judged against are free of those gaps. The content
// store implements it next to crossRepoDeadCodeEvidenceStore; a store that does
// not leaves the coverage unknown, which is never read as covered.
type crossRepoDeadCodeCoverageStore interface {
	CrossRepoDeadCodeConsumerCoverage(
		ctx context.Context,
		request code.CrossRepoDeadCodeCoverageRequest,
	) (code.CrossRepoDeadCodeCoverage, error)
}

// crossRepoDeadCodeConsumerCoverageResult is the coverage check's outcome for
// one request. The zero value means the check did not run -- the evidence read
// was unavailable or no candidate needed classifying -- which blocks nothing.
// Unavailable is set when the check should have run but the store cannot answer
// it.
type crossRepoDeadCodeConsumerCoverageResult struct {
	Checked     bool
	Unavailable bool
	code.CrossRepoDeadCodeCoverage
}

// incomplete reports whether the check ran and found a consumer repository
// that is not proven complete.
func (c crossRepoDeadCodeConsumerCoverageResult) incomplete() bool {
	return c.Checked && !c.Unavailable && !c.Complete()
}

// summary is the response's consumer_coverage object. It is nil when the check
// did not produce an answer, so a response never claims a coverage it did not
// establish.
//
// incomplete is the per-repository answer (repository_id, state, generation_id,
// retryable); incomplete_repo_ids is the same list's ids, kept for callers that
// read it before the detail existed. retryable is a hint, true only when a
// snapshot is expected for every gap without action: see code.CrossRepoDeadCodeCoverage.Retryable. generation_id
// is left out for a repository with no active scope, which has none. reason and
// next_step are plain-language text derived from state, and coverage_summary is
// one sentence over the listed gaps (#7594).
func (c crossRepoDeadCodeConsumerCoverageResult) summary() map[string]any {
	if !c.Checked || c.Unavailable {
		return nil
	}
	incomplete := make([]map[string]any, 0, len(c.Gaps))
	for _, gap := range c.Gaps {
		entry := map[string]any{
			"repository_id": gap.RepositoryID,
			"state":         gap.State,
			"retryable":     gap.Retryable,
		}
		if gap.GenerationID != "" {
			entry["generation_id"] = gap.GenerationID
		}
		if reason, nextStep := coverageGapText(gap.State); reason != "" {
			entry["reason"] = reason
			entry["next_step"] = nextStep
		}
		incomplete = append(incomplete, entry)
	}
	return map[string]any{
		"complete":             c.Complete(),
		"coverage_summary":     coverageSummary(c.CrossRepoDeadCodeCoverage),
		"retryable":            c.Retryable(),
		"incomplete":           incomplete,
		"incomplete_repo_ids":  c.IncompleteRepositoryIDs(),
		"incomplete_truncated": c.IncompleteTruncated,
	}
}

// crossRepoDeadCodeCoverageRequestFor derives the one coverage statement a
// request needs from the same read plan the evidence page follows, so the two
// can never disagree about which consumers the answer is about.
//
//   - a request that named consumers checks exactly those, and requires each to
//     have an active repository scope: the caller asked for that repository's
//     evidence by name, and a repository with no active generation contributes
//     none;
//   - a scoped caller who named none checks their grant;
//   - an unscoped caller who named none checks every repository.
func crossRepoDeadCodeCoverageRequestFor(
	reads code.CrossRepoDeadCodeConsumerReads,
) code.CrossRepoDeadCodeCoverageRequest {
	if len(reads.PageRepositoryIDs) == 0 {
		return code.CrossRepoDeadCodeCoverageRequest{AllRepositories: true}
	}
	return code.CrossRepoDeadCodeCoverageRequest{
		RepositoryIDs:      reads.PageRepositoryIDs,
		RequireActiveScope: len(reads.SignalGrant) == 0,
	}
}

// crossRepoDeadCodeConsumerCoverage runs the per-request coverage check. It
// runs once, after the candidate scan, and only when there is a candidate to
// classify and the evidence read itself was available: an unavailable evidence
// read already forces every candidate unknown. The check consults the store
// that answered the evidence read; a store that cannot answer it reports
// Unavailable, which the bucketing pass turns into
// cross_repo_evidence_unavailable rather than a covered answer.
func (a *Analyzer) crossRepoDeadCodeConsumerCoverage(
	ctx context.Context,
	candidateCount int,
	consumerRepoIDs []string,
	evidenceAvailable bool,
) (crossRepoDeadCodeConsumerCoverageResult, error) {
	if candidateCount == 0 || !evidenceAvailable {
		return crossRepoDeadCodeConsumerCoverageResult{}, nil
	}
	store, ok := a.deps.Content.(crossRepoDeadCodeCoverageStore)
	if !ok {
		return crossRepoDeadCodeConsumerCoverageResult{Checked: true, Unavailable: true}, nil
	}
	// evidenceAvailable implies the read plan succeeded, so it does here too.
	reads, ok := CrossRepoDeadCodeConsumerReadPlan(a.deps.GrantFilter(ctx), consumerRepoIDs)
	if !ok {
		return crossRepoDeadCodeConsumerCoverageResult{Checked: true, Unavailable: true}, nil
	}
	coverage, err := store.CrossRepoDeadCodeConsumerCoverage(ctx, crossRepoDeadCodeCoverageRequestFor(reads))
	if err != nil {
		return crossRepoDeadCodeConsumerCoverageResult{}, err
	}
	return crossRepoDeadCodeConsumerCoverageResult{
		Checked:                   true,
		CrossRepoDeadCodeCoverage: coverage,
	}, nil
}
