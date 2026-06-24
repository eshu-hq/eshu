// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (r *CodeCallProjectionRunner) loadAllAcceptanceUnitIntents(ctx context.Context, key SharedProjectionAcceptanceKey) ([]SharedProjectionIntentRow, error) {
	return r.loadAcceptanceUnitRows(ctx, key)
}

func (r *CodeCallProjectionRunner) loadAcceptanceUnitPartitionIntents(
	ctx context.Context,
	key SharedProjectionAcceptanceKey,
	partitionKey string,
) ([]SharedProjectionIntentRow, error) {
	if reader, ok := r.IntentReader.(CodeCallProjectionPartitionIntentReader); ok {
		return r.loadAcceptanceUnitPartitionRows(ctx, reader, key, partitionKey)
	}
	rows, err := r.loadAcceptanceUnitRows(ctx, key)
	if err != nil {
		return nil, err
	}
	return codeCallProjectionRowsForPartition(rows, partitionKey), nil
}

func (r *CodeCallProjectionRunner) loadAcceptanceUnitPartitionRows(
	ctx context.Context,
	reader CodeCallProjectionPartitionIntentReader,
	key SharedProjectionAcceptanceKey,
	partitionKey string,
) ([]SharedProjectionIntentRow, error) {
	limit := r.Config.batchLimit()
	acceptanceScanLimit := r.Config.acceptanceScanLimit()
	if limit > acceptanceScanLimit {
		limit = acceptanceScanLimit
	}
	for {
		rows, err := reader.ListPendingAcceptanceUnitPartitionIntents(ctx, key, DomainCodeCalls, partitionKey, limit)
		if err != nil {
			return nil, fmt.Errorf("list pending code call partition intents: %w", err)
		}
		if len(rows) < limit {
			return rows, nil
		}
		if limit >= acceptanceScanLimit {
			return rows, nil
		}
		nextLimit := limit * 2
		if nextLimit > acceptanceScanLimit {
			nextLimit = acceptanceScanLimit
		}
		limit = nextLimit
	}
}

func (r *CodeCallProjectionRunner) loadAcceptanceUnitRows(
	ctx context.Context,
	key SharedProjectionAcceptanceKey,
) ([]SharedProjectionIntentRow, error) {
	limit := r.Config.batchLimit()
	acceptanceScanLimit := r.Config.acceptanceScanLimit()
	if limit > acceptanceScanLimit {
		limit = acceptanceScanLimit
	}
	for {
		rows, err := r.IntentReader.ListPendingAcceptanceUnitIntents(ctx, key, DomainCodeCalls, limit)
		if err != nil {
			return nil, fmt.Errorf("list pending code call acceptance intents: %w", err)
		}
		if len(rows) < limit {
			return rows, nil
		}
		if limit >= acceptanceScanLimit {
			return rows, nil
		}
		nextLimit := limit * 2
		if nextLimit > acceptanceScanLimit {
			nextLimit = acceptanceScanLimit
		}
		limit = nextLimit
	}
}

func (r *CodeCallProjectionRunner) retractRepo(ctx context.Context, rows []SharedProjectionIntentRow) error {
	retractRows := buildCodeCallRetractRows(rows)
	for _, evidenceSource := range codeCallEvidenceSources() {
		if err := r.EdgeWriter.RetractEdges(ctx, DomainCodeCalls, retractRows, evidenceSource); err != nil {
			return fmt.Errorf("retract code call edges for %s: %w", evidenceSource, err)
		}
	}
	return nil
}

