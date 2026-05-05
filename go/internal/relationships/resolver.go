package relationships

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// DefaultConfidenceThreshold is the minimum confidence for an inferred
// candidate to be promoted to a resolved relationship.
const DefaultConfidenceThreshold = 0.75

// DedupeEvidenceFacts collapses exact duplicate evidence facts while
// preserving discovery order.
func DedupeEvidenceFacts(facts []EvidenceFact) []EvidenceFact {
	if len(facts) == 0 {
		return nil
	}

	type dedupeKey struct {
		RelationshipType RelationshipType
		EvidenceKind     EvidenceKind
		SourceRepoID     string
		TargetRepoID     string
		SourceEntityID   string
		TargetEntityID   string
		Confidence       float64
		Rationale        string
		DetailsJSON      string
	}

	seen := make(map[dedupeKey]struct{}, len(facts))
	deduped := make([]EvidenceFact, 0, len(facts))

	for i := range facts {
		detailsJSON, _ := json.Marshal(facts[i].Details)
		key := dedupeKey{
			RelationshipType: facts[i].RelationshipType,
			EvidenceKind:     facts[i].EvidenceKind,
			SourceRepoID:     facts[i].SourceRepoID,
			TargetRepoID:     facts[i].TargetRepoID,
			SourceEntityID:   facts[i].SourceEntityID,
			TargetEntityID:   facts[i].TargetEntityID,
			Confidence:       facts[i].Confidence,
			Rationale:        facts[i].Rationale,
			DetailsJSON:      string(detailsJSON),
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, facts[i])
	}

	return deduped
}

// Resolve runs the full resolution algorithm: group evidence into candidates,
// apply assertions (rejections and explicit overrides), filter by confidence,
// and return the resulting candidates and resolved relationships.
func Resolve(
	evidenceFacts []EvidenceFact,
	assertions []Assertion,
	confidenceThreshold float64,
) ([]Candidate, []ResolvedRelationship) {
	if confidenceThreshold <= 0 {
		confidenceThreshold = DefaultConfidenceThreshold
	}

	candidates := buildCandidates(evidenceFacts)
	rejections, explicitAssertions := groupAssertions(assertions)

	resolved := make([]ResolvedRelationship, 0, len(candidates))
	for i := range candidates {
		key := entityTriple{
			SourceEntityID:   candidates[i].SourceEntityID,
			TargetEntityID:   candidates[i].TargetEntityID,
			RelationshipType: candidates[i].RelationshipType,
		}
		if _, rejected := rejections[key]; rejected {
			continue
		}
		if candidates[i].Confidence < confidenceThreshold {
			continue
		}
		resolved = append(resolved, candidateToResolved(candidates[i]))
	}

	resolved = applyExplicitAssertions(resolved, explicitAssertions)
	resolved = dedupeResolved(resolved)
	sortResolved(resolved)

	return candidates, resolved
}

// entityTriple is the composite key for grouping evidence and assertions.
type entityTriple struct {
	SourceEntityID   string
	TargetEntityID   string
	RelationshipType RelationshipType
}

// buildCandidates groups evidence facts by (source, target, type) and
// aggregates them into candidates.
func buildCandidates(facts []EvidenceFact) []Candidate {
	type groupKey = entityTriple
	groups := make(map[groupKey][]EvidenceFact)
	order := make([]groupKey, 0)

	for i := range facts {
		srcID := entityIdentity(facts[i].SourceEntityID, facts[i].SourceRepoID)
		tgtID := entityIdentity(facts[i].TargetEntityID, facts[i].TargetRepoID)
		if srcID == "" || tgtID == "" {
			continue
		}
		key := groupKey{
			SourceEntityID:   srcID,
			TargetEntityID:   tgtID,
			RelationshipType: facts[i].RelationshipType,
		}
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], facts[i])
	}

	sort.Slice(order, func(i, j int) bool {
		if order[i].SourceEntityID != order[j].SourceEntityID {
			return order[i].SourceEntityID < order[j].SourceEntityID
		}
		if order[i].TargetEntityID != order[j].TargetEntityID {
			return order[i].TargetEntityID < order[j].TargetEntityID
		}
		return order[i].RelationshipType < order[j].RelationshipType
	})

	candidates := make([]Candidate, 0, len(order))
	for _, key := range order {
		bucket := groups[key]
		candidates = append(candidates, aggregateCandidate(key, bucket))
	}

	return candidates
}

