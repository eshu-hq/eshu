// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	// crossRepoDeadCodeHandleGroupLimit caps the group objects one row ships
	// under evidence_detail handles. The group that decided the row's bucket
	// sorts first, so the cap can only cut weaker groups, and a row that lost
	// groups says so (consumer_evidence_handles_truncated).
	crossRepoDeadCodeHandleGroupLimit = 5
	// crossRepoDeadCodeBoundaryHandleLimit caps the repository-boundary items
	// shipped once under handles. data.boundary_consumer_evidence_count keeps
	// the total.
	crossRepoDeadCodeBoundaryHandleLimit = 25
)

// ErrInvalidCrossRepoDeadCodeEvidenceDetail is wrapped by the request
// normalizer when evidence_detail is neither full nor handles. The handler
// answers it as HTTP 400.
var ErrInvalidCrossRepoDeadCodeEvidenceDetail = errors.New("invalid cross-repo dead-code evidence_detail")

// normalizeCrossRepoDeadCodeEvidenceDetail returns the detail mode for a
// request value: an absent value is full (the HTTP default; the MCP adapter
// sends handles explicitly), and anything else must be full or handles. It uses
// the same two strings as the context routes so one vocabulary covers every
// evidence_detail argument.
func normalizeCrossRepoDeadCodeEvidenceDetail(detail string) (string, error) {
	switch detail {
	case "", querycontract.ContextEvidenceDetailFull:
		return querycontract.ContextEvidenceDetailFull, nil
	case querycontract.ContextEvidenceDetailHandles:
		return detail, nil
	default:
		return "", fmt.Errorf("%w: %q is not one of %s", ErrInvalidCrossRepoDeadCodeEvidenceDetail, detail,
			strings.Join([]string{querycontract.ContextEvidenceDetailFull, querycontract.ContextEvidenceDetailHandles}, ", "))
	}
}

// crossRepoDeadCodeEvidenceDetail is the shaped evidence a response carries:
// the candidate buckets, the data keys that describe the boundary list and the
// detail mode, and the truth omissions the shaping owes the caller.
type crossRepoDeadCodeEvidenceDetail struct {
	Buckets   map[string]any
	Data      map[string]any
	Omissions []querycontract.TruthOmission
}

// shapeCrossRepoDeadCodeEvidence projects the consumer evidence of bucketed
// rows and the hoisted boundary list for the requested detail mode. It runs
// after classification, on its output, so buckets, reasons, hidden counts,
// bucket_counts, and analysis are computed from the full in-memory evidence and
// are identical in both modes.
//
// full returns the buckets untouched. handles replaces each row's
// consumer_evidence with at most crossRepoDeadCodeHandleGroupLimit group
// objects and the boundary list with at most crossRepoDeadCodeBoundaryHandleLimit
// projected items. Rows are copied before they change, so a map the bucketing
// pass shares is never mutated. Every cut carries its count and a marker.
func shapeCrossRepoDeadCodeEvidence(
	buckets map[string]any,
	boundary []CrossRepoDeadCodeEvidence,
	detail string,
) crossRepoDeadCodeEvidenceDetail {
	shaped := crossRepoDeadCodeEvidenceDetail{
		Buckets: buckets,
		Data: map[string]any{
			"evidence_detail":                  detail,
			"boundary_consumer_evidence":       crossRepoDeadCodeEvidenceMaps(boundary),
			"boundary_consumer_evidence_count": len(boundary),
		},
	}
	if detail != querycontract.ContextEvidenceDetailHandles {
		return shaped
	}

	shaped.Buckets = make(map[string]any, len(buckets))
	for name, raw := range buckets {
		shaped.Buckets[name] = raw
	}
	rowItems := 0
	for _, name := range []string{"dead", "live_by_consumer", "unknown"} {
		rows, _ := buckets[name].([]any)
		projected := make([]any, 0, len(rows))
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				projected = append(projected, raw)
				continue
			}
			rowItems += querycontract.IntVal(row, "consumer_evidence_count")
			projected = append(projected, projectCrossRepoDeadCodeRowHandles(row))
		}
		shaped.Buckets[name] = projected
	}

	boundaryHandles := crossRepoDeadCodeBoundaryHandles(boundary)
	shaped.Data["boundary_consumer_evidence"] = boundaryHandles
	if len(boundaryHandles) < len(boundary) {
		shaped.Data["boundary_consumer_evidence_truncated"] = true
	}
	if rowItems > 0 {
		shaped.Omissions = append(shaped.Omissions, querycontract.TruthOmission{
			Section: "candidate_buckets.consumer_evidence",
			Detail:  querycontract.ContextEvidenceDetailHandles,
			Total:   rowItems,
		})
	}
	if len(boundary) > 0 {
		shaped.Omissions = append(shaped.Omissions, querycontract.TruthOmission{
			Section: "boundary_consumer_evidence",
			Detail:  querycontract.ContextEvidenceDetailHandles,
			Total:   len(boundary),
		})
	}
	if len(shaped.Omissions) > 0 {
		shaped.Data["evidence_detail_drilldown"] = map[string]any{
			"full_rows": "repeat with evidence_detail full; narrow with consumer_repo_ids and limit",
		}
	}
	return shaped
}

