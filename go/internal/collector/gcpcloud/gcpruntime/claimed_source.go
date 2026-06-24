// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// NextClaimed implements collector.ClaimedSource for claim-driven GCP Cloud
// Asset Inventory work. The static config authorizes scopes and credential
// references; the claimed work item supplies generation and fencing identity.
func (s *Source) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	scopeCfg, err := s.scopeForClaim(item)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	collected, err := s.collectScope(ctx, scopeCfg)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	return collected, true, nil
}

func (s *Source) scopeForClaim(item workflow.WorkItem) (ScopeConfig, error) {
	if strings.TrimSpace(s.Config.CollectorInstanceID) == "" {
		return ScopeConfig{}, fmt.Errorf("gcp collector instance id is required")
	}
	if s.Provider == nil {
		return ScopeConfig{}, errors.New("gcp collector page provider is required")
	}
	if s.RedactionKey.IsZero() {
		return ScopeConfig{}, errors.New("gcp collector redaction key is required")
	}
	if item.CollectorKind != scope.CollectorGCP {
		return ScopeConfig{}, fmt.Errorf("claimed collector_kind %q must be %q", item.CollectorKind, scope.CollectorGCP)
	}
	if strings.TrimSpace(item.SourceSystem) != string(scope.CollectorGCP) {
		return ScopeConfig{}, fmt.Errorf("claimed source_system %q must be %q", item.SourceSystem, scope.CollectorGCP)
	}
	if strings.TrimSpace(item.CollectorInstanceID) != s.Config.CollectorInstanceID {
		return ScopeConfig{}, fmt.Errorf("claimed collector_instance_id %q must match configured GCP collector instance", item.CollectorInstanceID)
	}
	if item.Status != workflow.WorkItemStatusClaimed {
		return ScopeConfig{}, fmt.Errorf("claimed GCP work item must have claimed status")
	}
	scopeID := strings.TrimSpace(item.ScopeID)
	if scopeID == "" {
		return ScopeConfig{}, fmt.Errorf("claimed GCP scope_id is required")
	}
	if acceptance := strings.TrimSpace(item.AcceptanceUnitID); acceptance != "" && acceptance != scopeID {
		return ScopeConfig{}, fmt.Errorf("claimed GCP acceptance_unit_id must match scope_id")
	}
	if item.CurrentFencingToken <= 0 {
		return ScopeConfig{}, fmt.Errorf("claimed GCP fencing token must be positive")
	}
	generationID := strings.TrimSpace(item.GenerationID)
	if generationID == "" || strings.TrimSpace(item.SourceRunID) == "" {
		return ScopeConfig{}, fmt.Errorf("claimed GCP generation identity is required")
	}
	if strings.TrimSpace(item.SourceRunID) != generationID {
		return ScopeConfig{}, fmt.Errorf("claimed GCP source_run_id must match generation_id")
	}

	for _, candidate := range s.Config.Scopes {
		resolved := candidate.withDefaults()
		if resolved.ScopeID != scopeID {
			continue
		}
		resolved.GenerationID = generationID
		resolved.FencingToken = item.CurrentFencingToken
		if err := resolved.validate(); err != nil {
			return ScopeConfig{}, fmt.Errorf("claimed GCP scope configuration: %w", err)
		}
		return resolved, nil
	}
	return ScopeConfig{}, fmt.Errorf("gcp claim scope_id is not configured")
}

var _ collector.ClaimedSource = (*Source)(nil)
