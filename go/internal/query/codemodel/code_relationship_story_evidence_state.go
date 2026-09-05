// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"errors"
	"fmt"
	"strings"
)

// Evidence-state classification for relationship-story answers (issue #3158).
//
// These fields make code-relationship uncertainty explicit instead of leaving a
// caller to guess why a result is empty or short: a resolved target with no
// edges reads differently from a target that did not resolve, a confidence
// floor that removed every row, or a result capped by limit or token budget.
// The classification is descriptive only — it never changes the answer's
// TruthEnvelope and never upgrades a heuristic or unsupported edge into
// canonical truth.

// The evidence reason and truncation vocabularies below are exported
// because the staying story shaper and evidence tests classify through
// them via the root forwards.
const (
	// RelationshipStoryReasonComplete marks a complete story result.
	RelationshipStoryReasonComplete = "complete"
	// RelationshipStoryReasonTargetUnresolved marks an unresolved target.
	RelationshipStoryReasonTargetUnresolved = "target_unresolved"
	// RelationshipStoryReasonNoEdges marks a resolved target with no edges.
	RelationshipStoryReasonNoEdges = "no_relationships_found"
	// RelationshipStoryReasonFloorFiltered marks a floor-emptied result.
	RelationshipStoryReasonFloorFiltered = "all_below_confidence_floor"
	// RelationshipStoryReasonTruncatedLimit marks a count-truncated result.
	RelationshipStoryReasonTruncatedLimit = "truncated_by_limit"
	// RelationshipStoryReasonTruncatedBudget marks a budget-truncated result.
	RelationshipStoryReasonTruncatedBudget = "truncated_by_token_budget"

	// RelationshipStoryTruncationNone marks no truncation.
	RelationshipStoryTruncationNone = "none"
	// RelationshipStoryTruncationCount marks count truncation.
	RelationshipStoryTruncationCount = "count"
	// RelationshipStoryTruncationBudget marks token-budget truncation.
	RelationshipStoryTruncationBudget = "token_budget"
	// RelationshipStoryTruncationBoth marks count and budget truncation.
	RelationshipStoryTruncationBoth = "count_and_token_budget"
)

// RelationshipStoryEvidenceInputs carries the counts and flags needed to
// explain why a relationship-story result is complete, empty, filtered, or
// truncated. It is exported because the staying story shaper and evidence
// tests construct it across the boundary.
type RelationshipStoryEvidenceInputs struct {
	ResolutionStatus string
	RawCount         int
	AfterFloorCount  int
	FloorApplied     bool
	CountTruncated   bool
	BudgetTruncated  bool
	// RawPaged is true when the underlying fetch returned a bounded page that did
	// not exhaust the edge set (the graph reader caps at normalizedLimit()+1).
	// When the page was floor-emptied this prevents claiming an exhaustive
	// all_below_confidence_floor result over a partial page.
	RawPaged bool
}

// relationshipStoryTargetResolved reports whether a resolution status means the
// target was found well enough to read relationships. The repo-scoped override
// story sets "repo_scoped" and returns real rows, so it is resolved-equivalent;
// only genuinely unresolved statuses (ambiguous, not_found, content-fallback)
// are treated as unresolved.
func relationshipStoryTargetResolved(status string) bool {
	switch status {
	case "", "resolved", "repo_scoped":
		return true
	default:
		return false
	}
}

// RelationshipStoryEvidenceState is the classified missing-edge reason,
// truncation state, and a bounded human explanation. It is exported because
// the staying story shaper and evidence tests read it across the boundary.
type RelationshipStoryEvidenceState struct {
	Reason      string
	Truncation  string
	Explanation string
}

