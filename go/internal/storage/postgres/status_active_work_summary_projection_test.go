// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestActiveWorkSummaryProjectsWideFieldsOnlyForConsumers guards the measured
// #7009 spill fix on the SQL actually used by the status snapshot.
func TestActiveWorkSummaryProjectsWideFieldsOnlyForConsumers(t *testing.T) {
	t.Parallel()

	if err := checkActiveWorkSummaryProjection(activeWorkSummaryQuery); err != nil {
		t.Fatal(err)
	}
	original := "SELECT " + activeWorkSummaryColumns + "\n  FROM fact_work_items AS work"
	widened := strings.Replace(activeWorkSummaryQuery, original,
		"SELECT work.*\n  FROM fact_work_items AS work", 1)
	if widened == activeWorkSummaryQuery {
		t.Fatal("wildcard positive control did not change the production CTE")
	}
	if err := checkActiveWorkSummaryProjection(widened); err == nil {
		t.Fatal("wildcard positive control passed the projection guard")
	}
	appended := strings.Replace(activeWorkSummaryQuery, original,
		"SELECT "+activeWorkSummaryColumns+",\n         *\n  FROM fact_work_items AS work", 1)
	if appended == activeWorkSummaryQuery {
		t.Fatal("trailing wildcard positive control did not change the production CTE")
	}
	if err := checkActiveWorkSummaryProjection(appended); err == nil {
		t.Fatal("trailing wildcard positive control passed the projection guard")
	}
}

func checkActiveWorkSummaryProjection(query string) error {
	_, rest, ok := strings.Cut(query, "active_fact_work_items AS MATERIALIZED (\n")
	if !ok {
		return fmt.Errorf("summary has no materialized active work CTE")
	}
	cte, _, ok := strings.Cut(rest, "\n),\n")
	if !ok {
		return fmt.Errorf("summary has no materialized active work CTE boundary")
	}
	projection, _, ok := strings.Cut(cte, "\n  FROM fact_work_items AS work")
	if !ok {
		return fmt.Errorf("summary has no active work projection boundary")
	}
	if regexp.MustCompile(`(?:SELECT|,)\s*(?:[A-Za-z_][A-Za-z_0-9]*\.)?\*`).MatchString(projection) {
		return fmt.Errorf("summary materializes a wildcard projection")
	}
	required := map[string]string{
		"payload": `CASE WHEN work.stage = 'reducer'
                   AND work.status IN ('pending', 'retrying', 'claimed', 'running')
              THEN work.payload END AS payload`,
		"failure_class": `CASE WHEN work.status IN ('retrying', 'failed', 'dead_letter')
              THEN work.failure_class END AS failure_class`,
		"failure_message": `CASE WHEN work.status IN ('retrying', 'failed', 'dead_letter')
              THEN work.failure_message END AS failure_message`,
		"failure_details": `CASE WHEN work.status IN ('retrying', 'failed', 'dead_letter')
              THEN work.failure_details END AS failure_details`,
	}
	for column, clause := range required {
		if strings.Count(projection, "work."+column) != 1 || !strings.Contains(projection, clause) {
			return fmt.Errorf("summary does not conditionally project %s", column)
		}
	}
	return nil
}