// aggregateCandidate builds a single candidate from a group of evidence facts.
func aggregateCandidate(key entityTriple, facts []EvidenceFact) Candidate {
	maxConf := 0.0
	evidenceKinds := make(map[string]struct{})
	rationales := make([]string, 0)
	seenRationale := make(map[string]struct{})
	var srcRepoID, tgtRepoID string

	preview := make([]map[string]any, 0, min(len(facts), 5))

	for i := range facts {
		if facts[i].Confidence > maxConf {
			maxConf = facts[i].Confidence
		}
		evidenceKinds[string(facts[i].EvidenceKind)] = struct{}{}
		if _, seen := seenRationale[facts[i].Rationale]; !seen && facts[i].Rationale != "" {
			seenRationale[facts[i].Rationale] = struct{}{}
			rationales = append(rationales, facts[i].Rationale)
		}
		if srcRepoID == "" && facts[i].SourceRepoID != "" {
			srcRepoID = facts[i].SourceRepoID
		}
		if tgtRepoID == "" && facts[i].TargetRepoID != "" {
			tgtRepoID = facts[i].TargetRepoID
		}
		if len(preview) < 5 {
			preview = append(preview, map[string]any{
				"kind":       string(facts[i].EvidenceKind),
				"confidence": facts[i].Confidence,
				"details":    facts[i].Details,
			})
		}
	}

	sortedKinds := sortedKeys(evidenceKinds)

	return Candidate{
		SourceRepoID:     srcRepoID,
		TargetRepoID:     tgtRepoID,
		SourceEntityID:   key.SourceEntityID,
		TargetEntityID:   key.TargetEntityID,
		RelationshipType: key.RelationshipType,
		Confidence:       maxConf,
		EvidenceCount:    len(facts),
		Rationale:        strings.Join(rationales, "; "),
		Details: map[string]any{
			"evidence_kinds":   sortedKinds,
			"evidence_preview": preview,
		},
	}
}

// groupAssertions splits assertions into rejections and explicit assertion maps.
func groupAssertions(assertions []Assertion) (
	map[entityTriple]struct{},
	map[entityTriple]Assertion,
) {
	latest := make(map[entityTriple]Assertion)
	for i := range assertions {
		srcID := entityIdentity(assertions[i].SourceEntityID, assertions[i].SourceRepoID)
		tgtID := entityIdentity(assertions[i].TargetEntityID, assertions[i].TargetRepoID)
		if srcID == "" || tgtID == "" {
			continue
		}
		key := entityTriple{
			SourceEntityID:   srcID,
			TargetEntityID:   tgtID,
			RelationshipType: assertions[i].RelationshipType,
		}
		latest[key] = assertions[i]
	}

	rejections := make(map[entityTriple]struct{})
	explicit := make(map[entityTriple]Assertion)
	for key, assertion := range latest {
		switch assertion.Decision {
		case "reject":
			rejections[key] = struct{}{}
		case "assert":
			explicit[key] = assertion
		}
	}

	return rejections, explicit
}

// candidateToResolved promotes an inferred candidate to a resolved relationship.
func candidateToResolved(c Candidate) ResolvedRelationship {
	details := make(map[string]any, len(c.Details))
	for k, v := range c.Details {
		details[k] = v
	}

	return ResolvedRelationship{
		SourceRepoID:     c.SourceRepoID,
		TargetRepoID:     c.TargetRepoID,
		SourceEntityID:   c.SourceEntityID,
		TargetEntityID:   c.TargetEntityID,
		RelationshipType: c.RelationshipType,
		Confidence:       c.Confidence,
		EvidenceCount:    c.EvidenceCount,
		Rationale:        c.Rationale,
		ResolutionSource: ResolutionSourceInferred,
		Details:          details,
	}
}

