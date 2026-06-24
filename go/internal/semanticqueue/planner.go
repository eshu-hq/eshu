// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticqueue

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticguard"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
)

const (
	jobIDPrefix      = "semantic-job"
	workItemIDPrefix = "semantic-work"
)

// BuildPlan creates deterministic semantic extraction queue records.
func BuildPlan(request PlanRequest) (Plan, error) {
	request = normalizePlanRequest(request)
	if request.ScopeID == "" {
		return Plan{}, errors.New("scope_id is required")
	}
	if request.GenerationID == "" {
		return Plan{}, errors.New("generation_id is required")
	}
	if err := validateProvider(request.Provider); err != nil {
		return Plan{}, err
	}
	if request.Now.IsZero() {
		request.Now = time.Now().UTC()
	}

	previousByIdentity := previousRecordIndex(request.PreviousRecords)
	currentIdentities := make(map[string]struct{}, len(request.Chunks))
	plannedFingerprints := make(map[string]struct{}, len(request.Chunks))
	var plan Plan

	for _, chunk := range request.Chunks {
		record, identity, err := buildRecord(request, chunk)
		if err != nil {
			return Plan{}, err
		}
		if _, ok := plannedFingerprints[record.Fingerprint]; ok {
			currentIdentities[identity] = struct{}{}
			continue
		}
		plannedFingerprints[record.Fingerprint] = struct{}{}
		currentIdentities[identity] = struct{}{}

		if request.Provider.State == ProviderStateNotConfigured {
			record.Status = StatusSkippedNoProvider
			record.ProviderJob = false
			plan.Skipped = append(plan.Skipped, record)
			plan.Summary.NoProvider++
			continue
		}
		if request.Provider.State == ProviderStateUnavailable {
			record.Status = StatusProviderUnavailable
			record.ProviderJob = false
			record.Retryable = true
			record.Failure = Failure{Class: FailureClassProviderUnavailable}
			plan.Skipped = append(plan.Skipped, record)
			plan.Summary.ProviderUnavailable++
			continue
		}
		if !chunk.Policy.Allowed {
			record.Status = StatusSkippedPolicy
			record.ProviderJob = false
			plan.Skipped = append(plan.Skipped, record)
			plan.Summary.PolicyDenied++
			continue
		}
		if !chunk.Budget.Allowed {
			record.Status = StatusSkippedBudget
			record.ProviderJob = false
			plan.Skipped = append(plan.Skipped, record)
			plan.Summary.BudgetDenied++
			continue
		}
		if !chunk.Guard.Allowed {
			record.Status = StatusUnsafePayload
			record.ProviderJob = false
			plan.Skipped = append(plan.Skipped, record)
			plan.Summary.Unsafe++
			continue
		}

		if previous, ok := previousByIdentity[identity]; ok {
			if previous.Fingerprint == record.Fingerprint {
				previous.Status = StatusSkippedUnchanged
				previous.ScopeID = request.ScopeID
				previous.GenerationID = request.GenerationID
				previous.ProviderJob = false
				previous.UpdatedAt = request.Now
				plan.Skipped = append(plan.Skipped, previous)
				plan.Summary.Unchanged++
				continue
			}
			stale := markStale(previous, request.ScopeID, request.GenerationID, request.Now, StaleReasonSourceChanged)
			plan.Stale = append(plan.Stale, stale)
			plan.Summary.Stale++
			plan.Summary.Changed++
		}

		plan.Jobs = append(plan.Jobs, record)
		plan.Summary.Planned++
	}

	for identity, previous := range previousByIdentity {
		if _, ok := currentIdentities[identity]; ok {
			continue
		}
		stale := markStale(previous, request.ScopeID, request.GenerationID, request.Now, StaleReasonSourceDeleted)
		plan.Stale = append(plan.Stale, stale)
		plan.Summary.Stale++
		plan.Summary.Deleted++
	}

	return plan, nil
}

