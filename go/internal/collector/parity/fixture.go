// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// FactClass models how reducer admission is expected to treat a fixture fact.
// It captures the universal claim-driven contract without reimplementing any
// domain-specific reducer: admissible facts become readable (reducer readback),
// while permission-hidden and unsupported facts are committed as source evidence
// but must never appear in readback.
type FactClass string

const (
	// FactAdmissible facts are expected to pass reducer admission and become
	// readable, subject to idempotency and fencing.
	FactAdmissible FactClass = "admissible"
	// FactPermissionHidden facts are committed as evidence but withheld from
	// readback because the caller lacks permission to see them.
	FactPermissionHidden FactClass = "permission_hidden"
	// FactUnsupported facts are committed but have no reducer rule, so they are
	// counted and never become readable.
	FactUnsupported FactClass = "unsupported"
)

// ClaimOutcome is the terminal disposition the harness observed for a claim.
type ClaimOutcome string

const (
	// ClaimCompleted means the claim committed (or was unchanged) and completed.
	ClaimCompleted ClaimOutcome = "completed"
	// ClaimReleased means the source produced no generation and released.
	ClaimReleased ClaimOutcome = "released"
	// ClaimFailedRetryable means the claim failed and may be retried.
	ClaimFailedRetryable ClaimOutcome = "failed_retryable"
	// ClaimFailedTerminal means the claim failed terminally.
	ClaimFailedTerminal ClaimOutcome = "failed_terminal"
	// ClaimNone means no terminal disposition was recorded (a harness error).
	ClaimNone ClaimOutcome = ""
)

// FixtureFact pairs a fact envelope with its expected admission class. The
// harness stamps the owning scope, generation, and fencing token onto the
// envelope when they are unset so fixtures stay terse and internally consistent.
type FixtureFact struct {
	Envelope facts.Envelope
	Class    FactClass
}

// AdmissibleFact builds an admissible fixture fact with the given kind and
// stable key. The harness fills scope, generation, and fencing token.
func AdmissibleFact(factKind, stableKey string) FixtureFact {
	return classFact(factKind, stableKey, FactAdmissible)
}

// PermissionHiddenFact builds a permission-hidden fixture fact.
func PermissionHiddenFact(factKind, stableKey string) FixtureFact {
	return classFact(factKind, stableKey, FactPermissionHidden)
}

// UnsupportedFact builds an unsupported fixture fact.
func UnsupportedFact(factKind, stableKey string) FixtureFact {
	return classFact(factKind, stableKey, FactUnsupported)
}

func classFact(factKind, stableKey string, class FactClass) FixtureFact {
	return FixtureFact{
		Envelope: facts.Envelope{
			FactID:        stableKey,
			FactKind:      factKind,
			StableFactKey: stableKey,
		},
		Class: class,
	}
}

// Scenario is one fully in-memory claim-driven collection attempt. Use
// NewScenario to build a consistent scope/generation/work-item triple, then set
// the behavior and Expect fields. One Scenario drives exactly one claim attempt;
// multi-attempt sequences (retry-then-success, duplicate delivery, stale
// generation) run multiple scenarios against one Harness.
type Scenario struct {
	Name          string
	CollectorKind scope.CollectorKind
	InstanceID    string
	ScopeKind     scope.ScopeKind
	Scope         scope.IngestionScope
	Generation    scope.ScopeGeneration
	WorkItem      workflow.WorkItem
	FencingToken  int64
	Facts         []FixtureFact

	// SourceNotReady makes the claimed source report no generation, so the claim
	// is released without a commit.
	SourceNotReady bool
	// Unchanged makes the claimed source report a generation with no new facts;
	// the claim completes without a commit.
	Unchanged bool
	// CollectErr is returned by the claimed source. Wrap with TerminalError for
	// terminal classification; a plain error is retryable.
	CollectErr error
	// CommitErr is returned by the committer on this attempt. The collector
	// records a dead-letter and routes to retryable or terminal accordingly.
	CommitErr error
	// MaxAttempts is the bounded retry budget. Zero preserves unbounded retries.
	MaxAttempts int

	// Expect is the contract the harness verifies after the attempt.
	Expect Expectation
}

// Expectation is the contract a scenario must satisfy. Empty slices mean "none".
type Expectation struct {
	// ClaimOutcome is the required terminal claim disposition.
	ClaimOutcome ClaimOutcome
	// DeadLettered requires (or forbids) a recorded generation dead-letter.
	DeadLettered bool
	// ReadableFactKinds is the exact set of fact kinds expected to be readable
	// in the shared readback store after this attempt (cumulative across the
	// harness lifetime), sorted ascending. A nil value skips the readback
	// assertion; use an empty non-nil slice to assert that nothing is readable.
	ReadableFactKinds []string
}

// NewScenario builds a scenario with a consistent scope, generation, and work
// item for a non-Terraform claim-driven collector. The source system defaults to
// the collector kind and the scope kind defaults to a repository scope; override
// the returned fields as needed before running.
func NewScenario(name string, kind scope.CollectorKind, scopeID, generationID string, fencingToken int64) Scenario {
	sourceSystem := string(kind)
	observedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sc := Scenario{
		Name:          name,
		CollectorKind: kind,
		InstanceID:    "collector-" + string(kind),
		ScopeKind:     scope.KindRepository,
		FencingToken:  fencingToken,
	}
	sc.Scope = scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  sourceSystem,
		ScopeKind:     sc.ScopeKind,
		CollectorKind: kind,
		PartitionKey:  scopeID,
	}
	sc.Generation = scope.ScopeGeneration{
		GenerationID: generationID,
		ScopeID:      scopeID,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		Status:       scope.GenerationStatusActive,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	sc.WorkItem = workflow.WorkItem{
		WorkItemID:          "work-" + scopeID,
		CollectorKind:       kind,
		CollectorInstanceID: sc.InstanceID,
		SourceSystem:        sourceSystem,
		ScopeID:             scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		Status:              workflow.WorkItemStatusClaimed,
		CurrentFencingToken: fencingToken,
	}
	return sc
}

// WithFacts attaches fixture facts to the scenario and returns it for chaining.
func (s Scenario) WithFacts(facts ...FixtureFact) Scenario {
	s.Facts = append(s.Facts, facts...)
	return s
}

// Expecting sets the scenario expectation and returns it for chaining.
func (s Scenario) Expecting(expect Expectation) Scenario {
	s.Expect = expect
	return s
}
