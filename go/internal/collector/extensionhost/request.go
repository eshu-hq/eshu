// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"fmt"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

func (s *Source) requestForWorkItem(item workflow.WorkItem) Request {
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
		},
		Contract: s.contract,
		Config:   cloneConfig(s.config),
	}
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
		got.ConfigHandle != want.ConfigHandle {
		return fmt.Errorf("extension returned claim identity that does not match host claim")
	}
	return nil
}