func buildRecord(request PlanRequest, chunk SourceChunk) (Record, string, error) {
	chunk = normalizeChunk(chunk)
	if err := validateChunk(chunk); err != nil {
		return Record{}, "", err
	}
	sourceIDHash := hashParts("source", chunk.SourceID)
	chunkIDHash := hashParts("chunk", chunk.ChunkID)
	fingerprint := hashParts(
		"fingerprint-v1",
		request.ScopeID,
		chunk.SourceClass,
		chunk.SourceHash,
		chunk.SourceVersion,
		chunk.ChunkHash,
		chunk.NormalizedContentHash,
		chunk.PromptVersion,
		chunk.RedactionVersion,
		chunk.ExtractorVersion,
		request.Provider.ProviderProfileID,
		chunk.ExtractionMode,
		chunk.Policy.PolicyID,
		chunk.Policy.RuleID,
	)
	identity := sourceIDHash + ":" + chunkIDHash
	now := request.Now.UTC()
	return Record{
		JobID:                jobIDPrefix + ":" + hashParts("job", fingerprint),
		WorkItemID:           workItemIDPrefix + ":" + hashParts("work", request.ScopeID, request.GenerationID, fingerprint),
		Fingerprint:          fingerprint,
		ScopeID:              request.ScopeID,
		GenerationID:         request.GenerationID,
		SourceClass:          chunk.SourceClass,
		SourceIDHash:         sourceIDHash,
		ChunkIDHash:          chunkIDHash,
		SourceHash:           chunk.SourceHash,
		ChunkHash:            chunk.ChunkHash,
		SourceVersion:        chunk.SourceVersion,
		PromptVersion:        chunk.PromptVersion,
		RedactionVersion:     chunk.RedactionVersion,
		ExtractorVersion:     chunk.ExtractorVersion,
		ExtractionMode:       chunk.ExtractionMode,
		ProviderKind:         request.Provider.ProviderKind,
		ProviderProfileID:    request.Provider.ProviderProfileID,
		ProviderProfileClass: request.Provider.ProfileClass,
		PolicyID:             chunk.Policy.PolicyID,
		RuleID:               chunk.Policy.RuleID,
		PolicyState:          chunk.Policy.State,
		PolicyReason:         chunk.Policy.Reason,
		GuardState:           chunk.Guard.State,
		GuardReason:          chunk.Guard.Reason,
		ActorClass:           chunk.Guard.ActorClass,
		ACLState:             chunk.Guard.ACLState,
		ClassifierVersion:    chunk.Guard.ClassifierVersion,
		Budget:               chunk.Budget,
		Status:               StatusPending,
		ProviderJob:          true,
		CreatedAt:            now,
		UpdatedAt:            now,
	}, identity, nil
}

func previousRecordIndex(records []Record) map[string]Record {
	byIdentity := make(map[string]Record, len(records))
	for _, record := range records {
		record.SourceIDHash = strings.TrimSpace(record.SourceIDHash)
		record.ChunkIDHash = strings.TrimSpace(record.ChunkIDHash)
		if record.SourceIDHash == "" || record.ChunkIDHash == "" {
			continue
		}
		byIdentity[record.SourceIDHash+":"+record.ChunkIDHash] = record
	}
	return byIdentity
}

func markStale(record Record, scopeID string, generationID string, now time.Time, reason string) Record {
	record.Status = StatusStale
	record.ScopeID = scopeID
	record.GenerationID = generationID
	record.ProviderJob = false
	record.Retryable = false
	record.StaleReason = reason
	record.UpdatedAt = now.UTC()
	staleAt := now.UTC()
	record.StaleAt = &staleAt
	return record
}

func validateChunk(chunk SourceChunk) error {
	missing := []string{}
	required := map[string]string{
		"source_id":               chunk.SourceID,
		"source_class":            chunk.SourceClass,
		"source_hash":             chunk.SourceHash,
		"source_version":          chunk.SourceVersion,
		"chunk_id":                chunk.ChunkID,
		"chunk_hash":              chunk.ChunkHash,
		"normalized_content_hash": chunk.NormalizedContentHash,
		"prompt_version":          chunk.PromptVersion,
		"redaction_version":       chunk.RedactionVersion,
		"extractor_version":       chunk.ExtractorVersion,
		"extraction_mode":         chunk.ExtractionMode,
	}
	for name, value := range required {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("semantic chunk missing required provenance: %s", strings.Join(missing, ", "))
	}
	if chunk.Policy.ProviderProfileID != "" && chunk.Policy.ProviderProfileID != chunk.Guard.ProviderProfileID {
		return errors.New("semantic chunk policy and guard provider profile mismatch")
	}
	return nil
}

func validateProvider(provider Provider) error {
	switch provider.State {
	case ProviderStateReady:
		if provider.ProviderProfileID == "" {
			return errors.New("provider_profile_id is required when semantic provider is ready")
		}
	case ProviderStateNotConfigured, ProviderStateUnavailable:
		return nil
	default:
		return fmt.Errorf("semantic provider state %q is unsupported", provider.State)
	}
	return nil
}