// applyExplicitAssertions merges explicit assertion overrides into the resolved set.
func applyExplicitAssertions(
	resolved []ResolvedRelationship,
	assertions map[entityTriple]Assertion,
) []ResolvedRelationship {
	existingKeys := make(map[entityTriple]struct{}, len(resolved))
	for i := range resolved {
		key := entityTriple{
			SourceEntityID:   resolved[i].SourceEntityID,
			TargetEntityID:   resolved[i].TargetEntityID,
			RelationshipType: resolved[i].RelationshipType,
		}
		existingKeys[key] = struct{}{}
	}

	sortedAssertionKeys := make([]entityTriple, 0, len(assertions))
	for key := range assertions {
		sortedAssertionKeys = append(sortedAssertionKeys, key)
	}
	sort.Slice(sortedAssertionKeys, func(i, j int) bool {
		a, b := sortedAssertionKeys[i], sortedAssertionKeys[j]
		if a.SourceEntityID != b.SourceEntityID {
			return a.SourceEntityID < b.SourceEntityID
		}
		if a.TargetEntityID != b.TargetEntityID {
			return a.TargetEntityID < b.TargetEntityID
		}
		return a.RelationshipType < b.RelationshipType
	})

	for _, key := range sortedAssertionKeys {
		assertion := assertions[key]
		if _, exists := existingKeys[key]; exists {
			for i := range resolved {
				rKey := entityTriple{
					SourceEntityID:   resolved[i].SourceEntityID,
					TargetEntityID:   resolved[i].TargetEntityID,
					RelationshipType: resolved[i].RelationshipType,
				}
				if rKey != key {
					continue
				}
				details := make(map[string]any, len(resolved[i].Details)+1)
				for k, v := range resolved[i].Details {
					details[k] = v
				}
				details["actor"] = assertion.Actor
				resolved[i] = ResolvedRelationship{
					SourceRepoID:     coalesce(resolved[i].SourceRepoID, assertion.SourceRepoID),
					TargetRepoID:     coalesce(resolved[i].TargetRepoID, assertion.TargetRepoID),
					SourceEntityID:   key.SourceEntityID,
					TargetEntityID:   key.TargetEntityID,
					RelationshipType: assertion.RelationshipType,
					Confidence:       1.0,
					EvidenceCount:    resolved[i].EvidenceCount,
					Rationale:        assertion.Reason,
					ResolutionSource: ResolutionSourceAssertion,
					Details:          details,
				}
			}
			continue
		}
		resolved = append(resolved, ResolvedRelationship{
			SourceRepoID:     assertion.SourceRepoID,
			TargetRepoID:     assertion.TargetRepoID,
			SourceEntityID:   key.SourceEntityID,
			TargetEntityID:   key.TargetEntityID,
			RelationshipType: assertion.RelationshipType,
			Confidence:       1.0,
			EvidenceCount:    0,
			Rationale:        assertion.Reason,
			ResolutionSource: ResolutionSourceAssertion,
			Details:          map[string]any{"actor": assertion.Actor},
		})
	}

	return resolved
}

// dedupeResolved removes exact duplicate resolved relationships.
func dedupeResolved(resolved []ResolvedRelationship) []ResolvedRelationship {
	seen := make(map[string]struct{}, len(resolved))
	deduped := make([]ResolvedRelationship, 0, len(resolved))
	for i := range resolved {
		key := fmt.Sprintf("%s|%s|%s",
			resolved[i].SourceEntityID,
			resolved[i].TargetEntityID,
			resolved[i].RelationshipType,
		)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, resolved[i])
	}
	return deduped
}

// sortResolved sorts resolved relationships by (source, target, type).
func sortResolved(resolved []ResolvedRelationship) {
	sort.Slice(resolved, func(i, j int) bool {
		if resolved[i].SourceEntityID != resolved[j].SourceEntityID {
			return resolved[i].SourceEntityID < resolved[j].SourceEntityID
		}
		if resolved[i].TargetEntityID != resolved[j].TargetEntityID {
			return resolved[i].TargetEntityID < resolved[j].TargetEntityID
		}
		return resolved[i].RelationshipType < resolved[j].RelationshipType
	})
}

func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