func (r *CodeCallProjectionRunner) shouldSkipCodeCallRetract(
	ctx context.Context,
	key SharedProjectionAcceptanceKey,
	partitionKey string,
	active []SharedProjectionIntentRow,
	staleIDs []string,
) (bool, error) {
	if len(staleIDs) > 0 {
		return false, nil
	}
	partitionHistory, ok := r.IntentReader.(CodeCallProjectionCurrentRunPartitionHistoryLookup)
	if ok {
		hasCurrent, err := partitionHistory.HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents(
			ctx,
			key,
			partitionKey,
			DomainCodeCalls,
		)
		if err != nil {
			return false, fmt.Errorf("check completed current code call projection partition history: %w", err)
		}
		if hasCurrent {
			return true, nil
		}
	}
	if codeCallProjectionPartitionKindForKey(partitionKey) == codeCallProjectionPartitionFile {
		refreshHistory, ok := r.IntentReader.(CodeCallProjectionCurrentRunRefreshHistoryLookup)
		if ok {
			filePaths := codeCallProjectionFilePaths(active)
			hasRefresh, err := refreshHistory.HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents(
				ctx,
				key,
				filePaths,
				DomainCodeCalls,
			)
			if err != nil {
				return false, fmt.Errorf("check completed current code call refresh history: %w", err)
			}
			if hasRefresh {
				return true, nil
			}
		}
	}
	history, ok := r.IntentReader.(CodeCallProjectionHistoryLookup)
	if !ok {
		return false, nil
	}
	hasCompleted, err := history.HasCompletedAcceptanceUnitDomainIntents(ctx, key, DomainCodeCalls)
	if err != nil {
		return false, fmt.Errorf("check completed code call projection history: %w", err)
	}
	return !hasCompleted, nil
}

func (r *CodeCallProjectionRunner) writeActiveRows(ctx context.Context, rows []SharedProjectionIntentRow) (int, int, error) {
	groups := groupCodeCallUpsertRows(rows)
	if len(groups) == 0 {
		return 0, 0, nil
	}

	sources := make([]string, 0, len(groups))
	for source := range groups {
		sources = append(sources, source)
	}
	sort.Strings(sources)

	writtenRows := 0
	for _, source := range sources {
		group := groups[source]
		if len(group) == 0 {
			continue
		}
		if err := r.EdgeWriter.WriteEdges(ctx, DomainCodeCalls, group, source); err != nil {
			return 0, 0, fmt.Errorf("write code call edges for %s: %w", source, err)
		}
		writtenRows += len(group)
	}

	return writtenRows, len(sources), nil
}

func (r *CodeCallProjectionRunner) wait(ctx context.Context, interval time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, interval)
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func codeCallPollBackoff(base time.Duration, consecutiveEmpty int) time.Duration {
	backoff := base
	for i := 1; i < consecutiveEmpty && i < 4; i++ {
		backoff *= 2
	}
	if backoff > maxCodeCallPollInterval {
		backoff = maxCodeCallPollInterval
	}
	return backoff
}

func codeCallEvidenceSources() []string {
	return []string{codeCallEvidenceSource, pythonMetaclassEvidenceSource}
}

func buildCodeCallRetractRows(rows []SharedProjectionIntentRow) []SharedProjectionIntentRow {
	repositoryIDs := uniqueRepositoryIDs(rows)
	if len(repositoryIDs) == 0 {
		return nil
	}
	deltaFilePathsByRepoID, hasDeltaScope := codeCallDeltaFilePathsByRepoIDFromRows(rows)
	if hasDeltaScope {
		return buildCodeCallDeltaRetractRows(repositoryIDs, deltaFilePathsByRepoID)
	}
	return buildCodeCallRepoRetractRows(repositoryIDs)
}

func buildCodeCallRepoRetractRows(repositoryIDs []string) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		rows = append(rows, SharedProjectionIntentRow{
			RepositoryID: repositoryID,
			Payload:      map[string]any{"repo_id": repositoryID},
		})
	}
	return rows
}

func codeCallProjectionFilePaths(rows []SharedProjectionIntentRow) []string {
	seen := make(map[string]struct{})
	filePaths := make([]string, 0, len(rows))
	for _, row := range rows {
		for _, filePath := range semanticPayloadStringSlice(row.Payload, "delta_file_paths") {
			if _, ok := seen[filePath]; ok {
				continue
			}
			seen[filePath] = struct{}{}
			filePaths = append(filePaths, filePath)
		}
	}
	sort.Strings(filePaths)
	return filePaths
}

