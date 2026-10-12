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

func TestCoverageFloorAddsSelectedOperations(t *testing.T) {
	if got := CoverageFloor(nil); got != ExercisedCoverageFloor {
		t.Errorf("CoverageFloor(nil) = %d, want %d", got, ExercisedCoverageFloor)
	}
	operations := map[string]Operation{"pilot-a": {}, "pilot-b": {}, "pilot-c": {}}
	if got := CoverageFloor(operations); got != ExercisedCoverageFloor+3 {
		t.Errorf("CoverageFloor(3 operations) = %d, want %d", got, ExercisedCoverageFloor+3)
	}
	// Three pilots all exercised and three routes dropped must fail the run floor.
	results := make([]RouteLatency, 0, ExercisedCoverageFloor)
	for i := 0; i < ExercisedCoverageFloor; i++ {
		results = append(results, RouteLatency{Route: "r", Exercised: true})
	}
	if err := CheckCoverageFloor(results, CoverageFloor(operations)); err == nil {
		t.Error("expected failure: only the pinned floor is exercised while three operations were selected")
	}
}
