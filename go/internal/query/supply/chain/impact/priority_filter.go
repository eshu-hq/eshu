// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	supplyChainImpactSortFindingID         = "finding_id"
	supplyChainImpactSortPriorityScoreDesc = "priority_score_desc"
	supplyChainImpactSortPriorityScoreAsc  = "priority_score_asc"
)

// PriorityFilter parses and validates the priority_bucket, min_priority_score,
// and sort query params from a findings-list request, returning a request
// error when any value is unsupported.
func PriorityFilter(r *http.Request) (string, int, string, error) {
	bucket := querycontract.QueryParam(r, "priority_bucket")
	if bucket != "" && !ValidPriorityBucket(bucket) {
		return "", 0, "", fmt.Errorf("priority_bucket must be critical, high, medium, low, or informational")
	}
	minScore, err := OptionalMinPriorityScore(r)
	if err != nil {
		return "", 0, "", err
	}
	sort := NormalizeSort(querycontract.QueryParam(r, "sort"))
	if !validSupplyChainImpactSort(sort) {
		return "", 0, "", fmt.Errorf("sort must be finding_id, priority, priority_score_desc, or priority_score_asc")
	}
	return bucket, minScore, sort, nil
}

// OptionalMinPriorityScore parses the optional min_priority_score query
// parameter from r, returning 0 (no floor) when the caller omits it, and an
// error when the raw value is not an integer in [0, 100].
func OptionalMinPriorityScore(r *http.Request) (int, error) {
	raw := querycontract.QueryParam(r, "min_priority_score")
	if raw == "" {
		return 0, nil
	}
	score, err := strconv.Atoi(raw)
	if err != nil || score < 0 || score > 100 {
		return 0, fmt.Errorf("min_priority_score must be between 0 and 100")
	}
	return score, nil
}

// ValidPriorityBucket reports whether bucket is one of the closed
// priority-bucket enumeration values (critical, high, medium, low,
// informational).
func ValidPriorityBucket(bucket string) bool {
	switch bucket {
	case "critical", "high", "medium", "low", "informational":
		return true
	default:
		return false
	}
}

// NormalizeSort trims sort and maps it to its canonical query constant: an
// empty value defaults to the finding-id sort, and "priority" is an alias
// for the descending priority-score sort. Any other value passes through
// trimmed but otherwise unchanged, for PriorityFilter to reject.
func NormalizeSort(sort string) string {
	switch strings.TrimSpace(sort) {
	case "", supplyChainImpactSortFindingID:
		return supplyChainImpactSortFindingID
	case "priority", supplyChainImpactSortPriorityScoreDesc:
		return supplyChainImpactSortPriorityScoreDesc
	case supplyChainImpactSortPriorityScoreAsc:
		return supplyChainImpactSortPriorityScoreAsc
	default:
		return strings.TrimSpace(sort)
	}
}

func validSupplyChainImpactSort(sort string) bool {
	switch sort {
	case supplyChainImpactSortFindingID,
		supplyChainImpactSortPriorityScoreDesc,
		supplyChainImpactSortPriorityScoreAsc:
		return true
	default:
		return false
	}
}
