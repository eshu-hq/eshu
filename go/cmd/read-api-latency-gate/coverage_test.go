// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

func TestExercisedCoverageCounts(t *testing.T) {
	results := []RouteLatency{
		{Route: "a", Exercised: true},
		{Route: "b", Exercised: true},
		{Route: "c", Exercised: false, Status: 400},
	}
	exercised, total := ExercisedCoverage(results)
	if exercised != 2 {
		t.Errorf("exercised = %d, want 2", exercised)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
}

func TestCheckCoverageFloorPassesAtOrAboveFloor(t *testing.T) {
	results := []RouteLatency{
		{Route: "a", Exercised: true},
		{Route: "b", Exercised: true},
	}
	if err := CheckCoverageFloor(results, 2); err != nil {
		t.Errorf("CheckCoverageFloor: %v, want nil (exercised count meets the floor)", err)
	}
}

func TestCheckCoverageFloorFailsBelowFloor(t *testing.T) {
	results := []RouteLatency{
		{Route: "a", Exercised: true},
		{Route: "b", Exercised: false, Status: 400},
	}
	if err := CheckCoverageFloor(results, 2); err == nil {
		t.Errorf("expected an error: exercised count 1 is below the floor 2")
	}
}
