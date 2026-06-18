package azureruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// NextClaimed implements collector.ClaimedSource for claim-driven Azure Resource
// Graph work. The static config authorizes scope targets and credential
// references; the claimed work item supplies the coordinator-assigned generation
// and fencing identity. The source mutates no Azure state and reads only the
// authorized bounded scope; an unauthorized or stale claim is rejected before
// any provider call.
func (s *Source) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	target, generationID, err := s.targetForClaim(item)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	collected, err := s.scanTarget(ctx, s.Config.CollectorInstanceID, target, generationID)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	return collected, true, nil
}

// targetForClaim validates the claimed work item against the configured Azure
// collector instance and resolves the single authorized scope target. It returns
// the target with the claim's fencing token applied and the coordinator-assigned
// generation id. Errors never embed configured provider identity so claim
// rejection telemetry stays bounded.
func (s *Source) targetForClaim(item workflow.WorkItem) (TargetConfig, string, error) {
	config, err := s.Config.validated()
	if err != nil {
		return TargetConfig{}, "", err
	}
	if s.ProviderFactory == nil {
		return TargetConfig{}, "", errors.New("azure page provider factory is required")
	}
	if s.RedactionKey.IsZero() {
		// Claimed-live collection is the production path; a zero key would emit
		// tag observation facts with an unkeyed marker. The binary rejects a
		// blank key file, and the source enforces the same contract so any direct
		// caller cannot bypass it (parity with the GCP claimed source).
		return TargetConfig{}, "", errors.New("azure claimed source requires a non-zero redaction key")
	}
	if item.CollectorKind != scope.CollectorAzure {
		return TargetConfig{}, "", fmt.Errorf("claimed collector_kind %q must be %q", item.CollectorKind, scope.CollectorAzure)
	}
	if strings.TrimSpace(item.SourceSystem) != azurecloud.CollectorKind {
		return TargetConfig{}, "", fmt.Errorf("claimed source_system %q must be %q", item.SourceSystem, azurecloud.CollectorKind)
	}
	if strings.TrimSpace(item.CollectorInstanceID) != config.CollectorInstanceID {
		return TargetConfig{}, "", fmt.Errorf("claimed collector_instance_id %q must match the configured azure collector instance", item.CollectorInstanceID)
	}
	if item.Status != workflow.WorkItemStatusClaimed {
		return TargetConfig{}, "", fmt.Errorf("claimed azure work item must have claimed status, got %q", item.Status)
	}
	scopeID := strings.TrimSpace(item.ScopeID)
	if scopeID == "" {
		return TargetConfig{}, "", errors.New("claimed azure scope_id is required")
	}
	if acceptance := strings.TrimSpace(item.AcceptanceUnitID); acceptance != "" && acceptance != scopeID {
		return TargetConfig{}, "", errors.New("claimed azure acceptance_unit_id must match scope_id")
	}
	if item.CurrentFencingToken <= 0 {
		return TargetConfig{}, "", errors.New("claimed azure fencing token must be positive")
	}
	generationID := strings.TrimSpace(item.GenerationID)
	if generationID == "" || strings.TrimSpace(item.SourceRunID) == "" {
		return TargetConfig{}, "", errors.New("claimed azure generation identity is required")
	}
	if strings.TrimSpace(item.SourceRunID) != generationID {
		return TargetConfig{}, "", errors.New("claimed azure source_run_id must match generation_id")
	}

	for _, candidate := range config.Targets {
		if scopeIDForTarget(candidate) != scopeID {
			continue
		}
		candidate.FencingToken = item.CurrentFencingToken
		return candidate, generationID, nil
	}
	return TargetConfig{}, "", errors.New("claimed azure scope_id is not configured for this collector instance")
}

var _ collector.ClaimedSource = (*Source)(nil)
