// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linkcandidates

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestValidateCandidateAcceptsGeneratedSemanticCandidate(t *testing.T) {
	t.Parallel()

	candidate := validCandidate()
	candidate.TruthLevel = TruthLevelSemanticCandidate
	candidate.Decision = DecisionGenerated

	if err := ValidateCandidate(candidate); err != nil {
		t.Fatalf("ValidateCandidate() error = %v, want nil", err)
	}
}

func TestValidateCandidateAcceptsSuppressedAndAmbiguousCandidates(t *testing.T) {
	t.Parallel()

	suppressed := validCandidate()
	suppressed.ID = "candidate:suppressed"
	suppressed.Decision = DecisionSuppressed
	suppressed.Reason = "score below candidate threshold"

	if err := ValidateCandidate(suppressed); err != nil {
		t.Fatalf("ValidateCandidate(suppressed) error = %v, want nil", err)
	}

	ambiguous := validCandidate()
	ambiguous.ID = "candidate:ambiguous"
	ambiguous.Decision = DecisionAmbiguous
	ambiguous.Reason = "multiple targets share the same semantic neighborhood"

	if err := ValidateCandidate(ambiguous); err != nil {
		t.Fatalf("ValidateCandidate(ambiguous) error = %v, want nil", err)
	}
}

func TestValidateCandidateRejectsCanonicalOrIncompleteShape(t *testing.T) {
	t.Parallel()

	err := ValidateCandidate(Candidate{
		Score:      1.2,
		TruthLevel: TruthLevel("canonical"),
		Freshness:  Freshness{State: FreshnessState("unknown")},
	})
	if err == nil {
		t.Fatal("ValidateCandidate() error = nil, want validation errors")
	}
	for _, want := range []string{
		"id is required",
		"algorithm is required",
		"score must be finite and between 0 and 1",
		"source handle is required",
		"target handle is required",
		"evidence_context is required",
		"freshness.observed_at is required",
		"freshness.state is invalid",
		"reason is required",
		"truth_level must be candidate or semantic_candidate",
		"decision is invalid",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateCandidate() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateCandidateRejectsNonFiniteScore(t *testing.T) {
	t.Parallel()

	candidate := validCandidate()
	candidate.Score = math.NaN()

	err := ValidateCandidate(candidate)
	if err == nil {
		t.Fatal("ValidateCandidate() error = nil, want non-finite score error")
	}
	if want := "score must be finite and between 0 and 1"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateCandidate() error = %q, want substring %q", err, want)
	}
}

func TestValidateCandidateRejectsHighCardinalityAlgorithm(t *testing.T) {
	t.Parallel()

	candidate := validCandidate()
	candidate.Algorithm = "nornicdb.adamic_adar/service:checkout"

	err := ValidateCandidate(candidate)
	if err == nil {
		t.Fatal("ValidateCandidate() error = nil, want algorithm token error")
	}
	if want := "algorithm must be a low-cardinality token"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateCandidate() error = %q, want substring %q", err, want)
	}
}

func TestObservationForUsesLowCardinalityDimensions(t *testing.T) {
	t.Parallel()

	observation := ObservationFor(validCandidate())
	if got, want := observation.Algorithm, "nornicdb.adamic_adar"; got != want {
		t.Fatalf("observation.Algorithm = %q, want %q", got, want)
	}
	if got, want := observation.Decision, DecisionGenerated; got != want {
		t.Fatalf("observation.Decision = %q, want %q", got, want)
	}
}

func TestCandidateJSONUsesStableFieldNames(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(validCandidate())
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	body := string(payload)
	for _, want := range []string{
		`"evidence_context"`,
		`"truth_level"`,
		`"observed_at"`,
		`"kind"`,
		`"id"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("json payload = %s, want field %s", body, want)
		}
	}
	if strings.Contains(body, "Kind") || strings.Contains(body, "TruthLevel") {
		t.Fatalf("json payload = %s, want stable lower-case field names", body)
	}
}

func validCandidate() Candidate {
	return Candidate{
		ID:              "candidate:checkout:payments",
		Algorithm:       "nornicdb.adamic_adar",
		Score:           0.82,
		Source:          GraphHandle{Kind: "service", ID: "checkout"},
		Target:          GraphHandle{Kind: "service", ID: "payments"},
		EvidenceContext: "shared deployment context and code-reference neighborhood",
		Freshness: Freshness{
			State:      FreshnessFresh,
			ObservedAt: time.Date(2026, 6, 2, 8, 0, 0, 0, time.UTC),
		},
		Reason:     "link prediction suggests a related service dependency",
		TruthLevel: TruthLevelCandidate,
		Decision:   DecisionGenerated,
	}
}
