// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"strings"

	codecall "github.com/eshu-hq/eshu/go/internal/reducer/code/call"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

type codeCallProjectionPartitionKind int

const (
	codeCallProjectionPartitionLegacy codeCallProjectionPartitionKind = iota
	codeCallProjectionPartitionWhole
	codeCallProjectionPartitionFile
)

// FilePartitionKeyPrefix returns the durable prefix used by
// file-scoped code-call projection partition keys.
func FilePartitionKeyPrefix() string {
	return codecall.PartitionKeyVersion + ":files:"
}

func codeCallProjectionPartitionKindForKey(partitionKey string) codeCallProjectionPartitionKind {
	switch {
	case strings.HasPrefix(partitionKey, codecall.PartitionKeyVersion+":whole:"):
		return codeCallProjectionPartitionWhole
	case strings.HasPrefix(partitionKey, FilePartitionKeyPrefix()):
		return codeCallProjectionPartitionFile
	default:
		return codeCallProjectionPartitionLegacy
	}
}

func codeCallProjectionRowRepository(row sharedintent.Row) string {
	if repositoryID := strings.TrimSpace(row.RepositoryID); repositoryID != "" {
		return repositoryID
	}
	if row.Payload == nil {
		return ""
	}
	return strings.TrimSpace(payloadcore.AnyToString(row.Payload["repo_id"]))
}

func codeCallProjectionRowKind(row sharedintent.Row) codeCallProjectionPartitionKind {
	return codeCallProjectionPartitionKindForKey(row.PartitionKey)
}

func codeCallProjectionIsFileScoped(row sharedintent.Row) bool {
	return codeCallProjectionRowKind(row) == codeCallProjectionPartitionFile
}

func codeCallProjectionIsWholeScoped(row sharedintent.Row) bool {
	kind := codeCallProjectionRowKind(row)
	return kind == codeCallProjectionPartitionWhole || kind == codeCallProjectionPartitionLegacy
}

func codeCallProjectionIsRepoRefresh(row sharedintent.Row) bool {
	if row.Payload == nil {
		return false
	}
	return strings.TrimSpace(payloadcore.AnyToString(row.Payload["intent_type"])) == "repo_refresh"
}

func codeCallProjectionRefreshCoversRow(refresh sharedintent.Row, row sharedintent.Row) bool {
	if !codeCallProjectionIsRepoRefresh(refresh) ||
		!codeCallProjectionSameAcceptanceUnit(refresh, row) ||
		codeCallProjectionRowRepository(refresh) != codeCallProjectionRowRepository(row) {
		return false
	}
	if codeCallProjectionIsWholeScoped(refresh) {
		return true
	}
	if !codeCallProjectionIsFileScoped(refresh) {
		return false
	}
	if refresh.PartitionKey == row.PartitionKey {
		return true
	}
	rowFiles := payloadcore.SemanticPayloadStringSlice(row.Payload, "delta_file_paths")
	if len(rowFiles) == 0 {
		return false
	}
	refreshFiles := make(map[string]struct{}, len(rowFiles))
	for _, filePath := range payloadcore.SemanticPayloadStringSlice(refresh.Payload, "delta_file_paths") {
		refreshFiles[filePath] = struct{}{}
	}
	if len(refreshFiles) == 0 {
		return false
	}
	for _, filePath := range rowFiles {
		if _, ok := refreshFiles[filePath]; !ok {
			return false
		}
	}
	return true
}

func codeCallProjectionRowsForPartition(
	rows []sharedintent.Row,
	partitionKey string,
) []sharedintent.Row {
	kind := codeCallProjectionPartitionKindForKey(partitionKey)
	if kind != codeCallProjectionPartitionFile {
		return rows
	}

	filtered := make([]sharedintent.Row, 0, len(rows))
	for _, row := range rows {
		if row.PartitionKey == partitionKey {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func codeCallProjectionPartitionMatches(row sharedintent.Row, partitionID, partitionCount int) bool {
	rowPartitionID, err := sharedintent.PartitionForKey(row.PartitionKey, partitionCount)
	if err != nil {
		return false
	}
	return rowPartitionID == partitionID
}
