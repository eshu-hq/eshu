// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/replay"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const oduSchemaVersion = "1"

// FactLoader is the durable fact-loading seam Ifá uses for contract-layer Odù.
type FactLoader interface {
	LoadFacts(context.Context, projector.ScopeGenerationWork) ([]facts.Envelope, error)
}

// Odu is one scenario-level Ifá conformance case at the fact-envelope seam.
type Odu struct {
	Name  string
	Work  *projector.ScopeGenerationWork
	Facts []facts.Envelope
}

// CanonicalizeOdu renders odu into replay's deterministic canonical JSON form.
func CanonicalizeOdu(ctx context.Context, odu Odu, loader FactLoader) ([]byte, error) {
	factsForOdu, err := factsFromOdu(ctx, odu, loader)
	if err != nil {
		return nil, err
	}
	doc := map[string]any{
		"odu":            odu.Name,
		"schema_version": oduSchemaVersion,
		"scopes":         renderScopes(odu, factsForOdu),
	}
	canonical, err := replay.CanonicalizeValue(doc, replay.DefaultCanonicalOptions())
	if err != nil {
		return nil, fmt.Errorf("canonicalize odu %q: %w", odu.Name, err)
	}
	return canonical, nil
}

func factsFromOdu(ctx context.Context, odu Odu, loader FactLoader) ([]facts.Envelope, error) {
	if odu.Work == nil {
		return cloneFacts(odu.Facts), nil
	}
	if loader == nil {
		return nil, fmt.Errorf("load facts for odu %q: fact loader is required when work is set", odu.Name)
	}
	loaded, err := loader.LoadFacts(ctx, *odu.Work)
	if err != nil {
		return nil, fmt.Errorf("load facts for odu %q: %w", odu.Name, err)
	}
	return cloneFacts(loaded), nil
}

func cloneFacts(input []facts.Envelope) []facts.Envelope {
	out := make([]facts.Envelope, len(input))
	for i := range input {
		out[i] = input[i].Clone()
	}
	return out
}

func renderScopes(odu Odu, input []facts.Envelope) []any {
	byScope := make(map[string][]facts.Envelope)
	for _, fact := range input {
		byScope[fact.ScopeGenerationKey()] = append(byScope[fact.ScopeGenerationKey()], fact)
	}
	keys := make([]string, 0, len(byScope))
	for key := range byScope {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	scopes := make([]any, 0, len(keys))
	for _, key := range keys {
		scopeFacts := byScope[key]
		sort.Slice(scopeFacts, func(i, j int) bool {
			return scopeFacts[i].StableFactKey < scopeFacts[j].StableFactKey
		})
		scopes = append(scopes, renderScope(odu, scopeFacts))
	}
	return scopes
}

func renderScope(odu Odu, scopeFacts []facts.Envelope) map[string]any {
	first := scopeFacts[0]
	scopeValue := scope.IngestionScope{}
	generationValue := scope.ScopeGeneration{}
	if odu.Work != nil && odu.Work.Scope.ScopeID == first.ScopeID && odu.Work.Generation.GenerationID == first.GenerationID {
		scopeValue = odu.Work.Scope
		generationValue = odu.Work.Generation
	}

	renderedFacts := make([]any, 0, len(scopeFacts))
	for _, fact := range scopeFacts {
		renderedFacts = append(renderedFacts, renderFact(fact))
	}
	return map[string]any{
		"collector_kind": stringValue(string(scopeValue.CollectorKind), first.CollectorKind),
		"facts":          renderedFacts,
		"generation_id":  stringValue(generationValue.GenerationID, first.GenerationID),
		"observed_at":    timeString(generationValue.ObservedAt, first.ObservedAt),
		"scope_id":       first.ScopeID,
		"scope_kind":     string(scopeValue.ScopeKind),
		"source_system":  stringValue(scopeValue.SourceSystem, first.SourceRef.SourceSystem),
	}
}

func renderFact(fact facts.Envelope) map[string]any {
	return map[string]any{
		"collector_kind":    fact.CollectorKind,
		"fact_id":           fact.FactID,
		"fact_kind":         fact.FactKind,
		"fencing_token":     fact.FencingToken,
		"generation_id":     fact.GenerationID,
		"is_tombstone":      fact.IsTombstone,
		"observed_at":       timeString(fact.ObservedAt),
		"payload":           fact.Payload,
		"schema_version":    fact.SchemaVersion,
		"scope_id":          fact.ScopeID,
		"source_confidence": fact.SourceConfidence,
		"source_ref":        renderSourceRef(fact.SourceRef),
		"stable_fact_key":   fact.StableFactKey,
	}
}

func renderSourceRef(ref facts.Ref) map[string]any {
	return map[string]any{
		"fact_key":         ref.FactKey,
		"generation_id":    ref.GenerationID,
		"scope_id":         ref.ScopeID,
		"source_record_id": ref.SourceRecordID,
		"source_system":    ref.SourceSystem,
		"source_uri":       ref.SourceURI,
	}
}

func stringValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func timeString(values ...time.Time) string {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC().Format(time.RFC3339Nano)
		}
	}
	return ""
}
