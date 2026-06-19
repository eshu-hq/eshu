package reducer

import "strings"

type codeCallProjectionPartitionKind int

const (
	codeCallProjectionPartitionLegacy codeCallProjectionPartitionKind = iota
	codeCallProjectionPartitionWhole
	codeCallProjectionPartitionFile
)

// CodeCallProjectionFilePartitionKeyPrefix returns the durable prefix used by
// file-scoped code-call projection partition keys.
func CodeCallProjectionFilePartitionKeyPrefix() string {
	return codeCallPartitionKeyVersion + ":files:"
}

func codeCallProjectionPartitionKindForKey(partitionKey string) codeCallProjectionPartitionKind {
	switch {
	case strings.HasPrefix(partitionKey, codeCallPartitionKeyVersion+":whole:"):
		return codeCallProjectionPartitionWhole
	case strings.HasPrefix(partitionKey, CodeCallProjectionFilePartitionKeyPrefix()):
		return codeCallProjectionPartitionFile
	default:
		return codeCallProjectionPartitionLegacy
	}
}

func codeCallProjectionRowRepository(row SharedProjectionIntentRow) string {
	if repositoryID := strings.TrimSpace(row.RepositoryID); repositoryID != "" {
		return repositoryID
	}
	if row.Payload == nil {
		return ""
	}
	return strings.TrimSpace(anyToString(row.Payload["repo_id"]))
}

func codeCallProjectionRowKind(row SharedProjectionIntentRow) codeCallProjectionPartitionKind {
	return codeCallProjectionPartitionKindForKey(row.PartitionKey)
}

func codeCallProjectionIsFileScoped(row SharedProjectionIntentRow) bool {
	return codeCallProjectionRowKind(row) == codeCallProjectionPartitionFile
}

func codeCallProjectionIsWholeScoped(row SharedProjectionIntentRow) bool {
	kind := codeCallProjectionRowKind(row)
	return kind == codeCallProjectionPartitionWhole || kind == codeCallProjectionPartitionLegacy
}

func codeCallProjectionIsRepoRefresh(row SharedProjectionIntentRow) bool {
	if row.Payload == nil {
		return false
	}
	return strings.TrimSpace(anyToString(row.Payload["intent_type"])) == "repo_refresh"
}

func codeCallProjectionRefreshCoversRow(refresh SharedProjectionIntentRow, row SharedProjectionIntentRow) bool {
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
	rowFiles := semanticPayloadStringSlice(row.Payload, "delta_file_paths")
	if len(rowFiles) == 0 {
		return false
	}
	refreshFiles := make(map[string]struct{}, len(rowFiles))
	for _, filePath := range semanticPayloadStringSlice(refresh.Payload, "delta_file_paths") {
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
	rows []SharedProjectionIntentRow,
	partitionKey string,
) []SharedProjectionIntentRow {
	kind := codeCallProjectionPartitionKindForKey(partitionKey)
	if kind != codeCallProjectionPartitionFile {
		return rows
	}

	filtered := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		if row.PartitionKey == partitionKey {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func codeCallProjectionPartitionMatches(row SharedProjectionIntentRow, partitionID, partitionCount int) bool {
	rowPartitionID, err := PartitionForKey(row.PartitionKey, partitionCount)
	if err != nil {
		return false
	}
	return rowPartitionID == partitionID
}
