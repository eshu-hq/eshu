package reducer

import "strings"

type codeCallProjectionPartitionKind int

const (
	codeCallProjectionPartitionLegacy codeCallProjectionPartitionKind = iota
	codeCallProjectionPartitionWhole
	codeCallProjectionPartitionFile
)

func codeCallProjectionPartitionKindForKey(partitionKey string) codeCallProjectionPartitionKind {
	switch {
	case strings.HasPrefix(partitionKey, codeCallPartitionKeyVersion+":whole:"):
		return codeCallProjectionPartitionWhole
	case strings.HasPrefix(partitionKey, codeCallPartitionKeyVersion+":files:"):
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
