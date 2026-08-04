// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type cicdRunCorrelationPatchFactLoader interface {
	ListCICDRunFactsForRunKeys(
		ctx context.Context,
		scopeID string,
		targetGenerationID string,
		providers []string,
		runIDs []string,
		runAttempts []string,
	) ([]facts.Envelope, error)
	ListPreviousCICDRunCorrelationFacts(
		ctx context.Context,
		scopeID string,
		targetGenerationID string,
	) ([]facts.Envelope, error)
}

type cicdRunCorrelationPatchKey struct {
	provider   string
	runID      string
	runAttempt string
}

// loadCICDRunCorrelationPatchFacts expands an artifact-only generation into a
// bounded domain patch. Normal generations containing ci.run remain exact
// generation snapshots and do not make either historical query.
func (h CICDRunCorrelationHandler) loadCICDRunCorrelationPatchFacts(
	ctx context.Context,
	intent Intent,
	current []facts.Envelope,
) ([]facts.Envelope, []facts.Envelope, bool, error) {
	if hasCICDFactKind(current, facts.CICDRunFactKind) || !hasCICDFactKind(current, facts.CICDArtifactFactKind) {
		return current, nil, false, nil
	}
	loader, ok := h.FactLoader.(cicdRunCorrelationPatchFactLoader)
	if !ok {
		return nil, nil, true, fmt.Errorf("load ci/cd run correlation patch: fact loader does not support cross-generation evidence")
	}

	providers, runIDs, runAttempts := cicdArtifactPatchRunKeys(current)
	historical, err := loader.ListCICDRunFactsForRunKeys(
		ctx,
		intent.ScopeID,
		intent.GenerationID,
		providers,
		runIDs,
		runAttempts,
	)
	if err != nil {
		return nil, nil, true, fmt.Errorf("load historical ci/cd run facts: %w", classifyFactLoadError(err))
	}
	historical = excludeSupersededCICDArtifacts(historical, current)
	previous, err := loader.ListPreviousCICDRunCorrelationFacts(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return nil, nil, true, fmt.Errorf("load previous ci/cd run correlation facts: %w", classifyFactLoadError(err))
	}
	combined := make([]facts.Envelope, 0, len(historical)+len(current))
	combined = append(combined, historical...)
	combined = append(combined, current...)
	return combined, previous, true, nil
}

func hasCICDFactKind(envelopes []facts.Envelope, factKind string) bool {
	for _, envelope := range envelopes {
		if envelope.FactKind == factKind {
			return true
		}
	}
	return false
}

func cicdArtifactPatchRunKeys(envelopes []facts.Envelope) ([]string, []string, []string) {
	unique := make(map[cicdRunCorrelationPatchKey]struct{})
	for _, envelope := range envelopes {
		key, ok := cicdArtifactPatchKeyFromEnvelope(envelope)
		if !ok {
			continue
		}
		unique[key] = struct{}{}
	}
	keys := make([]cicdRunCorrelationPatchKey, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return cicdRunCorrelationPatchKeyString(keys[i]) < cicdRunCorrelationPatchKeyString(keys[j])
	})
	providers := make([]string, 0, len(keys))
	runIDs := make([]string, 0, len(keys))
	runAttempts := make([]string, 0, len(keys))
	for _, key := range keys {
		providers = append(providers, key.provider)
		runIDs = append(runIDs, key.runID)
		runAttempts = append(runAttempts, key.runAttempt)
	}
	return providers, runIDs, runAttempts
}

// excludeSupersededCICDArtifacts keeps current-generation artifact facts
// authoritative for every patched run. Retained run, environment, trigger,
// step, and deployment evidence still participates in the recomputation.
func excludeSupersededCICDArtifacts(historical, current []facts.Envelope) []facts.Envelope {
	currentKeys := make(map[cicdRunCorrelationPatchKey]struct{})
	for _, envelope := range current {
		key, ok := cicdArtifactPatchKeyFromEnvelope(envelope)
		if ok {
			currentKeys[key] = struct{}{}
		}
	}
	filtered := make([]facts.Envelope, 0, len(historical))
	for _, envelope := range historical {
		key, ok := cicdArtifactPatchKeyFromEnvelope(envelope)
		if ok {
			if _, superseded := currentKeys[key]; superseded {
				continue
			}
		}
		filtered = append(filtered, envelope)
	}
	return filtered
}

func cicdArtifactPatchKeyFromEnvelope(envelope facts.Envelope) (cicdRunCorrelationPatchKey, bool) {
	if envelope.FactKind != facts.CICDArtifactFactKind {
		return cicdRunCorrelationPatchKey{}, false
	}
	artifact, err := decodeCICDArtifact(envelope)
	if err != nil {
		return cicdRunCorrelationPatchKey{}, false
	}
	key := cicdRunCorrelationPatchKey{
		provider:   trimmedCICDField(artifact.Provider),
		runID:      trimmedCICDField(artifact.RunID),
		runAttempt: defaultCICDRunAttempt(trimmedCICDPtr(artifact.RunAttempt)),
	}
	return key, key.provider != "" && key.runID != ""
}

