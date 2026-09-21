// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"fmt"
)

// KindDrifted groups exactly the pair the #6837 reducer admitted: two
// function bodies whose renamed 5-shingle sets verify at Jaccard >=
// DriftedSimilarityThreshold (0.7) but share no equality fingerprint, so
// neither the exact nor the renamed read surface claims them. Drifted rows
// arrive as reducer_code_drifted_finding facts, not fingerprint groups.
const KindDrifted Kind = "parallel_implementation.drifted"

// ReasonDriftedPair is the value-bearing reason for a drifted pair: the
// Jaccard similarity over the pair's shingle sets with the admitting
// threshold in the sentence. One per member, so reasons still sum to the
// members x tokens score without remainder.
const ReasonDriftedPair = "drifted_pair_jaccard"

// DriftedRow is one active reducer_code_drifted_finding fact as the read
// surface sees it: the writer finding id (stable across generations while
// the pair exists), the measured similarity with its admitting threshold,
// the LSH band evidence that nominated the pair, and the two members.
type DriftedRow struct {
	FindingID   string
	Similarity  float64
	Threshold   float64
	SharedBands int
	Members     []Member
}

// AssembleDriftedFinding builds the finding for one drifted row, applying
// the same member-level suppression catalogue as the equality kinds so a
// test-file copy suppresses by default here too. It reports false when
// fewer than two members survive: a single surviving copy is not a parallel
// implementation. Suppression counts report even on a drop, so a quiet
// result stays distinguishable from a filtered one.
//
// The finding id derives from (repo_id, kind, fingerprint) like every other
// kind, so stat order and finding order agree exactly on score ties; the
// writer finding id rides as the fingerprint for cross-generation
// continuity. The pass-level suppression counts the reducer wrote into the
// fact payload stay in the payload: attributing them per finding would
// multiply-count them across the page, and the reducer already emits them
// through its own counters.
func AssembleDriftedFinding(repoID string, row DriftedRow, includeTests bool) (Finding, bool) {
	survivors, suppressions := suppressMembers(row.Members, includeTests)
	partial := Finding{
		ID:           findingID(repoID, KindDrifted, row.FindingID),
		RepoID:       repoID,
		Kind:         KindDrifted,
		Fingerprint:  row.FindingID,
		Suppressions: suppressions,
	}
	if len(survivors) < 2 {
		return partial, false
	}
	tokenCount := 0
	for _, member := range survivors {
		if member.TokenCount > tokenCount {
			tokenCount = member.TokenCount
		}
	}
	return Finding{
		ID:           partial.ID,
		RepoID:       repoID,
		Kind:         KindDrifted,
		Fingerprint:  row.FindingID,
		Members:      survivors,
		Reasons:      buildDriftedReasons(row, len(survivors), tokenCount, survivors),
		Score:        len(survivors) * tokenCount,
		Suppressions: suppressions,
	}, true
}

// buildDriftedReasons decomposes members x tokens without remainder: one
// Jaccard reason per member carrying the measured similarity and threshold,
// then the same zero-weight package-span and large-body signals the
// equality kinds report.
func buildDriftedReasons(row DriftedRow, memberCount, tokenCount int, members []Member) []Reason {
	reasons := make([]Reason, 0, memberCount+2)
	for i := 0; i < memberCount; i++ {
		role := fmt.Sprintf("drifted pair shares Jaccard %.2f over shingle sets at or above the %.2f threshold (%d shared LSH bands) across %d members (max %d tokens)", row.Similarity, row.Threshold, row.SharedBands, memberCount, tokenCount)
		if i > 0 {
			role = fmt.Sprintf("additional drifted copy %d of %d (%.2f Jaccard over %d shared bands, %d tokens)", i+1, memberCount, row.Similarity, row.SharedBands, tokenCount)
		}
		reasons = append(reasons, Reason{Code: ReasonDriftedPair, Sentence: role, Value: tokenCount})
	}
	packages := map[string]struct{}{}
	for _, member := range members {
		packages[PackageOf(member.RelativePath)] = struct{}{}
	}
	if len(packages) > 1 {
		reasons = append(reasons, Reason{
			Code:     ReasonSpanPackages,
			Sentence: fmt.Sprintf("members span %d packages (ranking signal, no score weight)", len(packages)),
			Value:    0,
		})
	}
	if tokenCount >= LargeBodyTokens {
		reasons = append(reasons, Reason{
			Code:     ReasonLargeBody,
			Sentence: fmt.Sprintf("body holds %d tokens at or above the %d-token large-body mark (judgment signal, no score weight)", tokenCount, LargeBodyTokens),
			Value:    0,
		})
	}
	return reasons
}
