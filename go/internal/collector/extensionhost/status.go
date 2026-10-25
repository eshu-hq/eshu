// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"strings"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
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

// unknownCollectorKind labels a host-rejected claim whose collector kind the
// manifest does not declare, so an arbitrary kind cannot become a label.
const unknownCollectorKind = "unknown"

// recordResult reports the one ResultRecord for a NextClaimed call. A host
// verdict carries the host failure class and the state ClaimedService applies;
// an SDK result carries its own state and the summed status counts. err is
// always an extensionFailure or nil here, so a type assertion suffices.
func (s *Source) recordResult(
	ctx context.Context,
	item workflow.WorkItem,
	result sdkcollector.Result,
	origin ResultOrigin,
	err error,
) {
	if s.resultRecorder == nil {
		return
	}
	record := ResultRecord{CollectorKind: unknownCollectorKind, Origin: origin}
	if _, ok := s.collectorKinds[item.CollectorKind]; ok {
		record.CollectorKind = string(item.CollectorKind)
	}
	failure, failed := err.(extensionFailure)
	if origin == ResultOriginHost {
		record.State = sdkcollector.ResultRetryable
		if failure.terminal {
			record.State = sdkcollector.ResultTerminal
		}
		record.FailureClass = failure.class
		s.resultRecorder.RecordExtensionResult(ctx, record)
		return
	}
	record.State = result.State
	record.FailureClass = failureClassFromStatus(result, "")
	if failed {
		record.FailureClass = failure.class
		record.RetryAfter = failure.retryAfter
	}
	record.StatusCount = len(result.Statuses)
	for _, status := range result.Statuses {
		record.FactCount += status.FactCount
		record.WarningCount += status.WarningCount
		record.SourceLatencyMS = max(record.SourceLatencyMS, status.SourceLatencyMS)
		record.RetryAfterRequestedSeconds = max(record.RetryAfterRequestedSeconds, status.RetryAfterSeconds)
		record.Partial = record.Partial || status.Partial
	}
	s.resultRecorder.RecordExtensionResult(ctx, record)
}
