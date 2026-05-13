package ociruntime

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ClaimedSource resolves workflow-owned OCI registry work items into one
// bounded repository scan.
type ClaimedSource struct {
	Source Source
}

// NextClaimed scans the configured target whose scope_id matches the claimed
// work item. The work item's generation_id is reused so claim retries are
// idempotent at the workflow boundary.
func (s ClaimedSource) NextClaimed(ctx context.Context, item workflow.WorkItem) (collector.CollectedGeneration, bool, error) {
	config, err := s.Source.Config.validated()
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if s.Source.ClientFactory == nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("OCI registry client factory is required")
	}
	if item.CollectorKind != scope.CollectorOCIRegistry {
		return collector.CollectedGeneration{}, false, fmt.Errorf("OCI registry claimed source requires collector_kind %q", scope.CollectorOCIRegistry)
	}
	if item.CollectorInstanceID != config.CollectorInstanceID {
		return collector.CollectedGeneration{}, false, fmt.Errorf("claimed collector_instance_id %q does not match OCI runtime %q", item.CollectorInstanceID, config.CollectorInstanceID)
	}
	var matched *TargetConfig
	for _, target := range config.Targets {
		scopeID, err := targetScopeID(target)
		if err != nil {
			return collector.CollectedGeneration{}, false, err
		}
		if scopeID != item.ScopeID {
			continue
		}
		if matched != nil {
			return collector.CollectedGeneration{}, false, fmt.Errorf("OCI registry claimed source found multiple configured targets for scope_id %q", item.ScopeID)
		}
		targetCopy := target
		matched = &targetCopy
	}
	if matched == nil {
		return collector.CollectedGeneration{}, false, nil
	}
	matched.FencingToken = item.CurrentFencingToken
	collected, err := s.Source.scanTarget(ctx, config, *matched, item.GenerationID)
	return collected, true, err
}

func targetScopeID(target TargetConfig) (string, error) {
	identity, err := ociregistry.NormalizeRepositoryIdentity(target.identity())
	if err != nil {
		return "", err
	}
	return identity.ScopeID, nil
}
