package reducer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (r *CodeCallProjectionRunner) loadAllAcceptanceUnitIntents(ctx context.Context, key SharedProjectionAcceptanceKey) ([]SharedProjectionIntentRow, error) {
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
	retractRows := buildCodeCallRetractRows(uniqueRepositoryIDs(rows))
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
	staleIDs []string,
) (bool, error) {
	if len(staleIDs) > 0 {
		return false, nil
	}
	currentHistory, ok := r.IntentReader.(CodeCallProjectionCurrentRunHistoryLookup)
	if ok {
		hasCurrent, err := currentHistory.HasCompletedAcceptanceUnitSourceRunDomainIntents(ctx, key, DomainCodeCalls)
		if err != nil {
			return false, fmt.Errorf("check completed current code call projection history: %w", err)
		}
		if hasCurrent {
			return true, nil
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

func buildCodeCallRetractRows(repositoryIDs []string) []SharedProjectionIntentRow {
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
