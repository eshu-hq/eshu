// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
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
		if envelope.FactKind != facts.CICDArtifactFactKind {
			continue
		}
		artifact, err := decodeCICDArtifact(envelope)
		if err != nil {
			continue
		}
		key := cicdRunCorrelationPatchKey{
			provider:   trimmedCICDField(artifact.Provider),
			runID:      trimmedCICDField(artifact.RunID),
			runAttempt: defaultCICDRunAttempt(trimmedCICDPtr(artifact.RunAttempt)),
		}
		if key.provider == "" || key.runID == "" {
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
	encoded, err := json.Marshal(payload)
	if err != nil {
		return CICDRunCorrelationDecision{}, fmt.Errorf("encode correlation payload: %w", err)
	}
	var decoded struct {
		Provider            string                    `json:"provider"`
		RunID               string                    `json:"run_id"`
		RunAttempt          string                    `json:"run_attempt"`
		RepositoryID        string                    `json:"repository_id"`
		CommitSHA           string                    `json:"commit_sha"`
		Environment         string                    `json:"environment"`
		EnvironmentEvidence string                    `json:"environment_evidence"`
		ArtifactDigest      string                    `json:"artifact_digest"`
		ImageRef            string                    `json:"image_ref"`
		Outcome             CICDRunCorrelationOutcome `json:"outcome"`
		Reason              string                    `json:"reason"`
		ProvenanceOnly      bool                      `json:"provenance_only"`
		CanonicalWrites     int                       `json:"canonical_writes"`
		EvidenceFactIDs     []string                  `json:"evidence_fact_ids"`
		CanonicalTarget     string                    `json:"canonical_target"`
		CorrelationKind     string                    `json:"correlation_kind"`
		SourceLayerKinds    []string                  `json:"source_layer_kinds"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return CICDRunCorrelationDecision{}, fmt.Errorf("decode correlation payload: %w", err)
	}
	decision := CICDRunCorrelationDecision{
		Provider:            strings.TrimSpace(decoded.Provider),
		RunID:               strings.TrimSpace(decoded.RunID),
		RunAttempt:          strings.TrimSpace(decoded.RunAttempt),
		RepositoryID:        strings.TrimSpace(decoded.RepositoryID),
		CommitSHA:           strings.TrimSpace(decoded.CommitSHA),
		Environment:         strings.TrimSpace(decoded.Environment),
		EnvironmentEvidence: strings.TrimSpace(decoded.EnvironmentEvidence),
		ArtifactDigest:      strings.TrimSpace(decoded.ArtifactDigest),
		ImageRef:            strings.TrimSpace(decoded.ImageRef),
		Outcome:             decoded.Outcome,
		Reason:              strings.TrimSpace(decoded.Reason),
		ProvenanceOnly:      decoded.ProvenanceOnly,
		CanonicalWrites:     decoded.CanonicalWrites,
		EvidenceFactIDs:     uniqueSortedStrings(decoded.EvidenceFactIDs),
		CanonicalTarget:     strings.TrimSpace(decoded.CanonicalTarget),
		CorrelationKind:     strings.TrimSpace(decoded.CorrelationKind),
		SourceLayerKinds:    uniqueSortedStrings(decoded.SourceLayerKinds),
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