func buildCodeCallDeltaRetractRows(
	repositoryIDs []string,
	deltaFilePathsByRepoID map[string][]string,
) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		filePaths := deltaFilePathsByRepoID[repositoryID]
		rows = append(rows, SharedProjectionIntentRow{
			RepositoryID: repositoryID,
			Payload: map[string]any{
				"repo_id":          repositoryID,
				"delta_projection": true,
				"delta_file_paths": append([]string(nil), filePaths...),
				"intent_type":      "repo_refresh",
			},
		})
	}
	return rows
}

func codeCallDeltaFilePathsByRepoIDFromRows(rows []SharedProjectionIntentRow) (map[string][]string, bool) {
	seenByRepoID := make(map[string]map[string]struct{})
	hasDeltaScope := false
	for _, row := range rows {
		repositoryID := strings.TrimSpace(row.RepositoryID)
		if repositoryID == "" || !codeCallPayloadBool(row.Payload, "delta_projection") {
			continue
		}
		hasDeltaScope = true
		for _, filePath := range semanticPayloadStringSlice(row.Payload, "delta_file_paths") {
			seen := seenByRepoID[repositoryID]
			if seen == nil {
				seen = make(map[string]struct{})
				seenByRepoID[repositoryID] = seen
			}
			seen[filePath] = struct{}{}
		}
	}
	if len(seenByRepoID) == 0 {
		return nil, hasDeltaScope
	}

	pathsByRepoID := make(map[string][]string, len(seenByRepoID))
	for repositoryID, seen := range seenByRepoID {
		filePaths := make([]string, 0, len(seen))
		for filePath := range seen {
			filePaths = append(filePaths, filePath)
		}
		sort.Strings(filePaths)
		pathsByRepoID[repositoryID] = filePaths
	}
	return pathsByRepoID, hasDeltaScope
}

func groupCodeCallUpsertRows(rows []SharedProjectionIntentRow) map[string][]SharedProjectionIntentRow {
	groups := make(map[string][]SharedProjectionIntentRow)
	for _, row := range rows {
		if !isCodeCallEdgeRow(row) {
			continue
		}
		source := codeCallRowEvidenceSource(row)
		groups[source] = append(groups[source], row)
	}
	return groups
}

func uniqueRepositoryIDs(rows []SharedProjectionIntentRow) []string {
	seen := make(map[string]struct{}, len(rows))
	repositoryIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		repositoryID := strings.TrimSpace(row.RepositoryID)
		if repositoryID == "" {
			continue
		}
		if _, ok := seen[repositoryID]; ok {
			continue
		}
		seen[repositoryID] = struct{}{}
		repositoryIDs = append(repositoryIDs, repositoryID)
	}
	sort.Strings(repositoryIDs)
	return repositoryIDs
}

func acceptedGenerationID(rows []SharedProjectionIntentRow) string {
	for _, row := range rows {
		if generationID := strings.TrimSpace(row.GenerationID); generationID != "" {
			return generationID
		}
	}
	return ""
}

func isCodeCallEdgeRow(row SharedProjectionIntentRow) bool {
	if row.Payload == nil {
		return false
	}
	if action := codeCallRowPayloadString(row, "action"); action == "delete" {
		return false
	}
	if codeCallRowPayloadString(row, "relationship_type") == "USES_METACLASS" {
		return codeCallRowPayloadString(row, "source_entity_id") != "" && codeCallRowPayloadString(row, "target_entity_id") != ""
	}
	return codeCallRowPayloadString(row, "caller_entity_id") != "" && codeCallRowPayloadString(row, "callee_entity_id") != ""
}

func codeCallRowEvidenceSource(row SharedProjectionIntentRow) string {
	if source := strings.TrimSpace(codeCallRowPayloadString(row, "evidence_source")); source != "" {
		return source
	}
	if codeCallRowPayloadString(row, "relationship_type") == "USES_METACLASS" {
		return pythonMetaclassEvidenceSource
	}
	return codeCallEvidenceSource
}

func codeCallRowPayloadString(row SharedProjectionIntentRow, key string) string {
	if row.Payload == nil {
		return ""
	}
	value, ok := row.Payload[key]
	if !ok {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(str)
}
