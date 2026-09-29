// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (w *CanonicalNodeWriter) buildRetractStatements(mat canonical.CanonicalMaterialization) []Statement {
	if mat.FirstGeneration {
		return nil
	}
	if !hasRepositoryScopedRetract(mat) {
		return nil
	}
	if mat.DeltaProjection {
		return w.buildDeltaRetractStatements(mat)
	}

	retractParams := map[string]any{
		"repo_id":       mat.RepoID,
		"generation_id": mat.GenerationID,
	}

	filePaths := make([]string, 0, len(mat.Files))
	for _, f := range mat.Files {
		filePaths = append(filePaths, f.Path)
	}
	filePaths = dedupeStringValues(filePaths)
	directoryPaths := make([]string, 0, len(mat.Directories))
	for _, directory := range mat.Directories {
		directoryPaths = append(directoryPaths, directory.Path)
	}
	directoryPaths = dedupeStringValues(directoryPaths)

	stmts := make([]Statement, 0, 3)
	fileRetractCypher := canonicalNodeRetractFilesCypher
	fileRetractParams := retractParams
	if len(filePaths) > 0 {
		fileRetractCypher = canonicalNodeRetractRemovedFilesCypher
		fileRetractParams = map[string]any{
			"repo_id":       mat.RepoID,
			"generation_id": mat.GenerationID,
			"file_paths":    filePaths,
		}
	}
	// Drain=true: this is one of the 4 unbounded full-refresh DETACH DELETE
	// statements. The NornicDB executor converts it into a bounded drain loop.
	// Delta and positive-list retracts are never marked Drain (see issue #4232).
	stmts = append(stmts, Statement{
		Operation:  OperationCanonicalRetract,
		Cypher:     fileRetractCypher,
		Parameters: fileRetractParams,
		Drain:      true,
		DrainVar:   "f",
	})

	if len(filePaths) > 0 {
		for _, cypher := range []string{
			canonicalNodeRefreshCurrentFileImportEdgesCypher,
			canonicalNodeRefreshCurrentDirectoryFileEdgesCypher,
		} {
			stmts = append(stmts, buildStringSliceRetractStatements(
				cypher,
				"file_paths",
				filePaths,
				canonicalNodeRefreshFilePathBatchSize,
			)...)
		}
	}
	stmts = append(stmts, buildEntityContainmentRefreshStatements(mat.Entities, mat.ClassMembers, mat.NestedFuncs)...)
	if directoryParentEdgeRefreshRows := currentDirectoryParentEdgeRefreshRows(mat.Directories); len(directoryParentEdgeRefreshRows) > 0 {
		stmts = append(stmts, buildDirectoryParentEdgeRefreshStatements(
			canonicalNodeRefreshCurrentDirectoryParentEdgesCypher,
			mat.RepoID,
			directoryParentEdgeRefreshRows,
			canonicalNodeRefreshFilePathBatchSize,
		)...)
	}

	// Drain=true: unbounded full-refresh directory DETACH DELETE (issue #4232).
	stmts = append(stmts, Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    canonicalNodeRetractDirectoriesCypher,
		Parameters: map[string]any{
			"repo_id":         mat.RepoID,
			"generation_id":   mat.GenerationID,
			"directory_paths": directoryPaths,
		},
		Drain:    true,
		DrainVar: "d",
	})

	// Parameter retraction uses file_paths
	if len(filePaths) > 0 {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    canonicalNodeRetractParametersCypher,
			Parameters: map[string]any{
				"file_paths":    filePaths,
				"generation_id": mat.GenerationID,
			},
		})
	}

	return stmts
}

func (w *CanonicalNodeWriter) buildEntityRetractStatements(mat canonical.CanonicalMaterialization) []Statement {
	if mat.FirstGeneration {
		return nil
	}
	if !hasRepositoryScopedRetract(mat) {
		return nil
	}
	if mat.DeltaProjection {
		return buildDeltaEntityRetractStatements(mat)
	}
	labels := canonicalNodeRetractEntityLabels()
	stmts := make([]Statement, 0, len(labels))
	for _, label := range labels {
		// Drain=true: unbounded full-refresh entity DETACH DELETE per label
		// (issue #4232). DrainVar is always "n" — the template variable name.
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    fmt.Sprintf(canonicalNodeRetractEntityTemplate, label),
			Parameters: map[string]any{
				"repo_id":       mat.RepoID,
				"generation_id": mat.GenerationID,
			},
			Drain:    true,
			DrainVar: "n",
		})
	}
	return stmts
}

func hasRepositoryScopedRetract(mat canonical.CanonicalMaterialization) bool {
	return strings.TrimSpace(mat.RepoID) != ""
}

