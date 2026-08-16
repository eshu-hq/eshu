// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demo

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/firstrunbench"
)

// The demo scorecard speaks the first-run benchmark's criterion vocabulary.
// firstrunbench.Criterion, CriterionName, CriterionStatus, their constants,
// and the shared criterion names are imported, not copied, so a harness
// reading either scorecard sees one shape and a wire change happens in
// exactly one place. Only the two criteria below are scored solely by the
// demo lane, so only they are declared here.

const (
	// CriterionPhaseTimings asserts every required phase was recorded.
	CriterionPhaseTimings firstrunbench.CriterionName = "phase_timings_complete"
	// CriterionModeObserved asserts the declared cold/warm mode matches
	// what the harness observed about the image cache.
	CriterionModeObserved firstrunbench.CriterionName = "declared_mode_matches_observed"
)

// quoteIfEmpty renders a placeholder for an empty value so the scorecard line
// stays readable. Copied from go/cmd/eshu's first_run.go, which is `package
// main` and cannot be imported; firstrunbench keeps its own unexported copy
// for the same reason.
func quoteIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "<repo>"
	}
	return value
}
