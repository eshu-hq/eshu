// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cassette

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Compile-time proof that the cassette flavor satisfies the shared replay.Source
// contract (which embeds collector.Source), so it drops into the same
// collector.Service poll loop as the live collector.
var _ replay.Source = (*Source)(nil)

// DeriveFactID returns the fact_id a cassette-replayed envelope carries when
// the cassette file leaves it unset: a StableID over the CassetteReplay
// namespace keyed by scope, generation, and stable fact key, so every replay
// of the same cassette stamps the same fact_ids.
//
// Exported because the incident_routing_evidence live leaf's vacuity guard
// (#6228) must stamp the same ids onto the compiled Odù's envelopes before
// running the production builder: the reducer keys incident-routing evidence
// identity off the envelope fact_id, so a guard that kept the fixture's
// literal ids would prove an edge set no live drive can reproduce. The
// committed cassettes carry no fact_id field, so the drive always takes this
// branch; stamping it in the guard replays the same derivation instead of
// duplicating its body.
func DeriveFactID(scopeID, generationID, stableFactKey string) string {
	return facts.StableID("CassetteReplay", map[string]any{
		"scope_id":        scopeID,
		"generation_id":   generationID,
		"stable_fact_key": stableFactKey,
	})
}

// Source implements collector.Source for credential-free cassette replay. Each
// Next call yields one CollectedGeneration for the next scope in the cassette
// file. When all scopes are exhausted the source returns ok=false so the
// collector.Service poll loop waits for the next poll interval, then restarts
// from the first scope on the following poll.
//
// Source performs no network calls and requires no credentials. It is
// single-goroutine per collector.Service; it is not safe for concurrent Next
// calls.
type Source struct {
	// File is the parsed cassette document. Use NewSource to load from a path.
	File File

	scopeIndex int
	drained    bool
}

// NewSource loads a cassette from path and returns a ready-to-use Source.
func NewSource(path string) (*Source, error) {
	f, err := LoadFile(path)
	if err != nil {
		return nil, err
	}
	return &Source{File: f}, nil
}

// Next emits the next scope generation from the cassette. Returns ok=false when
// the cassette is exhausted so the service can wait for the next poll interval,
// then restarts the batch.
func (s *Source) Next(_ context.Context) (collector.CollectedGeneration, bool, error) {
	if s.drained {
		s.drained = false
		s.scopeIndex = 0
		return collector.CollectedGeneration{}, false, nil
	}
	if s.scopeIndex >= len(s.File.Scopes) {
		s.drained = true
		return collector.CollectedGeneration{}, false, nil
	}

	scopeCfg := s.File.Scopes[s.scopeIndex]
	s.scopeIndex++
	if s.scopeIndex >= len(s.File.Scopes) {
		s.drained = true
	}

	gen, err := s.collectScope(scopeCfg)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	return gen, true, nil
}

func (s *Source) collectScope(sc Scope) (collector.CollectedGeneration, error) {
	scopeValue := scope.IngestionScope{
		ScopeID:       sc.ScopeID,
		SourceSystem:  sc.SourceSystem,
		ScopeKind:     scope.ScopeKind(sc.ScopeKind),
		CollectorKind: scope.CollectorKind(sc.CollectorKind),
		PartitionKey:  sc.partitionKey(),
		Metadata:      sc.Metadata,
	}
	generationValue := scope.ScopeGeneration{
		GenerationID: sc.GenerationID,
		ScopeID:      sc.ScopeID,
		ObservedAt:   sc.ObservedAt,
		IngestedAt:   sc.ObservedAt,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKind(sc.triggerKind()),
	}

	envelopes := make([]facts.Envelope, 0, len(sc.Facts))
	for i, fct := range sc.Facts {
		env, err := s.buildEnvelope(sc, fct)
		if err != nil {
			return collector.CollectedGeneration{}, fmt.Errorf("scope %q fact[%d]: %w", sc.ScopeID, i, err)
		}
		envelopes = append(envelopes, env)
	}

	return collector.FactsFromSlice(scopeValue, generationValue, envelopes), nil
}

func (s *Source) buildEnvelope(sc Scope, fct Fact) (facts.Envelope, error) {
	ck := fct.collectorKind(sc.CollectorKind)

	// Derive the FactID from the stable key if the cassette did not provide one.
	factID := DeriveFactID(sc.ScopeID, sc.GenerationID, fct.StableFactKey)

	payload := fct.Payload
	if payload == nil {
		payload = map[string]any{}
	}

	return facts.Envelope{
		FactID:           factID,
		ScopeID:          sc.ScopeID,
		GenerationID:     sc.GenerationID,
		FactKind:         fct.FactKind,
		StableFactKey:    fct.StableFactKey,
		SchemaVersion:    fct.SchemaVersion,
		CollectorKind:    strings.TrimSpace(ck),
		FencingToken:     fct.fencingToken(),
		SourceConfidence: fct.sourceConfidence(),
		ObservedAt:       sc.ObservedAt,
		Payload:          payload,
		IsTombstone:      fct.IsTombstone,
		SourceRef: facts.Ref{
			SourceSystem:   sc.SourceSystem,
			ScopeID:        sc.ScopeID,
			GenerationID:   sc.GenerationID,
			FactKey:        fct.StableFactKey,
			SourceURI:      strings.TrimSpace(fct.SourceURI),
			SourceRecordID: fct.sourceRecordID(),
		},
	}, nil
}
