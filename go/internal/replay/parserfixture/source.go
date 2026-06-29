// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parserfixture

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Compile-time proof that the parser-fixture flavor satisfies the shared
// replay.Source contract (which embeds collector.Source), so it drops into the
// same collector.Service poll loop as the live collector.
var _ replay.Source = (*Source)(nil)

// stableIDNamespace is the facts.StableID namespace the Git collector uses for
// its fact envelopes. The replay side re-derives FactID with the same namespace
// and key set so a replayed envelope's FactID is byte-equal to the live one.
const stableIDNamespace = "GoGitCollectorFact"

// Source implements collector.Source for credential-free parser-fixture replay.
// Each Next call yields one CollectedGeneration for the fixture's single scope,
// then returns ok=false so the collector.Service poll loop waits for the next
// poll interval and restarts. Source makes no network calls, runs no parser, and
// needs no source tree on disk: it reproduces the recorded envelopes (including
// provenance) directly from the fixture. It is single-goroutine per
// collector.Service; Next is not safe for concurrent use.
type Source struct {
	// File is the parsed parser-fixture document. Use NewSource to load from path.
	File File

	drained bool
}

// NewSource loads a parser fixture from path and returns a ready Source. Use it
// for non-portable (absolute-path) fixtures such as a temp-dir recording.
func NewSource(path string) (*Source, error) {
	f, err := LoadFile(path)
	if err != nil {
		return nil, err
	}
	return &Source{File: f}, nil
}

// NewSourceRehydrated loads a portable committed fixture from path, rehydrating
// its repo-root sentinel against repoRoot, and returns a ready Source. This is
// the replay entry point for committed fixtures, whose provenance and payload
// paths are stored machine-independently and must be rebound to the local
// checkout to reproduce the live parser's absolute paths exactly.
func NewSourceRehydrated(path, repoRoot string) (*Source, error) {
	f, err := LoadFileRehydrated(path, repoRoot)
	if err != nil {
		return nil, err
	}
	return &Source{File: f}, nil
}

// Next emits the fixture's single scope generation, then signals exhaustion so
// the service waits for the next poll interval and restarts the batch.
func (s *Source) Next(_ context.Context) (collector.CollectedGeneration, bool, error) {
	if s.drained {
		s.drained = false
		return collector.CollectedGeneration{}, false, nil
	}
	s.drained = true

	gen, err := s.collectScope(s.File.Scope)
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
		PartitionKey:  sc.ScopeID,
	}
	generationValue := scope.ScopeGeneration{
		GenerationID: sc.GenerationID,
		ScopeID:      sc.ScopeID,
		ObservedAt:   sc.ObservedAt,
		IngestedAt:   sc.ObservedAt,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKind("snapshot"),
	}

	envelopes := make([]facts.Envelope, 0, len(sc.Facts))
	for i, fct := range sc.Facts {
		env, err := buildEnvelope(sc, fct)
		if err != nil {
			return collector.CollectedGeneration{}, fmt.Errorf("scope %q fact[%d]: %w", sc.ScopeID, i, err)
		}
		envelopes = append(envelopes, env)
	}
	return collector.FactsFromSlice(scopeValue, generationValue, envelopes), nil
}

// buildEnvelope reconstructs one fact envelope from a recorded fixture fact,
// re-deriving the FactID with the same namespace and key set the Git collector
// uses so the replayed envelope is identical to the live one — including its
// SourceRef provenance.
func buildEnvelope(sc Scope, fct Fact) (facts.Envelope, error) {
	factID := facts.StableID(stableIDNamespace, map[string]any{
		"fact_key":      fct.StableFactKey,
		"fact_kind":     fct.FactKind,
		"generation_id": sc.GenerationID,
		"scope_id":      sc.ScopeID,
	})

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
		CollectorKind:    strings.TrimSpace(fct.collectorKind(sc.CollectorKind)),
		FencingToken:     fct.fencingToken(),
		SourceConfidence: fct.sourceConfidence(),
		ObservedAt:       sc.ObservedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   fct.sourceSystem(sc.SourceSystem),
			ScopeID:        sc.ScopeID,
			GenerationID:   sc.GenerationID,
			FactKey:        fct.StableFactKey,
			SourceURI:      strings.TrimSpace(fct.SourceURI),
			SourceRecordID: fct.sourceRecordID(),
		},
	}, nil
}
