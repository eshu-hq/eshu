// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"
)

type codeCallDeltaFilePartition struct {
	filePath      string
	partitionKey  string
	partitionPath string
}

func buildCodeCallDeltaFilePartitions(
	repositoryID string,
	deltaScope codeCallDeltaFileScope,
) ([]codeCallDeltaFilePartition, bool) {
	if len(deltaScope.partitionPaths) == 0 || len(deltaScope.filePaths) != len(deltaScope.partitionPaths) {
		return nil, false
	}

	filePathByPartitionPath := make(map[string]string, len(deltaScope.partitionPaths))
	for i, rawPartitionPath := range deltaScope.partitionPaths {
		partitionPath, ok := normalizeCodeCallDeltaRelativePath(rawPartitionPath)
		if !ok {
			return nil, false
		}
		filePath := strings.TrimSpace(deltaScope.filePaths[i])
		if filePath == "" {
			return nil, false
		}
		if previous, exists := filePathByPartitionPath[partitionPath]; exists {
			if previous != filePath {
				return nil, false
			}
			continue
		}
		filePathByPartitionPath[partitionPath] = filePath
	}
	if len(filePathByPartitionPath) == 0 {
		return nil, false
	}

	partitionPaths := make([]string, 0, len(filePathByPartitionPath))
	for partitionPath := range filePathByPartitionPath {
		partitionPaths = append(partitionPaths, partitionPath)
	}
	sort.Strings(partitionPaths)

	partitions := make([]codeCallDeltaFilePartition, 0, len(partitionPaths))
	for _, partitionPath := range partitionPaths {
		partitionKey, ok := codeCallRefreshPartitionKeyForDeltaScope(repositoryID, []string{partitionPath})
		if !ok {
			return nil, false
		}
		partitions = append(partitions, codeCallDeltaFilePartition{
			filePath:      filePathByPartitionPath[partitionPath],
			partitionKey:  partitionKey,
			partitionPath: partitionPath,
		})
	}
	return partitions, true
}

func buildCodeCallDeltaPartitionIndexByRepoID(
	deltaScopesByRepoID map[string]codeCallDeltaFileScope,
) map[string]map[string]codeCallDeltaFilePartition {
	if len(deltaScopesByRepoID) == 0 {
		return nil
	}

	partitionsByRepoID := make(map[string]map[string]codeCallDeltaFilePartition, len(deltaScopesByRepoID))
	for repositoryID, deltaScope := range deltaScopesByRepoID {
		partitions, ok := buildCodeCallDeltaFilePartitions(repositoryID, deltaScope)
		if !ok {
			continue
		}
		byPartitionPath := make(map[string]codeCallDeltaFilePartition, len(partitions))
		for _, partition := range partitions {
			byPartitionPath[partition.partitionPath] = partition
		}
		partitionsByRepoID[repositoryID] = byPartitionPath
	}
	return partitionsByRepoID
}

func codeCallDeltaPartitionForPayload(
	payload map[string]any,
	partitionsByPath map[string]codeCallDeltaFilePartition,
) (codeCallDeltaFilePartition, bool) {
	sourcePath, ok := codeCallDeltaSourcePartitionPath(payload)
	if !ok {
		return codeCallDeltaFilePartition{}, false
	}
	partition, ok := partitionsByPath[sourcePath]
	if !ok {
		return codeCallDeltaFilePartition{}, false
	}
	return partition, true
}

func codeCallDeltaSourcePartitionPath(payload map[string]any) (string, bool) {
	for _, key := range []string{"caller_file", "source_file"} {
		value := strings.TrimSpace(anyToString(payload[key]))
		if value == "" {
			continue
		}
		return normalizeCodeCallDeltaRelativePath(value)
	}
	return "", false
}

func codeCallDeltaEdgeIdentityKey(partitionKey, callerID, calleeID, repositoryID, relationshipType string) string {
	edgeKey := strings.TrimSpace(callerID) + "->" + strings.TrimSpace(calleeID)
	if edgeKey == "->" {
		edgeKey = strings.TrimSpace(repositoryID)
	}
	if relationshipType != "" {
		edgeKey += ":" + strings.TrimSpace(relationshipType)
	}
	return strings.TrimSpace(partitionKey) + ":edge:" + edgeKey
}
