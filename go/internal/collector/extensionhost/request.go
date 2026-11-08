// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// requestForWorkItem builds the satellite request for an already-claimed
// work item. When a scope query resolver is configured it delivers the
// run's requested_scope_set entry on the claim (eshu-hq/eshu#9432); a
// resolver error fails the claim before the satellite launches. Without a
// resolver the claim carries no scope queries, byte-identical to the legacy
// shape old satellites read. A configured derived target resolver adds the
// claim's target to its own config clone under HostDerivedTargetKey; its errors
// classify the same way.
func (s *Source) requestForWorkItem(ctx context.Context, item workflow.WorkItem) (Request, error) {
	var transport collector.ResolvedScopeTransport
	if s.scopeQueryResolver != nil {
		resolved, err := s.scopeQueryResolver(ctx, item)
		if err != nil {
			return Request{}, fmt.Errorf("resolve scope queries for claim: %w", err)
		}
		transport = resolved
	}
	config := cloneConfig(s.config)
	if s.derivedTargetResolver != nil {
		target, err := s.derivedTargetResolver(ctx, item)
		if err != nil {
			return Request{}, fmt.Errorf("resolve derived target for claim: %w", err)
		}
		if target != nil {
			if config == nil {
				config = make(map[string]any, 1)
			}
			config[HostDerivedTargetKey] = target
		}
	}
	return Request{
		ProtocolVersion: s.contract.ProtocolVersion,
		Claim: sdkcollector.Claim{
			ComponentID:   s.manifest.Metadata.ID,
			InstanceID:    s.collectorInstanceID,
			CollectorKind: string(item.CollectorKind),
			SourceSystem:  item.SourceSystem,
			Scope: sdkcollector.Scope{
				ID:   item.ScopeID,
				Kind: string(s.scopeKind),
			},
			SourceRunID:  item.SourceRunID,
			GenerationID: item.GenerationID,
			WorkItemID:   item.WorkItemID,
			FencingToken: strconv.FormatInt(item.CurrentFencingToken, 10),
			Attempt:      item.AttemptCount,
			Deadline:     item.LeaseExpiresAt.UTC(),
			ConfigHandle: s.configHandle,
			// The resolver returns the zero transport for every scope
			// without transport, so omitempty keeps the wire
			// byte-identical to the legacy claim on all non-sharded
			// claims.
			ScopeQueries: transport.Queries,
			ScopeCVEIDs:  transport.CVEIDs,
		},
		Contract: s.contract,
		Config:   config,
	}, nil
}

func validateReturnedClaim(want sdkcollector.Claim, got sdkcollector.Claim) error {
	if got.ComponentID != want.ComponentID ||
		got.InstanceID != want.InstanceID ||
		got.CollectorKind != want.CollectorKind ||
		got.SourceSystem != want.SourceSystem ||
		got.Scope.ID != want.Scope.ID ||
		got.Scope.Kind != want.Scope.Kind ||
		got.SourceRunID != want.SourceRunID ||
		got.GenerationID != want.GenerationID ||
		got.WorkItemID != want.WorkItemID ||
		got.FencingToken != want.FencingToken ||
		got.Attempt != want.Attempt ||
		!got.Deadline.Equal(want.Deadline) ||
		got.ConfigHandle != want.ConfigHandle ||
		!slices.Equal(got.ScopeQueries, want.ScopeQueries) ||
		!slices.Equal(got.ScopeCVEIDs, want.ScopeCVEIDs) {
		return fmt.Errorf("extension returned claim identity that does not match host claim")
	}
	return nil
}
