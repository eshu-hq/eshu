// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// deferredScopedRepoIDChunkSize keeps the repo-id self-exclusion arm bounded for
// oversized catalogs without multiplying the representative 896-repository
// corpus into repeated per-scope scans.
const deferredScopedRepoIDChunkSize = 1024

type deferredScopedFactLoadTask struct {
	partition ScopeGenerationPartition
	params    DeferredScopedFactQueryParams
}

func mergeDeferredScopedTaskFacts(perTask [][]facts.Envelope) []facts.Envelope {
	total := 0
	for _, envelopes := range perTask {
		total += len(envelopes)
	}
	if total == 0 {
		return nil
	}
	seen := make(map[string]struct{}, total)
	merged := make([]facts.Envelope, 0, total)
	for _, envelopes := range perTask {
		for _, envelope := range envelopes {
			if envelope.FactID != "" {
				if _, ok := seen[envelope.FactID]; ok {
					continue
				}
				seen[envelope.FactID] = struct{}{}
			}
			merged = append(merged, envelope)
		}
	}
	return merged
}

func buildDeferredScopedFactLoadTasks(
	partitions []ScopeGenerationPartition,
	params DeferredScopedFactQueryParams,
) []deferredScopedFactLoadTask {
	tasks := make([]deferredScopedFactLoadTask, 0, len(partitions))
	for _, partition := range partitions {
		repoIDValues := []string(params.RepoIDValues)
		repoIDReferenceKeys := []string(DeferredRepoIDReferenceKeys(params.RepoIDValues, params.RepoIDReferenceKey))
		if len(repoIDValues) == 0 {
			tasks = append(tasks, deferredScopedFactLoadTask{partition: partition, params: params})
			continue
		}
		for start := 0; start < len(repoIDValues); start += deferredScopedRepoIDChunkSize {
			end := start + deferredScopedRepoIDChunkSize
			if end > len(repoIDValues) {
				end = len(repoIDValues)
			}
			taskParams := DeferredScopedFactQueryParams{
				RepoIDValues:       array.StringArray(repoIDValues[start:end]),
				RepoIDReferenceKey: array.StringArray(repoIDReferenceKeys[start:end]),
			}
			if start == 0 {
				taskParams.NonRepoIDLike = params.NonRepoIDLike
			} else {
				taskParams.NonRepoIDLike = array.StringArray{}
			}
			tasks = append(tasks, deferredScopedFactLoadTask{
				partition: partition,
				params:    taskParams,
			})
		}
	}
	return tasks
}