// dedupeStringValues preserves first-seen order so retract chunking stays
// deterministic while positive UNWIND cleanups avoid duplicate deletes.
func dedupeStringValues(values []string) []string {
	if len(values) == 0 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	deduped := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		deduped = append(deduped, value)
	}
	return deduped
}

func canonicalNodeRetractEntityLabels() []string {
	labels := make(map[string]struct{})
	for _, family := range []map[string]struct{}{
		canonicalNodeRetractCodeEntityLabels,
		canonicalNodeRetractInfraEntityLabels,
		canonicalNodeRetractTerraformEntityLabels,
		canonicalNodeRetractCloudFormationEntityLabels,
		canonicalNodeRetractSQLEntityLabels,
		canonicalNodeRetractDataEntityLabels,
		canonicalNodeRetractOCIEntityLabels,
		canonicalNodeRetractPackageRegistryEntityLabels,
	} {
		for label := range family {
			labels[label] = struct{}{}
		}
	}

	sorted := make([]string, 0, len(labels))
	for label := range labels {
		sorted = append(sorted, label)
	}
	sort.Strings(sorted)
	return sorted
}

func buildStringSliceRetractStatements(cypher string, paramName string, values []string, batchSize int) []Statement {
	if len(values) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = len(values)
	}
	stmts := make([]Statement, 0, (len(values)+batchSize-1)/batchSize)
	for start := 0; start < len(values); start += batchSize {
		end := start + batchSize
		if end > len(values) {
			end = len(values)
		}
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    cypher,
			Parameters: map[string]any{
				paramName: append([]string(nil), values[start:end]...),
			},
		})
	}
	return stmts
}

