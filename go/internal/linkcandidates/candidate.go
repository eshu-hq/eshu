// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linkcandidates

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// TruthLevel marks diagnostic link-prediction output as non-canonical evidence.
type TruthLevel string

const (
	// TruthLevelCandidate means the suggestion is bounded candidate evidence.
	TruthLevelCandidate TruthLevel = "candidate"
	// TruthLevelSemanticCandidate means semantic retrieval produced the candidate.
	TruthLevelSemanticCandidate TruthLevel = "semantic_candidate"
)

// Decision records what the candidate generator did with one suggestion.
type Decision string

const (
	// DecisionGenerated means the candidate is visible as diagnostic evidence.
	DecisionGenerated Decision = "generated"
	// DecisionSuppressed means the candidate was withheld but counted.
	DecisionSuppressed Decision = "suppressed"
	// DecisionAmbiguous means the candidate remains provenance-only.
	DecisionAmbiguous Decision = "ambiguous"
)

// FreshnessState records candidate evidence freshness.
type FreshnessState string

const (
	// FreshnessFresh means the candidate was generated from fresh input.
	FreshnessFresh FreshnessState = "fresh"
	// FreshnessStale means the candidate was generated from stale input.
	FreshnessStale FreshnessState = "stale"
	// FreshnessBuilding means the candidate input is still being built.
	FreshnessBuilding FreshnessState = "building"
	// FreshnessUnavailable means freshness could not be established.
	FreshnessUnavailable FreshnessState = "unavailable"
)

// Freshness records when and how recently candidate evidence was observed.
type Freshness struct {
	State      FreshnessState `json:"state"`
	ObservedAt time.Time      `json:"observed_at"`
}

// GraphHandle is a bounded graph expansion handle for a candidate endpoint.
type GraphHandle struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Candidate is one diagnostic relationship suggestion from link prediction.
type Candidate struct {
	ID              string      `json:"id"`
	Algorithm       string      `json:"algorithm"`
	Score           float64     `json:"score"`
	Source          GraphHandle `json:"source"`
	Target          GraphHandle `json:"target"`
	EvidenceContext string      `json:"evidence_context"`
	Freshness       Freshness   `json:"freshness"`
	Reason          string      `json:"reason"`
	TruthLevel      TruthLevel  `json:"truth_level"`
	Decision        Decision    `json:"decision"`
}

// Observation is the low-cardinality counter key for candidate generation.
type Observation struct {
	Algorithm string
	Decision  Decision
}

// ValidateCandidate checks the diagnostic candidate evidence contract.
func ValidateCandidate(candidate Candidate) error {
	var problems []string
	if strings.TrimSpace(candidate.ID) == "" {
		problems = append(problems, "id is required")
	}
	if strings.TrimSpace(candidate.Algorithm) == "" {
		problems = append(problems, "algorithm is required")
	} else if !validAlgorithm(candidate.Algorithm) {
		problems = append(problems, "algorithm must be a low-cardinality token")
	}
	if math.IsNaN(candidate.Score) || math.IsInf(candidate.Score, 0) ||
		candidate.Score < 0 || candidate.Score > 1 {
		problems = append(problems, "score must be finite and between 0 and 1")
	}
	if !validHandle(candidate.Source) {
		problems = append(problems, "source handle is required")
	}
	if !validHandle(candidate.Target) {
		problems = append(problems, "target handle is required")
	}
	if strings.TrimSpace(candidate.EvidenceContext) == "" {
		problems = append(problems, "evidence_context is required")
	}
	problems = append(problems, validateFreshness(candidate.Freshness)...)
	if strings.TrimSpace(candidate.Reason) == "" {
		problems = append(problems, "reason is required")
	}
	if !validTruthLevel(candidate.TruthLevel) {
		problems = append(problems, "truth_level must be candidate or semantic_candidate")
	}
	if !validDecision(candidate.Decision) {
		problems = append(problems, "decision is invalid")
	}
	return joinedValidationError(problems)
}

// ObservationFor returns the bounded telemetry dimensions for a candidate.
func ObservationFor(candidate Candidate) Observation {
	return Observation{
		Algorithm: strings.TrimSpace(candidate.Algorithm),
		Decision:  candidate.Decision,
	}
}

func validateFreshness(freshness Freshness) []string {
	var problems []string
	if freshness.ObservedAt.IsZero() {
		problems = append(problems, "freshness.observed_at is required")
	}
	switch freshness.State {
	case FreshnessFresh, FreshnessStale, FreshnessBuilding, FreshnessUnavailable:
	default:
		problems = append(problems, "freshness.state is invalid")
	}
	return problems
}

func validHandle(handle GraphHandle) bool {
	return strings.TrimSpace(handle.Kind) != "" && strings.TrimSpace(handle.ID) != ""
}

func validAlgorithm(algorithm string) bool {
	algorithm = strings.TrimSpace(algorithm)
	if algorithm == "" || len(algorithm) > 64 {
		return false
	}
	for _, r := range algorithm {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func validTruthLevel(level TruthLevel) bool {
	switch level {
	case TruthLevelCandidate, TruthLevelSemanticCandidate:
		return true
	default:
		return false
	}
}

func validDecision(decision Decision) bool {
	switch decision {
	case DecisionGenerated, DecisionSuppressed, DecisionAmbiguous:
		return true
	default:
		return false
	}
}

func joinedValidationError(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	errs := make([]error, 0, len(problems))
	for _, problem := range problems {
		errs = append(errs, errors.New(problem))
	}
	return fmt.Errorf("link candidate validation: %w", errors.Join(errs...))
}
