// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const (
	supplyChainImpactSortFindingID         = "finding_id"
	supplyChainImpactSortPriorityScoreDesc = "priority_score_desc"
	supplyChainImpactSortPriorityScoreAsc  = "priority_score_asc"
)

func supplyChainImpactPriorityFilter(r *http.Request) (string, int, string, error) {
	bucket := QueryParam(r, "priority_bucket")
	if bucket != "" && !validSupplyChainImpactPriorityBucket(bucket) {
		return "", 0, "", fmt.Errorf("priority_bucket must be critical, high, medium, low, or informational")
	}
	minScore, err := optionalSupplyChainImpactMinPriorityScore(r)
	if err != nil {
		return "", 0, "", err
	}
	sort := normalizeSupplyChainImpactSort(QueryParam(r, "sort"))
	if !validSupplyChainImpactSort(sort) {
		return "", 0, "", fmt.Errorf("sort must be finding_id, priority, priority_score_desc, or priority_score_asc")
	}
	return bucket, minScore, sort, nil
}

func optionalSupplyChainImpactMinPriorityScore(r *http.Request) (int, error) {
	raw := QueryParam(r, "min_priority_score")
	if raw == "" {
		return 0, nil
	}
	score, err := strconv.Atoi(raw)
	if err != nil || score < 0 || score > 100 {
		return 0, fmt.Errorf("min_priority_score must be between 0 and 100")
	}
	return score, nil
}

func validSupplyChainImpactPriorityBucket(bucket string) bool {
	switch bucket {
	case "critical", "high", "medium", "low", "informational":
		return true
	default:
		return false
	}
}

func normalizeSupplyChainImpactSort(sort string) string {
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