// crossRepoDeadCodeHandleGroup is one (consumer repository, relationship type,
// evidence family) group of a row's evidence.
type crossRepoDeadCodeHandleGroup struct {
	consumerRepoID   string
	relationshipType string
	evidenceFamily   string
	confidenceLabel  string
	confidence       float64
	itemCount        int
}

func (g crossRepoDeadCodeHandleGroup) handle() map[string]any {
	return map[string]any{
		"consumer_repo_id":  g.consumerRepoID,
		"relationship_type": g.relationshipType,
		"evidence_family":   g.evidenceFamily,
		"confidence_label":  g.confidenceLabel,
		"item_count":        g.itemCount,
	}
}

// compareCrossRepoDeadCodeHandleGroups orders groups strongest first: highest
// confidence, then most items, then the identity keys ascending so the order
// is deterministic. The group holding the row's strongest item therefore leads
// and is the last one any cap can reach.
func compareCrossRepoDeadCodeHandleGroups(a, b crossRepoDeadCodeHandleGroup) int {
	switch {
	case a.confidence != b.confidence:
		if a.confidence > b.confidence {
			return -1
		}
		return 1
	case a.itemCount != b.itemCount:
		return b.itemCount - a.itemCount
	}
	if c := strings.Compare(a.consumerRepoID, b.consumerRepoID); c != 0 {
		return c
	}
	if c := strings.Compare(a.relationshipType, b.relationshipType); c != 0 {
		return c
	}
	return strings.Compare(a.evidenceFamily, b.evidenceFamily)
}

// projectCrossRepoDeadCodeRowHandles returns a copy of one bucketed row whose
// consumer_evidence is replaced by capped group objects. The row's
// consumer_evidence_count (the items it held) is left as classification set it.
func projectCrossRepoDeadCodeRowHandles(row map[string]any) map[string]any {
	items, _ := row["consumer_evidence"].([]any)
	type groupKey struct{ repo, relationship, family string }
	index := make(map[groupKey]int, len(items))
	groups := make([]crossRepoDeadCodeHandleGroup, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key := groupKey{
			repo:         querycontract.StringVal(item, "consumer_repo_id"),
			relationship: querycontract.StringVal(item, "relationship_type"),
			family:       querycontract.StringVal(item, "evidence_family"),
		}
		confidence := querycontract.FloatVal(item, "confidence")
		position, seen := index[key]
		if !seen {
			index[key] = len(groups)
			groups = append(groups, crossRepoDeadCodeHandleGroup{
				consumerRepoID: key.repo, relationshipType: key.relationship, evidenceFamily: key.family,
				confidence: confidence, confidenceLabel: querycontract.StringVal(item, "confidence_label"), itemCount: 1,
			})
			continue
		}
		group := &groups[position]
		group.itemCount++
		if confidence > group.confidence {
			group.confidence = confidence
			group.confidenceLabel = querycontract.StringVal(item, "confidence_label")
		}
	}
	slices.SortStableFunc(groups, compareCrossRepoDeadCodeHandleGroups)

	clone := make(map[string]any, len(row)+2)
	for key, value := range row {
		clone[key] = value
	}
	shipped := min(len(groups), crossRepoDeadCodeHandleGroupLimit)
	handles := make([]any, 0, shipped)
	for _, group := range groups[:shipped] {
		handles = append(handles, group.handle())
	}
	clone["consumer_evidence"] = handles
	clone["consumer_evidence_group_count"] = len(groups)
	if len(groups) > shipped {
		clone["consumer_evidence_handles_truncated"] = true
	}
	return clone
}

// crossRepoDeadCodeBoundaryHandles projects the boundary list to the same
// group objects, one per item, strongest first, capped at
// crossRepoDeadCodeBoundaryHandleLimit. The caller keeps the total.
func crossRepoDeadCodeBoundaryHandles(boundary []CrossRepoDeadCodeEvidence) []any {
	groups := make([]crossRepoDeadCodeHandleGroup, 0, len(boundary))
	for _, item := range boundary {
		label := item.ConfidenceLabel
		if label == "" {
			label = CrossRepoDeadCodeConfidenceLabel(item.Confidence)
		}
		family := item.EvidenceFamily
		if family == "" {
			family = "code_reachability"
		}
		groups = append(groups, crossRepoDeadCodeHandleGroup{
			consumerRepoID: item.ConsumerRepoID, relationshipType: item.RelationshipType, evidenceFamily: family,
			confidence: item.Confidence, confidenceLabel: label, itemCount: 1,
		})
	}
	slices.SortStableFunc(groups, compareCrossRepoDeadCodeHandleGroups)
	shipped := min(len(groups), crossRepoDeadCodeBoundaryHandleLimit)
	handles := make([]any, 0, shipped)
	for _, group := range groups[:shipped] {
		handles = append(handles, group.handle())
	}
	return handles
}
