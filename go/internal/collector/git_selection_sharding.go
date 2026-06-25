// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
)

func loadRepoShardConfig(getenv func(string) string) (int, int, error) {
	rawCount := strings.TrimSpace(getenv("ESHU_REPO_SHARD_COUNT"))
	rawIndex := strings.TrimSpace(getenv("ESHU_REPO_SHARD_INDEX"))
	if rawCount == "" && rawIndex == "" {
		return 1, 0, nil
	}
	count := 1
	if rawCount != "" {
		parsed, err := strconv.Atoi(rawCount)
		if err != nil || parsed < 1 {
			return 0, 0, fmt.Errorf("ESHU_REPO_SHARD_COUNT must be a positive integer")
		}
		count = parsed
	}
	index := 0
	if rawIndex != "" {
		parsed, err := strconv.Atoi(rawIndex)
		if err != nil || parsed < 0 {
			return 0, 0, fmt.Errorf("ESHU_REPO_SHARD_INDEX must be a non-negative integer")
		}
		index = parsed
	}
	if index >= count {
		return 0, 0, fmt.Errorf("ESHU_REPO_SHARD_INDEX=%d must be less than ESHU_REPO_SHARD_COUNT=%d", index, count)
	}
	return count, index, nil
}

func filterRepositoryIDsByShard(repositoryIDs []string, config RepoSyncConfig) []string {
	if config.RepoShardCount <= 1 {
		return append([]string(nil), repositoryIDs...)
	}
	filtered := make([]string, 0, len(repositoryIDs)/config.RepoShardCount+1)
	for _, repositoryID := range repositoryIDs {
		if repositoryShardForID(repositoryID, config.RepoShardCount) == config.RepoShardIndex {
			filtered = append(filtered, repositoryID)
		}
	}
	return filtered
}

func repositoryShardForID(repositoryID string, shardCount int) int {
	if shardCount <= 1 {
		return 0
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(strings.ToLower(strings.TrimSpace(repositoryID))))
	return int(hasher.Sum32() % uint32(shardCount)) // #nosec G115 -- bounded: result is in [0, shardCount-1] which is a small positive int validated by caller
}