func mergeCICDRunCorrelationPatchDecisions(
	previousFacts []facts.Envelope,
	patched []CICDRunCorrelationDecision,
) ([]CICDRunCorrelationDecision, error) {
	merged := make(map[string]CICDRunCorrelationDecision, len(previousFacts)+len(patched))
	for _, envelope := range previousFacts {
		decision, err := cicdRunCorrelationDecisionFromPayload(envelope.Payload)
		if err != nil {
			return nil, fmt.Errorf("decode previous correlation fact %q: %w", envelope.FactID, err)
		}
		key := cicdRunCorrelationDecisionKey(decision)
		if _, exists := merged[key]; exists {
			return nil, fmt.Errorf("duplicate previous correlation decision for %q", key)
		}
		merged[key] = decision
	}
	for _, decision := range patched {
		key := cicdRunCorrelationDecisionKey(decision)
		merged[key] = decision
	}
	out := make([]CICDRunCorrelationDecision, 0, len(merged))
	for _, decision := range merged {
		out = append(out, decision)
	}
	sort.Slice(out, func(i, j int) bool {
		return cicdRunCorrelationDecisionKey(out[i]) < cicdRunCorrelationDecisionKey(out[j])
	})
	return out, nil
}

func cicdRunCorrelationDecisionFromPayload(payload map[string]any) (CICDRunCorrelationDecision, error) {
	var decision CICDRunCorrelationDecision
	stringFields := []struct {
		key    string
		target *string
	}{
		{key: "provider", target: &decision.Provider},
		{key: "run_id", target: &decision.RunID},
		{key: "run_attempt", target: &decision.RunAttempt},
		{key: "repository_id", target: &decision.RepositoryID},
		{key: "commit_sha", target: &decision.CommitSHA},
		{key: "environment", target: &decision.Environment},
		{key: "environment_evidence", target: &decision.EnvironmentEvidence},
		{key: "artifact_digest", target: &decision.ArtifactDigest},
		{key: "image_ref", target: &decision.ImageRef},
		{key: "reason", target: &decision.Reason},
		{key: "canonical_target", target: &decision.CanonicalTarget},
		{key: "correlation_kind", target: &decision.CorrelationKind},
	}
	for _, field := range stringFields {
		value, err := cicdRunCorrelationPayloadString(payload, field.key)
		if err != nil {
			return CICDRunCorrelationDecision{}, err
		}
		*field.target = value
	}
	outcome, err := cicdRunCorrelationPayloadString(payload, "outcome")
	if err != nil {
		return CICDRunCorrelationDecision{}, err
	}
	decision.Outcome = CICDRunCorrelationOutcome(outcome)
	decision.ProvenanceOnly, err = cicdRunCorrelationPayloadBool(payload, "provenance_only")
	if err != nil {
		return CICDRunCorrelationDecision{}, err
	}
	decision.CanonicalWrites, err = cicdRunCorrelationPayloadInt(payload, "canonical_writes")
	if err != nil {
		return CICDRunCorrelationDecision{}, err
	}
	decision.EvidenceFactIDs, err = cicdRunCorrelationPayloadStrings(payload, "evidence_fact_ids")
	if err != nil {
		return CICDRunCorrelationDecision{}, err
	}
	decision.SourceLayerKinds, err = cicdRunCorrelationPayloadStrings(payload, "source_layer_kinds")
	if err != nil {
		return CICDRunCorrelationDecision{}, err
	}
	if decision.RunAttempt == "" {
		decision.RunAttempt = defaultCICDRunAttempt("")
	}
	if decision.Provider == "" || decision.RunID == "" {
		return CICDRunCorrelationDecision{}, fmt.Errorf("provider and run_id are required")
	}
	if !isCICDRunCorrelationOutcome(decision.Outcome) {
		return CICDRunCorrelationDecision{}, fmt.Errorf("unsupported outcome %q", decision.Outcome)
	}
	if decision.CanonicalWrites < 0 {
		return CICDRunCorrelationDecision{}, fmt.Errorf("canonical_writes must be non-negative")
	}
	return decision, nil
}

func cicdRunCorrelationPayloadString(payload map[string]any, key string) (string, error) {
	value, exists := payload[key]
	if !exists || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return strings.TrimSpace(text), nil
}

func cicdRunCorrelationPayloadBool(payload map[string]any, key string) (bool, error) {
	value, exists := payload[key]
	if !exists || value == nil {
		return false, nil
	}
	boolean, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return boolean, nil
}

func cicdRunCorrelationPayloadInt(payload map[string]any, key string) (int, error) {
	value, exists := payload[key]
	if !exists || value == nil {
		return 0, nil
	}
	switch number := value.(type) {
	case int:
		return number, nil
	case float64:
		maxInt := int(^uint(0) >> 1)
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number ||
			number >= float64(maxInt) || number <= float64(-maxInt-1) {
			return 0, fmt.Errorf("%s must be an integer", key)
		}
		return int(number), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
}

func cicdRunCorrelationPayloadStrings(payload map[string]any, key string) ([]string, error) {
	value, exists := payload[key]
	if !exists || value == nil {
		return nil, nil
	}
	var stringsValue []string
	switch list := value.(type) {
	case []string:
		stringsValue = append(stringsValue, list...)
	case []any:
		stringsValue = make([]string, 0, len(list))
		for _, item := range list {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must contain only strings", key)
			}
			stringsValue = append(stringsValue, text)
		}
	default:
		return nil, fmt.Errorf("%s must be an array of strings", key)
	}
	return uniqueSortedStrings(stringsValue), nil
}

func cicdRunCorrelationDecisionKey(decision CICDRunCorrelationDecision) string {
	attempt := defaultCICDRunAttempt(decision.RunAttempt)
	return strings.TrimSpace(decision.Provider) + "\x00" + strings.TrimSpace(decision.RunID) + "\x00" + attempt
}

func cicdRunCorrelationPatchKeyString(key cicdRunCorrelationPatchKey) string {
	return key.provider + "\x00" + key.runID + "\x00" + key.runAttempt
}

func isCICDRunCorrelationOutcome(outcome CICDRunCorrelationOutcome) bool {
	for _, candidate := range cicdRunCorrelationOutcomes() {
		if outcome == candidate {
			return true
		}
	}
	return false
}