func normalizePlanRequest(request PlanRequest) PlanRequest {
	request.ScopeID = strings.TrimSpace(request.ScopeID)
	request.GenerationID = strings.TrimSpace(request.GenerationID)
	request.Provider.State = ProviderState(strings.TrimSpace(string(request.Provider.State)))
	if request.Provider.State == "" {
		request.Provider.State = ProviderStateNotConfigured
	}
	request.Provider.ProviderKind = strings.TrimSpace(request.Provider.ProviderKind)
	request.Provider.ProviderProfileID = strings.TrimSpace(request.Provider.ProviderProfileID)
	request.Provider.ProfileClass = strings.TrimSpace(request.Provider.ProfileClass)
	return request
}

func normalizeChunk(chunk SourceChunk) SourceChunk {
	chunk.SourceID = strings.TrimSpace(chunk.SourceID)
	chunk.SourceClass = strings.TrimSpace(chunk.SourceClass)
	chunk.SourceHash = strings.TrimSpace(chunk.SourceHash)
	chunk.SourceVersion = strings.TrimSpace(chunk.SourceVersion)
	chunk.ChunkID = strings.TrimSpace(chunk.ChunkID)
	chunk.ChunkHash = strings.TrimSpace(chunk.ChunkHash)
	chunk.NormalizedContentHash = strings.TrimSpace(chunk.NormalizedContentHash)
	chunk.PromptVersion = strings.TrimSpace(chunk.PromptVersion)
	chunk.RedactionVersion = strings.TrimSpace(chunk.RedactionVersion)
	chunk.ExtractorVersion = strings.TrimSpace(chunk.ExtractorVersion)
	chunk.ExtractionMode = strings.TrimSpace(chunk.ExtractionMode)
	chunk.Policy = normalizePolicy(chunk.Policy)
	chunk.Guard = normalizeGuard(chunk.Guard)
	chunk.Budget.State = strings.TrimSpace(chunk.Budget.State)
	chunk.Budget.Reason = strings.TrimSpace(chunk.Budget.Reason)
	chunk.Budget.BudgetUnit = strings.TrimSpace(chunk.Budget.BudgetUnit)
	chunk.Budget.BudgetWindow = strings.TrimSpace(chunk.Budget.BudgetWindow)
	return chunk
}

func normalizePolicy(policy PolicyDecision) PolicyDecision {
	policy.State = strings.TrimSpace(policy.State)
	policy.Reason = strings.TrimSpace(policy.Reason)
	policy.PolicyID = strings.TrimSpace(policy.PolicyID)
	policy.RuleID = strings.TrimSpace(policy.RuleID)
	policy.ProviderProfileID = strings.TrimSpace(policy.ProviderProfileID)
	policy.SourceClass = strings.TrimSpace(policy.SourceClass)
	if policy.State == "" && policy.Allowed {
		policy.State = "allowed"
	}
	if policy.Reason == "" && policy.Allowed {
		policy.Reason = semanticpolicy.ReasonAllowed
	}
	return policy
}

func normalizeGuard(guard GuardDecision) GuardDecision {
	guard.State = strings.TrimSpace(guard.State)
	guard.Reason = strings.TrimSpace(guard.Reason)
	guard.PolicyID = strings.TrimSpace(guard.PolicyID)
	guard.RuleID = strings.TrimSpace(guard.RuleID)
	guard.ProviderProfileID = strings.TrimSpace(guard.ProviderProfileID)
	guard.SourceClass = strings.TrimSpace(guard.SourceClass)
	guard.ActorClass = strings.TrimSpace(guard.ActorClass)
	guard.ACLState = strings.TrimSpace(guard.ACLState)
	guard.ClassifierVersion = strings.TrimSpace(guard.ClassifierVersion)
	guard.SourceHash = strings.TrimSpace(guard.SourceHash)
	guard.ChunkHash = strings.TrimSpace(guard.ChunkHash)
	if guard.State == "" && guard.Allowed {
		guard.State = semanticguard.StateAllowed
	}
	if guard.Reason == "" && guard.Allowed {
		guard.Reason = semanticguard.ReasonAllowed
	}
	return guard
}

func hashParts(parts ...string) string {
	hasher := sha256.New()
	for _, part := range parts {
		_, _ = hasher.Write([]byte(strings.TrimSpace(part)))
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}
