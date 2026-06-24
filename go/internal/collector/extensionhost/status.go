// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

func (s *Source) recordStatuses(ctx context.Context, item workflow.WorkItem, result sdkcollector.Result) error {
	if s.statusRecorder == nil {
		return nil
	}
	for _, status := range result.Statuses {
		record := StatusRecord{
			ComponentID:       s.manifest.Metadata.ID,
			InstanceID:        s.collectorInstanceID,
			CollectorKind:     string(item.CollectorKind),
			SourceSystem:      item.SourceSystem,
			ScopeID:           item.ScopeID,
			WorkItemID:        item.WorkItemID,
			GenerationID:      result.Generation.ID,
			State:             result.State,
			Class:             status.Class,
			FailureClass:      strings.TrimSpace(status.FailureClass),
			RetryAfterSeconds: status.RetryAfterSeconds,
			Partial:           status.Partial,
			WarningCount:      status.WarningCount,
			FactCount:         status.FactCount,
			SourceLatencyMS:   status.SourceLatencyMS,
			ObservedAt:        result.Generation.ObservedAt.UTC(),
		}
		if err := s.statusRecorder.RecordExtensionStatus(ctx, record); err != nil {
			return err
		}
	}
	return nil
}
