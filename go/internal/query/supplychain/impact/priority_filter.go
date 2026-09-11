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

func SupplyChainImpactPriorityFilter(r *http.Request) (string, int, string, error) {
	bucket := querycontract.QueryParam(r, "priority_bucket")
	if bucket != "" && !ValidSupplyChainImpactPriorityBucket(bucket) {
		return "", 0, "", fmt.Errorf("priority_bucket must be critical, high, medium, low, or informational")
	}
	minScore, err := OptionalSupplyChainImpactMinPriorityScore(r)
	if err != nil {
		return "", 0, "", err
	}
	sort := NormalizeSupplyChainImpactSort(querycontract.QueryParam(r, "sort"))
	if !validSupplyChainImpactSort(sort) {
		return "", 0, "", fmt.Errorf("sort must be finding_id, priority, priority_score_desc, or priority_score_asc")
	}
	return bucket, minScore, sort, nil
}

func OptionalSupplyChainImpactMinPriorityScore(r *http.Request) (int, error) {
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

func ValidSupplyChainImpactPriorityBucket(bucket string) bool {
	switch bucket {
	case "critical", "high", "medium", "low", "informational":
		return true
	default:
		return false
	}
}

func NormalizeSupplyChainImpactSort(sort string) string {
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
