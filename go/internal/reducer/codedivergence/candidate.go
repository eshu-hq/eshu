// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	querycodedivergence "github.com/eshu-hq/eshu/go/internal/query/codedivergence"
)

// ReasonSimilarityBelowThreshold counts candidate pairs the exact-Jaccard
// verification rejected under the ship threshold. The rejection carries the
// measured value (see the similarity evidence atom convention on admitted
// pairs): a rejected pair is counted, never silently dropped.
const ReasonSimilarityBelowThreshold = "similarity_below_threshold"

// ReasonBudgetExhausted counts entities with more nominated partners than
// MaxCandidatesPerEntity: only their top-K pairs verified, the rest dropped
// by budget rather than by evidence.
const ReasonBudgetExhausted = "candidate_budget_exhausted"

// RuleAdmitDrifted is the rule-dimension value for admitted pairs on the
// shared correlation counters: the pair survived verification and every
// suppression rule.
const RuleAdmitDrifted = "admit_drifted"

// DriftedPack is the pack-dimension value identifying code_drifted
// emissions on the shared correlation counters.
const DriftedPack = "code_drifted"

// DriftedKind is the drift-kind-dimension value for admitted drifted pairs.
// The domain has a single finding flavor; the dimension stays for
// counter-shape stability with the sibling drift packs.
const DriftedKind = "drifted"

// Drift evidence types carried on admitted pairs. similarity bears the
// measured Jaccard value (the drift-specific similarity=0.87 reason);
// differs_in_ranges bears the member body ranges the pair differs within
// (the drift-specific differs_in_ranges reason).
const (
	EvidenceTypeSimilarity      = "similarity"
	EvidenceTypeDiffersInRanges = "differs_in_ranges"
)

// MemberRow is one fingerprinted function nominated for drift verification,
// with its decoded shingle set and the member fields the suppression rules
// read. TokenCount below querycodedivergence.TokenFloor drops the pair.
type MemberRow struct {
	EntityID     string
	EntityName   string
	EntityType   string
	RelativePath string
	Language     string
	StartLine    int
	EndLine      int
	TokenCount   int
	Shingles     []uint64
	FPExact      string
	FPRenamed    string
}

// queryMember renders the suppression-rule view of a member row. The rules
// read path, name, language, and token count only — never a body.
func (m MemberRow) queryMember() querycodedivergence.Member {
	return querycodedivergence.Member{
		EntityID:     m.EntityID,
		EntityName:   m.EntityName,
		EntityType:   m.EntityType,
		RelativePath: m.RelativePath,
		Language:     m.Language,
		StartLine:    m.StartLine,
		EndLine:      m.EndLine,
		TokenCount:   m.TokenCount,
	}
}

// CandidatePair is one LSH-nominated pair: two members sharing SharedBands
// LSH bands, nominated for exact-Jaccard verification.
type CandidatePair struct {
	A           MemberRow
	B           MemberRow
	SharedBands int
}

// CandidateStats carries the loader-side pipeline filter counts for one
// repo: rows that never became candidate pairs. They merge into the
// generation suppression totals so no filtering is silent.
type CandidateStats struct {
	// BudgetExhausted lists entity IDs with more partners than
	// MaxCandidatesPerEntity: only their top-K pairs verified.
	BudgetExhausted []string
	// BelowFloor counts fingerprinted rows under the token floor.
	BelowFloor int
	// NoShingles counts full-tier rows with no persisted shingle set
	// (pre-#6837 payloads).
	NoShingles int
	// EqualityDuplicates counts band pairs excluded as equality findings
	// (shared fp_exact or fp_renamed): the #6836 read surface owns them.
	EqualityDuplicates int
}

// CandidatePage is one loader result for a repo: the within-budget pairs
// plus the pipeline filter stats.
type CandidatePage struct {
	Pairs []CandidatePair
	Stats CandidateStats
}

// CandidateLoader supplies the LSH-nominated candidate pairs for one repo.
// The loader enforces the token floor, the shingle presence, the equality
// ownership exclusion, and the per-entity candidate budget in SQL; the
// handler verifies, suppresses, and publishes what the loader returns.
type CandidateLoader interface {
	LoadCandidates(ctx context.Context, repoID string) (CandidatePage, error)
}