// ClassifyRelationshipStoryEvidence classifies the result. Reason priority is
// fixed: an unresolved target and an empty graph are reported before truncation,
// because truncation of an already-empty result would be misleading.
//
// A floor that empties the page is reported as all_below_confidence_floor only
// when the page was the complete edge set (!rawPaged). When the fetch was paged,
// a later page may hold a qualifying edge, so the honest reason is truncation,
// not an exhaustive floor verdict.
func ClassifyRelationshipStoryEvidence(in RelationshipStoryEvidenceInputs) RelationshipStoryEvidenceState {
	// A paged raw fetch means more edges existed than were returned, which is a
	// count truncation even when the post-floor row count fits under the limit.
	countTruncated := in.CountTruncated || in.RawPaged
	truncation := relationshipStoryTruncationState(countTruncated, in.BudgetTruncated)
	switch {
	case !relationshipStoryTargetResolved(in.ResolutionStatus) && in.RawCount == 0:
		return RelationshipStoryEvidenceState{
			Reason:      RelationshipStoryReasonTargetUnresolved,
			Truncation:  truncation,
			Explanation: "the target did not resolve to a known entity, so no relationships could be read",
		}
	case in.RawCount == 0:
		return RelationshipStoryEvidenceState{
			Reason:      RelationshipStoryReasonNoEdges,
			Truncation:  truncation,
			Explanation: "the target resolved but has no relationships of the requested type and direction",
		}
	case in.FloorApplied && in.AfterFloorCount == 0 && !in.RawPaged:
		return RelationshipStoryEvidenceState{
			Reason:      RelationshipStoryReasonFloorFiltered,
			Truncation:  truncation,
			Explanation: "relationships exist but all fell below the requested min_confidence floor",
		}
	case countTruncated:
		return RelationshipStoryEvidenceState{
			Reason:      RelationshipStoryReasonTruncatedLimit,
			Truncation:  truncation,
			Explanation: "more relationships exist than the limit; raise limit or page with offset to evaluate them, including against any min_confidence floor",
		}
	case in.BudgetTruncated:
		return RelationshipStoryEvidenceState{
			Reason:      RelationshipStoryReasonTruncatedBudget,
			Truncation:  truncation,
			Explanation: "results were trimmed to fit token_budget; raise token_budget or narrow the query",
		}
	default:
		return RelationshipStoryEvidenceState{
			Reason:      RelationshipStoryReasonComplete,
			Truncation:  truncation,
			Explanation: "all matching relationships were returned",
		}
	}
}

