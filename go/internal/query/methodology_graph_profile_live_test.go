// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"fmt"
	"strings"

	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// methodologyValidateProfileCounters verifies raw counters before the pinned
// driver can turn missing or wrong-typed Bolt fields into zero-valued accessors.
func methodologyValidateProfileCounters(plan neo4j.ProfiledPlan, capturedRows int) error {
	if plan == nil {
		return fmt.Errorf("PROFILE has no plan")
	}
	if plan.Records() != int64(capturedRows) {
		return fmt.Errorf("PROFILE root rows=%d, captured rows=%d", plan.Records(), capturedRows)
	}
	var check func(neo4j.ProfiledPlan) error
	check = func(node neo4j.ProfiledPlan) error {
		if strings.TrimSpace(node.Operator()) == "" {
			return fmt.Errorf("PROFILE has unnamed operator")
		}
		arguments := node.Arguments()
		hits, hitsOK := arguments["DbHits"].(int64)
		rows, rowsOK := arguments["Rows"].(int64)
		cacheHits, cacheHitsOK := arguments["PageCacheHits"].(int64)
		cacheMisses, cacheMissesOK := arguments["PageCacheMisses"].(int64)
		if !hitsOK || !rowsOK || !cacheHitsOK || !cacheMissesOK ||
			hits < 0 || rows < 0 || cacheHits < 0 || cacheMisses < 0 ||
			hits != node.DbHits() || rows != node.Records() ||
			cacheHits != node.PageCacheHits() || cacheMisses != node.PageCacheMisses() {
			return fmt.Errorf("PROFILE operator %s lacks matching numeric work arguments", node.Operator())
		}
		if raw, present := arguments["Time"]; present {
			value, ok := raw.(int64)
			if !ok || value < 0 || value != node.Time() {
				return fmt.Errorf("PROFILE operator %s has invalid Time argument", node.Operator())
			}
		}
		if raw, present := arguments["PageCacheHitRatio"]; present {
			value, ok := raw.(float64)
			if !ok || value < 0 || value > 1 || value != node.PageCacheHitRatio() {
				return fmt.Errorf("PROFILE operator %s has invalid PageCacheHitRatio argument", node.Operator())
			}
		}
		for _, child := range node.Children() {
			if child == nil {
				return fmt.Errorf("PROFILE operator %s has nil child", node.Operator())
			}
			if err := check(child); err != nil {
				return err
			}
		}
		return nil
	}
	return check(plan)
}

func methodologyPlanTree(plan neo4j.ProfiledPlan) methodologyPlanReport {
	result := methodologyPlanReport{
		Operator: plan.Operator(), Arguments: plan.Arguments(), Identifiers: plan.Identifiers(),
		DbHits: plan.DbHits(), Rows: plan.Records(), PageCacheHits: plan.PageCacheHits(), PageCacheMisses: plan.PageCacheMisses(),
	}
	if value, present := plan.Arguments()["PageCacheHitRatio"].(float64); present {
		result.PageCacheHitRatio = &value
	}
	if value, present := plan.Arguments()["Time"].(int64); present {
		result.TimeRaw = &value
	}
	for _, child := range plan.Children() {
		result.Children = append(result.Children, methodologyPlanTree(child))
	}
	return result
}

func methodologyPlanHits(plan neo4j.ProfiledPlan) int64 {
	if plan == nil {
		return 0
	}
	hits := plan.DbHits()
	for _, child := range plan.Children() {
		hits += methodologyPlanHits(child)
	}
	return hits
}