// AdmittedPair is a candidate pair that verified at/above threshold with
// clean members, carrying the drift evidence atoms the writer persists.
type AdmittedPair struct {
	Pair       CandidatePair
	Similarity float64
	Evidence   []model.EvidenceAtom
}

// DriftedFindingID derives the stable pair identity from (repo_id, ordered
// entity pair) so a finding keeps its id across generations and loader row
// order while the pair exists. Orientation-independent: member order never
// duplicates a row.
func DriftedFindingID(repoID string, a, b MemberRow) string {
	lo, hi := a.EntityID, b.EntityID
	if hi < lo {
		lo, hi = hi, lo
	}
	sum := sha256.Sum256([]byte(repoID + "\x00" + lo + "\x00" + hi))
	return hex.EncodeToString(sum[:])[:16]
}

// ApplyRules verifies one candidate pair and runs the intentional-parallel
// suppression catalogue over it, reusing the per-rule functions and rule
// names from #6836. It returns the admitted pair, or suppressed=true with
// the counting reason: below_floor, a member rule name, or
// similarity_below_threshold. A suppressed pair is counted by the caller,
// never silently dropped.
//
// Rule order is load-bearing: structural drops (floor) precede member rules,
// verification precedes threshold. The wrapper-family group rule does not
// apply to pairs: it needs WrapperFamilyMinMembers same-name members and a
// pair is below that threshold by construction, so same-name pairs (the
// highest-value drift shape, e.g. Scan drift across services) keep reporting
// here while the #6836 surface owns the group call.
func ApplyRules(pair CandidatePair) (AdmittedPair, bool, string) {
	for _, m := range []MemberRow{pair.A, pair.B} {
		if m.TokenCount < querycodedivergence.TokenFloor {
			return AdmittedPair{}, true, querycodedivergence.RuleBelowFloor
		}
		if reason, suppressed := memberRule(m); suppressed {
			return AdmittedPair{}, true, reason
		}
	}
	similarity := Jaccard(pair.A.Shingles, pair.B.Shingles)
	if similarity < DriftedSimilarityThreshold {
		return AdmittedPair{}, true, ReasonSimilarityBelowThreshold
	}
	return AdmittedPair{
		Pair:       pair,
		Similarity: similarity,
		Evidence:   driftEvidence(pair, similarity),
	}, false, ""
}

// memberRule runs the member-level suppression functions over one member,
// returning the first tripped rule name. Test files suppress unconditionally
// at write: the materialized drifted kind has no read-time include_tests
// resurrection, matching the #6834 coincidence catalogue (test-idiom
// skeletons suppress).
func memberRule(m MemberRow) (string, bool) {
	qm := m.queryMember()
	switch {
	case querycodedivergence.SuppressGenerated(qm):
		return querycodedivergence.RuleGenerated, true
	case querycodedivergence.SuppressVendored(qm):
		return querycodedivergence.RuleVendored, true
	case querycodedivergence.SuppressTestFile(qm, false):
		return querycodedivergence.RuleTestFile, true
	case querycodedivergence.SuppressTrivialAccessor(qm):
		return querycodedivergence.RuleTrivialAccessor, true
	default:
		return "", false
	}
}

// driftEvidence builds the drift-specific reason atoms for an admitted pair:
// the measured similarity value and the member body ranges the pair differs
// within (function granularity: shingle sets prove overlap below identity
// but cannot localize lines without source, which the grouping path must
// never read).
func driftEvidence(pair CandidatePair, similarity float64) []model.EvidenceAtom {
	return []model.EvidenceAtom{
		{
			EvidenceType: EvidenceTypeSimilarity,
			Key:          EvidenceTypeSimilarity,
			Value:        fmt.Sprintf("%.4f", similarity),
		},
		{
			EvidenceType: EvidenceTypeDiffersInRanges,
			Key:          EvidenceTypeDiffersInRanges,
			Value: fmt.Sprintf("%s:%d-%d vs %s:%d-%d",
				pair.A.RelativePath, pair.A.StartLine, pair.A.EndLine,
				pair.B.RelativePath, pair.B.StartLine, pair.B.EndLine),
		},
	}
}
