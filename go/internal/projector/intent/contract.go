// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intent

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// ReducerIntent describes one shared-domain work item emitted after
// source-local projection.
type ReducerIntent struct {
	ScopeID      string
	GenerationID string
	Domain       reducer.Domain
	EntityKey    string
	Reason       string
	FactID       string
	SourceSystem string
	Payload      map[string]any
}

// ScopeGenerationKey returns the stable scope-generation identity for the
// intent.
func (i ReducerIntent) ScopeGenerationKey() string {
	return fmt.Sprintf("%s:%s", i.ScopeID, i.GenerationID)
}

// FactLookup is the read-only fact-selection contract used by intent-family
// builders. Implementations must return the earliest matching fact in the
// original generation order.
type FactLookup interface {
	FirstOfKind(kind string) (facts.Envelope, bool)
	FirstOfKindMatching(kind string, accept func(facts.Envelope) bool) (facts.Envelope, bool)
	FirstAcrossKinds(accept func(facts.Envelope) bool, kinds ...string) (facts.Envelope, bool)
	FirstMatchingKindPredicate(
		kindPredicate func(string) bool,
		accept func(facts.Envelope) bool,
	) (facts.Envelope, bool)
}
