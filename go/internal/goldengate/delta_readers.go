// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// DeltaReaderAssertions is the snapshot's graph.delta_reader_assertions
// section (#10022): reducer outputs of one delta-active git repository that a
// full generation of the same commit produces. A reader that loses the base
// generation's unchanged rows while the scope is delta-active drops an edge
// here, which the delta-union file-set comparison cannot see.
type DeltaReaderAssertions struct {
	// Description explains the fixture and how the gate drives it.
	Description string `json:"description"`
	// RepositoryID is the canonical Repository node id the edges hang off
	// (Repository-[:REPO_CONTAINS]->File-[:CONTAINS]->entity).
	RepositoryID string `json:"repository_id"`
	// ScopeID is the git ingestion scope whose active generation must be a
	// full at the full stage and a delta at the delta stage.
	ScopeID string `json:"scope_id"`
	// Edges are the asserted entity-to-entity relationships.
	Edges []DeltaReaderEdge `json:"edges"`
	// Unexpressed lists affected readers this fixture cannot assert, each
	// with its defect and the reason, so the gap is reported on every run
	// instead of being silently absent.
	Unexpressed []DeltaReaderGap `json:"unexpressed_readers,omitempty"`
}

// DeltaReaderEdge is one asserted relationship between two entities of the
// delta-active repository, matched by relationship type, a label each
// endpoint carries, and each endpoint's name.
type DeltaReaderEdge struct {
	ID           string `json:"id"`
	Description  string `json:"description"`
	Reader       string `json:"reader"`
	Relationship string `json:"relationship"`
	FromLabel    string `json:"from_label"`
	FromName     string `json:"from_name"`
	ToLabel      string `json:"to_label"`
	ToName       string `json:"to_name"`
	// ExpectedFailure, when set, marks an open defect: at the delta stage the
	// edge must be ABSENT, and a present edge is a required failure telling
	// the fixing PR to remove the marker. The full stage still requires the
	// edge, so the marker can never pass an edge no generation produced.
	ExpectedFailure *DeltaReaderExpectedFailure `json:"expected_failure,omitempty"`
}

// DeltaReaderExpectedFailure names the open defect behind an expected loss.
type DeltaReaderExpectedFailure struct {
	// Defect is the tracking issue, "#N".
	Defect string `json:"defect"`
	// Reason states which read loses which base rows.
	Reason string `json:"reason"`
}

// DeltaReaderGap records an affected reader the fixture does not assert.
type DeltaReaderGap struct {
	Reader string `json:"reader"`
	Defect string `json:"defect"`
	Reason string `json:"reason"`
}

// DeltaReaderStage selects which generation the observation must reflect.
type DeltaReaderStage string

const (
	// DeltaReaderStageFull runs after the base full generation is active:
	// every edge, expected failures included, must be present.
	DeltaReaderStageFull DeltaReaderStage = "full"
	// DeltaReaderStageDelta runs after the delta generation is active and
	// drained: plain edges must survive and expected failures must stay lost.
	DeltaReaderStageDelta DeltaReaderStage = "delta"
)

// DeltaReaderObservedEdge is one graph relationship read from the repository.
type DeltaReaderObservedEdge struct {
	Relationship string
	FromLabels   []string
	FromName     string
	ToLabels     []string
	ToName       string
}

// DeltaReaderObservation is the scope's generation state plus the edges the
// graph holds for the repository's entities.
type DeltaReaderObservation struct {
	// ActiveGenerationID is the scope's active generation, "" when none.
	ActiveGenerationID string
	// ActiveIsDelta tells whether that generation is a delta.
	ActiveIsDelta bool
	// Edges are the repository's entity relationships.
	Edges []DeltaReaderObservedEdge
}

const deltaReaderPhase = "delta-readers"

var deltaReaderDefectPattern = regexp.MustCompile(`^#[0-9]+$`)