// RelationshipStoryRowsAboveConfidenceFloor drops rows below an optional
// min_confidence floor. It filters the response only; it never changes the
// answer's canonical truth (ADR #2222).
func RelationshipStoryRowsAboveConfidenceFloor(rows []map[string]any, req RelationshipStoryRequest) []map[string]any {
	if req.MinConfidence == nil || *req.MinConfidence <= 0 {
		return rows
	}
	floor := *req.MinConfidence
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		confidence, ok := relationshipStoryNumericConfidence(row)
		if ok && confidence >= floor {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// relationshipStoryNumericConfidence reads a row's numeric confidence, returning
// false when the edge carries none (a legacy or unsupported edge), so callers
// never coerce a missing confidence into 0.
func relationshipStoryNumericConfidence(row map[string]any) (float64, bool) {
	value, ok := row["confidence"]
	if !ok || value == nil {
		return 0, false
	}
	switch confidence := value.(type) {
	case float64:
		return confidence, true
	case float32:
		return float64(confidence), true
	case int:
		return float64(confidence), true
	case int64:
		return float64(confidence), true
	default:
		return 0, false
	}
}

func relationshipStoryTruncationState(countTruncated, budgetTruncated bool) string {
	switch {
	case countTruncated && budgetTruncated:
		return RelationshipStoryTruncationBoth
	case countTruncated:
		return RelationshipStoryTruncationCount
	case budgetTruncated:
		return RelationshipStoryTruncationBudget
	default:
		return RelationshipStoryTruncationNone
	}
}

// The relationship-story query contract below split here from root
// code_relationship_story.go and code_relationship_story_budget.go (#6060
// lane A L1): the evidence-state classifier above takes the request, and
// Go requires a type's methods to live with its declaration, so the
// request's methods move with it — including normalizedRelationshipTypes
// and normalizedTokenBudget from the staying budget file, and
// relationshipStorySupportedType which only they use. The *CodeHandler
// route methods stay in root; root's family_code_shim.go aliases the types
// back so the staying handlers, resolvers, and tests keep their names. The
// methods are exported because staying root callers use them; target()
// became EffectiveTarget because the struct already has a Target field.
// GraphAnchorProperty and GraphAnchorPropertyResolved are exported
// request-internal NornicDB anchor state (never decoded: json:"-").
const (
	relationshipStoryDefaultLimit = 25
	relationshipStoryMaxLimit     = 200
	relationshipStoryMaxOffset    = 10000
)

// RelationshipStoryRequest is the decoded relationship-story lookup
// request. GraphAnchorProperty and GraphAnchorPropertyResolved are
// request-internal NornicDB anchor state set by the resolvers, never
// decoded from the body.
type RelationshipStoryRequest struct {
	QueryType         string `json:"query_type"`
	Target            string `json:"target"`
	Name              string `json:"name"`
	EntityID          string `json:"entity_id"`
	RepoID            string `json:"repo_id"`
	Language          string `json:"language"`
	CrossRepo         bool   `json:"cross_repo"`
	Direction         string `json:"direction"`
	RelationshipType  string `json:"relationship_type"`
	IncludeTransitive bool   `json:"include_transitive"`
	MaxDepth          int    `json:"max_depth"`
	Limit             int    `json:"limit"`
	Offset            int    `json:"offset"`
	// MinConfidence is an optional response-only floor. A nil floor preserves
	// legacy and low-confidence rows; a positive floor keeps only rows with a
	// numeric confidence at or above the threshold.
	MinConfidence *float64 `json:"min_confidence"`
	// RelationshipTypes is an optional additive multi-type filter. When set it
	// supersedes RelationshipType: each requested type is followed with the same
	// bounded single-type query and the results are merged. It applies only to
	// direct (non-transitive) relationship lookups.
	RelationshipTypes []string `json:"relationship_types"`
	// TokenBudget optionally caps the response by an estimated serialized token
	// cost. Zero or absent means no budget. It is a second, tighter bound applied
	// after the count limit so an agent can cap prompt cost; cuts are reported
	// with guidance to narrow.
	TokenBudget int `json:"token_budget"`
	// graphAnchorProperty records the single identity property selected
	// for this resolved NornicDB target. It is request-internal and reused by
	// every requested relationship type and direction.
	GraphAnchorProperty string `json:"-"`
	// graphAnchorPropertyResolved distinguishes a confirmed missing graph anchor
	// from an unresolved/ambiguous uid-id collision that must retain the legacy
	// per-query fallback behavior.
	GraphAnchorPropertyResolved bool `json:"-"`
}

// RelationshipStoryResolution is the name-target resolution outcome: one
// resolved entity, or the ambiguous candidates for the caller to pick from.
type RelationshipStoryResolution struct {
	Status     string           `json:"status"`
	Target     string           `json:"target,omitempty"`
	EntityID   string           `json:"entity_id,omitempty"`
	Name       string           `json:"name,omitempty"`
	RepoID     string           `json:"repo_id,omitempty"`
	Language   string           `json:"language,omitempty"`
	Candidates []map[string]any `json:"candidates,omitempty"`
	Truncated  bool             `json:"truncated,omitempty"`
}

// Validate rejects an unbounded or malformed story request before any
// graph read runs.
func (r RelationshipStoryRequest) Validate() error {
	if strings.TrimSpace(r.EntityID) == "" && strings.TrimSpace(r.EffectiveTarget()) == "" && !r.IsRepoScopedOverrideStory() {
		return errors.New("entity_id or target is required")
	}
	if r.CrossRepo && strings.TrimSpace(r.RepoID) == "" {
		return errors.New("cross_repo relationship story requires repo_id")
	}
	if r.CrossRepo && r.NormalizedQueryType() == "class_hierarchy" {
		return errors.New("cross_repo class_hierarchy enrichment is not supported; use relationship_type INHERITS")
	}
	if r.CrossRepo && r.NormalizedQueryType() == "overrides" {
		return errors.New("cross_repo overrides enrichment is not supported; use relationship_type OVERRIDES")
	}
	if r.Offset < 0 {
		return errors.New("offset must be >= 0")
	}
	if r.Offset > relationshipStoryMaxOffset {
		return errors.New("offset must be <= 10000")
	}
	if _, err := r.NormalizedDirection(); err != nil {
		return err
	}
	if _, err := r.NormalizedRelationshipType(); err != nil {
		return err
	}
	if r.TokenBudget < 0 {
		return errors.New("token_budget must be >= 0")
	}
	if r.MinConfidence != nil && (*r.MinConfidence < 0 || *r.MinConfidence > 1) {
		return errors.New("min_confidence must be between 0 and 1")
	}
	if _, err := r.NormalizedRelationshipTypes(); err != nil {
		return err
	}
	if len(r.RelationshipTypes) > 0 {
		if r.IncludeTransitive {
			return errors.New("relationship_types cannot be combined with include_transitive")
		}
		switch r.NormalizedQueryType() {
		case "class_hierarchy", "overrides":
			return errors.New("relationship_types cannot be combined with class_hierarchy or overrides query types")
		}
	}
	if r.IncludeTransitive {
		if r.Offset != 0 {
			return errors.New("include_transitive requires offset 0")
		}
		if relationshipType, _ := r.NormalizedRelationshipType(); relationshipType != "CALLS" {
			return errors.New("include_transitive currently supports CALLS relationships only")
		}
		if direction, _ := r.NormalizedDirection(); direction == "both" {
			return errors.New("set direction to incoming or outgoing when include_transitive is true")
		}
	}
	return nil
}

// NormalizedQueryType lowercases the requested story family.
func (r RelationshipStoryRequest) NormalizedQueryType() string {
	return strings.ToLower(strings.TrimSpace(r.QueryType))
}

// IsRepoScopedOverrideStory reports whether the request is the
// repo-scoped overrides story, which resolves without an entity target.
func (r RelationshipStoryRequest) IsRepoScopedOverrideStory() bool {
	return r.NormalizedQueryType() == "overrides" &&
		strings.TrimSpace(r.EntityID) == "" &&
		strings.TrimSpace(r.EffectiveTarget()) == ""
}

// EffectiveTarget returns the explicit target or falls back to the
// name under investigation.
func (r RelationshipStoryRequest) EffectiveTarget() string {
	if target := strings.TrimSpace(r.Target); target != "" {
		return target
	}
	return strings.TrimSpace(r.Name)
}

// NormalizedLimit clamps the requested page limit to the story bounds.
func (r RelationshipStoryRequest) NormalizedLimit() int {
	switch {
	case r.Limit <= 0:
		return relationshipStoryDefaultLimit
	case r.Limit > relationshipStoryMaxLimit:
		return relationshipStoryMaxLimit
	default:
		return r.Limit
	}
}

// NormalizedDirection validates the requested edge direction,
// defaulting to both.
func (r RelationshipStoryRequest) NormalizedDirection() (string, error) {
	switch direction := strings.ToLower(strings.TrimSpace(r.Direction)); direction {
	case "":
		return "both", nil
	case "incoming", "outgoing", "both":
		return direction, nil
	default:
		return "", errors.New("direction must be incoming, outgoing, or both")
	}
}

// NormalizedRelationshipType validates the single requested edge type,
// defaulting by query family.
func (r RelationshipStoryRequest) NormalizedRelationshipType() (string, error) {
	relationshipType := strings.ToUpper(strings.TrimSpace(r.RelationshipType))
	if relationshipType == "" {
		switch r.NormalizedQueryType() {
		case "class_hierarchy":
			return "INHERITS", nil
		case "overrides":
			return "OVERRIDES", nil
		}
		return "CALLS", nil
	}
	if !relationshipStorySupportedType(relationshipType) {
		return "", fmt.Errorf("relationship_type %q is not supported", strings.TrimSpace(r.RelationshipType))
	}
	return relationshipType, nil
}

// relationshipStorySupportedType reports whether t is a relationship type the
// bounded relationship-story query path can follow.
func relationshipStorySupportedType(t string) bool {
	switch t {
	case "CALLS", "IMPORTS", "REFERENCES", "INHERITS", "OVERRIDES", "TAINT_FLOWS_TO":
		return true
	default:
		return false
	}
}

// NormalizedRelationshipTypes returns the effective, validated,
// de-duplicated set of edge types to follow.
func (r RelationshipStoryRequest) NormalizedRelationshipTypes() ([]string, error) {
	if len(r.RelationshipTypes) == 0 {
		single, err := r.NormalizedRelationshipType()
		if err != nil {
			return nil, err
		}
		return []string{single}, nil
	}
	seen := make(map[string]struct{}, len(r.RelationshipTypes))
	out := make([]string, 0, len(r.RelationshipTypes))
	for _, raw := range r.RelationshipTypes {
		relationshipType := strings.ToUpper(strings.TrimSpace(raw))
		if relationshipType == "" {
			continue
		}
		if !relationshipStorySupportedType(relationshipType) {
			return nil, fmt.Errorf("relationship_types entry %q is not supported", strings.TrimSpace(raw))
		}
		if _, ok := seen[relationshipType]; ok {
			continue
		}
		seen[relationshipType] = struct{}{}
		out = append(out, relationshipType)
	}
	if len(out) == 0 {
		single, err := r.NormalizedRelationshipType()
		if err != nil {
			return nil, err
		}
		return []string{single}, nil
	}
	return out, nil
}

// zero values as "no budget".
// NormalizedTokenBudget returns the effective token budget, treating
// negative or zero values as "no budget".
func (r RelationshipStoryRequest) NormalizedTokenBudget() int {
	if r.TokenBudget < 0 {
		return 0
	}
	return r.TokenBudget
}