func buildEntityContainmentRefreshStatements(
	entities []canonical.EntityRow,
	classMembers []canonical.ClassMemberRow,
	nestedFuncs []canonical.NestedFunctionRow,
) []Statement {
	parentChildIDs := make(map[string]map[string]struct{})
	parentLabels := make(map[string]string)
	classIDsByFileName := make(map[string][]string)
	functionIDsByFileName := make(map[string][]string)
	functionIDsByFileNameLine := make(map[string][]string)

	for _, entity := range entities {
		if entity.EntityID == "" {
			continue
		}
		switch entity.Label {
		case "Class":
			parentChildIDs[entity.EntityID] = make(map[string]struct{})
			parentLabels[entity.EntityID] = entity.Label
			classIDsByFileName[fileNameKey(entity.FilePath, entity.EntityName)] = append(
				classIDsByFileName[fileNameKey(entity.FilePath, entity.EntityName)],
				entity.EntityID,
			)
		case "Function":
			parentChildIDs[entity.EntityID] = make(map[string]struct{})
			parentLabels[entity.EntityID] = entity.Label
			functionIDsByFileName[fileNameKey(entity.FilePath, entity.EntityName)] = append(
				functionIDsByFileName[fileNameKey(entity.FilePath, entity.EntityName)],
				entity.EntityID,
			)
			functionIDsByFileNameLine[fileNameLineKey(entity.FilePath, entity.EntityName, entity.StartLine)] = append(
				functionIDsByFileNameLine[fileNameLineKey(entity.FilePath, entity.EntityName, entity.StartLine)],
				entity.EntityID,
			)
		}
	}

	for _, classMember := range classMembers {
		childIDs := functionIDsByFileNameLine[fileNameLineKey(classMember.FilePath, classMember.FunctionName, classMember.FunctionLine)]
		if len(childIDs) == 0 {
			continue
		}
		for _, parentID := range classIDsByFileName[fileNameKey(classMember.FilePath, classMember.ClassName)] {
			for _, childID := range childIDs {
				parentChildIDs[parentID][childID] = struct{}{}
			}
		}
	}

	for _, nestedFunc := range nestedFuncs {
		childIDs := functionIDsByFileNameLine[fileNameLineKey(nestedFunc.FilePath, nestedFunc.InnerName, nestedFunc.InnerLine)]
		if len(childIDs) == 0 {
			continue
		}
		for _, parentID := range functionIDsByFileName[fileNameKey(nestedFunc.FilePath, nestedFunc.OuterName)] {
			for _, childID := range childIDs {
				parentChildIDs[parentID][childID] = struct{}{}
			}
		}
	}

	if len(parentChildIDs) == 0 {
		return nil
	}
	parentIDs := make([]string, 0, len(parentChildIDs))
	for parentID := range parentChildIDs {
		parentIDs = append(parentIDs, parentID)
	}
	sort.Strings(parentIDs)

	rowsByLabel := make(map[string][]map[string]any, 2)
	for _, parentID := range parentIDs {
		childIDs := make([]string, 0, len(parentChildIDs[parentID]))
		for childID := range parentChildIDs[parentID] {
			childIDs = append(childIDs, childID)
		}
		sort.Strings(childIDs)
		label := parentLabels[parentID]
		rowsByLabel[label] = append(rowsByLabel[label], map[string]any{
			"parent_entity_id": parentID,
			"child_entity_ids": childIDs,
		})
	}

	labels := make([]string, 0, len(rowsByLabel))
	for label := range rowsByLabel {
		if label != "" {
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)

	stmts := make([]Statement, 0, len(parentIDs))
	for _, label := range labels {
		stmts = append(
			stmts,
			buildBatchedRetractStatements(
				fmt.Sprintf(canonicalNodeRefreshCurrentEntityContainmentEdgesTemplate, label),
				rowsByLabel[label],
				canonicalNodeRefreshEntityContainmentBatchSize,
			)...,
		)
	}
	return stmts
}

func fileNameKey(filePath, name string) string {
	return filePath + "\x00" + name
}

func fileNameLineKey(filePath, name string, line int) string {
	return fmt.Sprintf("%s\x00%s\x00%d", filePath, name, line)
}

// --- Phase B: Repository ---

// isRepositoryPathCleanupEntry reports whether a write-count entry belongs to
// the path-conflict retirement statement. It matches on the Cypher text,
// which no executor rewrites; the summary metadata key is stripped before
// the Bolt seam on the phase-group path (SanitizeStatement).
func isRepositoryPathCleanupEntry(entry WriteCountEntry) bool {
	return entry.Cypher == canonicalNodeRepositoryPathCleanupCypher
}

// captureRepositoryRetirement stashes a filtered write-count collector for
// the statements about to execute when they include the path-conflict
// retirement, and returns ctx unchanged (and a nil collector) otherwise, so
// first-generation and delta writes pay nothing. The collector forwards
// every entry to any collector the caller already stashed.
func captureRepositoryRetirement(ctx context.Context, statements []Statement) (context.Context, *WriteCountsCollector) {
	for _, stmt := range statements {
		if stmt.Cypher != canonicalNodeRepositoryPathCleanupCypher {
			continue
		}
		collector := NewFilteredWriteCountsCollector(WriteCountsCollectorFromContext(ctx), isRepositoryPathCleanupEntry)
		return WithWriteCountsCollector(ctx, collector), collector
	}
	return ctx, nil
}

// reportRepositoryRetirement publishes what the committed path-conflict
// retirement deleted (#7324). The statement DETACH DELETEs a different-id
// Repository still holding this path, which drops every relationship on it:
// reducer edges other scopes wrote into it, which nothing re-arms, and its
// own projector edges. relationships_deleted is the backend's count of both
// directions; direction and type are not attributed.
//
// The last collected entry is the committed attempt: a driver or executor
// retry re-runs the statement and reports again, and Write calls this only
// after the statement's transaction committed. The `canonical repository
// retired` line and the counter appear only when the statement deleted a
// node; a retirement that matched nothing (the steady state) records nothing
// at INFO or WARN. With no entry at all the executor reported no write
// summary, so nothing is known about the retirement: a DEBUG line with a
// distinct message and deletes_counted=false says so, and no counter moves.
// A retirement that committed but whose driver call still returned an error
// is re-run on retry, matches nothing, and so is not counted: the count is
// at most once per retirement, not exactly once.
func (w *CanonicalNodeWriter) reportRepositoryRetirement(
	ctx context.Context,
	mat canonical.CanonicalMaterialization,
	collector *WriteCountsCollector,
) {
	if collector == nil || mat.Repository == nil {
		return
	}
	entries := collector.Entries()
	fields := []any{
		"scope_id", mat.ScopeID,
		"generation_id", mat.GenerationID,
		"repo_id", mat.Repository.RepoID,
		"path", mat.Repository.Path,
	}
	if len(entries) == 0 {
		slog.DebugContext(ctx, "canonical repository retirement not counted: executor reported no write summary",
			append(fields, "deletes_counted", false)...)
		return
	}
	counters := entries[len(entries)-1].Counters
	if counters.NodesDeleted == 0 {
		return
	}
	outcome, level := telemetry.RepositoryRetirementOutcomeClean, slog.LevelInfo
	if counters.RelationshipsDeleted > 0 {
		outcome, level = telemetry.RepositoryRetirementOutcomeDroppedRelationships, slog.LevelWarn
	}
	if w.instruments != nil && w.instruments.CanonicalRepositoryRetirements != nil {
		w.instruments.CanonicalRepositoryRetirements.Add(ctx, 1, metric.WithAttributes(telemetry.AttrOutcome(outcome)))
	}
	slog.Log(ctx, level, "canonical repository retired", append(fields,
		"nodes_deleted", counters.NodesDeleted,
		"relationships_deleted", counters.RelationshipsDeleted,
		"deletes_counted", true,
		"outcome", outcome,
	)...)
}