// EvaluateDeltaReaderAssertions records the delta-reader findings for one
// stage. honorExpectedFailures=false treats every edge as plainly required,
// which is how a run captures the RED of the open defects. An absent or
// invalid section, an unknown stage, or a scope whose active generation does
// not match the stage is a required failure: each would make the edge checks
// vacuous.
func EvaluateDeltaReaderAssertions(
	spec *DeltaReaderAssertions,
	stage DeltaReaderStage,
	obs DeltaReaderObservation,
	honorExpectedFailures bool,
	r *Report,
) {
	if problems := validateDeltaReaderSpec(spec); len(problems) != 0 {
		r.AddCheck(deltaReaderPhase, "spec", false, true,
			"invalid graph.delta_reader_assertions: "+strings.Join(problems, "; "))
		return
	}
	if stage != DeltaReaderStageFull && stage != DeltaReaderStageDelta {
		r.AddCheck(deltaReaderPhase, "stage", false, true,
			fmt.Sprintf("unknown stage %q, want %q or %q", stage, DeltaReaderStageFull, DeltaReaderStageDelta))
		return
	}
	wantDelta := stage == DeltaReaderStageDelta
	genOK := obs.ActiveGenerationID != "" && obs.ActiveIsDelta == wantDelta
	r.AddCheck(deltaReaderPhase, "active_generation", genOK, true,
		fmt.Sprintf("stage=%s scope=%s active=%q delta=%v (want delta=%v)",
			stage, spec.ScopeID, obs.ActiveGenerationID, obs.ActiveIsDelta, wantDelta))
	for _, gap := range spec.Unexpressed {
		r.AddCheck(deltaReaderPhase, "unexpressed_"+gap.Reader, true, false,
			fmt.Sprintf("not expressed (%s): %s", gap.Defect, gap.Reason))
	}
	if !genOK {
		return
	}
	for _, want := range spec.Edges {
		present := deltaReaderEdgePresent(want, obs.Edges)
		check := "edge_" + want.ID
		label := fmt.Sprintf("%s:%s -[%s]-> %s:%s (reader %s)",
			want.FromLabel, want.FromName, want.Relationship, want.ToLabel, want.ToName, want.Reader)
		switch {
		case !wantDelta || want.ExpectedFailure == nil || !honorExpectedFailures:
			detail := "present: " + label
			if !present {
				detail = fmt.Sprintf("missing at stage %s: %s", stage, label)
			}
			r.AddCheck(deltaReaderPhase, check, present, true, detail)
		case present:
			r.AddCheck(deltaReaderPhase, check, false, true, fmt.Sprintf(
				"XPASS: %s survived the delta, so %s appears fixed; remove expected_failure from %s",
				label, want.ExpectedFailure.Defect, want.ID))
		default:
			r.AddCheck(deltaReaderPhase, check, true, true, fmt.Sprintf(
				"expected failure %s holds: %s lost under the delta (%s)",
				want.ExpectedFailure.Defect, label, want.ExpectedFailure.Reason))
		}
	}
}

func deltaReaderEdgePresent(want DeltaReaderEdge, edges []DeltaReaderObservedEdge) bool {
	for _, got := range edges {
		if got.Relationship == want.Relationship &&
			got.FromName == want.FromName && got.ToName == want.ToName &&
			slices.Contains(got.FromLabels, want.FromLabel) &&
			slices.Contains(got.ToLabels, want.ToLabel) {
			return true
		}
	}
	return false
}

func validateDeltaReaderSpec(spec *DeltaReaderAssertions) []string {
	if spec == nil {
		return []string{"section is absent"}
	}
	var problems []string
	if strings.TrimSpace(spec.RepositoryID) == "" {
		problems = append(problems, "repository_id is empty")
	}
	if strings.TrimSpace(spec.ScopeID) == "" {
		problems = append(problems, "scope_id is empty")
	}
	if len(spec.Edges) == 0 {
		problems = append(problems, "edges is empty")
	}
	seen := make(map[string]bool, len(spec.Edges))
	for i, e := range spec.Edges {
		name := fmt.Sprintf("edges[%d]", i)
		if strings.TrimSpace(e.ID) == "" {
			problems = append(problems, name+" has no id")
		} else if seen[e.ID] {
			problems = append(problems, name+" duplicates id "+e.ID)
		}
		seen[e.ID] = true
		for field, value := range map[string]string{
			"reader": e.Reader, "relationship": e.Relationship, "from_label": e.FromLabel,
			"from_name": e.FromName, "to_label": e.ToLabel, "to_name": e.ToName,
		} {
			if strings.TrimSpace(value) == "" {
				problems = append(problems, fmt.Sprintf("%s (%s) has empty %s", name, e.ID, field))
			}
		}
		if f := e.ExpectedFailure; f != nil {
			if !deltaReaderDefectPattern.MatchString(f.Defect) {
				problems = append(problems, fmt.Sprintf("%s (%s) expected_failure.defect %q is not #N", name, e.ID, f.Defect))
			}
			if strings.TrimSpace(f.Reason) == "" {
				problems = append(problems, fmt.Sprintf("%s (%s) expected_failure has no reason", name, e.ID))
			}
		}
	}
	for i, g := range spec.Unexpressed {
		if strings.TrimSpace(g.Reader) == "" || strings.TrimSpace(g.Reason) == "" ||
			!deltaReaderDefectPattern.MatchString(g.Defect) {
			problems = append(problems, fmt.Sprintf("unexpressed_readers[%d] needs reader, #N defect, and reason", i))
		}
	}
	slices.Sort(problems)
	return problems
}
