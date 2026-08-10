// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

const (
	supplyChainKubernetesRuntimeProbeMaxConcurrency         = 32
	supplyChainKubernetesRuntimeProbeMaxAllScopesCandidates = 400
)

// KubernetesRuntimeProbeMetadata describes the bounded digest-local candidate
// budget. A nil WorkloadRefsTruncated means authorization prevents the caller
// from learning whether hidden candidates exist.
type KubernetesRuntimeProbeMetadata struct {
	CandidateLimit        int   `json:"candidate_limit"`
	WorkloadRefsTruncated *bool `json:"workload_refs_truncated"`
}

type kubernetesRuntimeProbePlan struct {
	Digest     string
	Quota      int
	QueryLimit int
}

type kubernetesRuntimeProbeSlot struct {
	plan         kubernetesRuntimeProbePlan
	candidates   []KubernetesRuntimeCandidate
	rawExhausted bool
}

type kubernetesRuntimeProbeFanout struct {
	slots                 []kubernetesRuntimeProbeSlot
	candidates            []KubernetesRuntimeCandidate
	plannedCandidateLimit int
	maxConcurrency        int
}

func planKubernetesRuntimeProbeQueries(digests []string, allScopes bool) []kubernetesRuntimeProbePlan {
	digests = sortedUniqueNonEmptyStrings(digests)
	if len(digests) > supplyChainCloudRuntimeProbeMaxDigests {
		digests = digests[:supplyChainCloudRuntimeProbeMaxDigests]
	}
	if len(digests) == 0 {
		return nil
	}
	baseQuota := supplyChainKubernetesRuntimeProbeMaxResults / len(digests)
	remainder := supplyChainKubernetesRuntimeProbeMaxResults % len(digests)
	plans := make([]kubernetesRuntimeProbePlan, len(digests))
	for i, digest := range digests {
		quota := baseQuota
		if i < remainder {
			quota++
		}
		queryLimit := quota
		if allScopes {
			queryLimit++
		}
		plans[i] = kubernetesRuntimeProbePlan{Digest: digest, Quota: quota, QueryLimit: queryLimit}
	}
	return plans
}

func queryKubernetesRuntimeCandidates(
	ctx context.Context,
	graph GraphQuery,
	plans []kubernetesRuntimeProbePlan,
) (kubernetesRuntimeProbeFanout, error) {
	if len(plans) == 0 {
		return kubernetesRuntimeProbeFanout{}, nil
	}
	concurrencyLimit := min(len(plans), supplyChainKubernetesRuntimeProbeMaxConcurrency)
	result := kubernetesRuntimeProbeFanout{
		slots: make([]kubernetesRuntimeProbeSlot, len(plans)),
	}
	jobs := make(chan int, len(plans))
	for i, plan := range plans {
		result.slots[i].plan = plan
		result.plannedCandidateLimit += plan.QueryLimit
		jobs <- i
	}
	close(jobs)

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var firstErr error
	var firstErrOnce sync.Once
	var active atomic.Int32
	var maximum atomic.Int32
	var workers sync.WaitGroup
	workers.Add(concurrencyLimit)
	for range concurrencyLimit {
		go func() {
			defer workers.Done()
			for index := range jobs {
				if workerCtx.Err() != nil {
					continue
				}
				plan := plans[index]
				current := active.Add(1)
				updateKubernetesRuntimeProbeMaximum(&maximum, current)
				graphRows, err := graph.Run(workerCtx, supplyChainKubernetesRuntimeProbeCypher, map[string]any{
					"subject_digests": []string{plan.Digest},
					"evidence_source": supplyChainKubernetesRuntimeEvidenceSource,
					"resolution_mode": supplyChainKubernetesRuntimeResolutionMode,
					"limit":           plan.QueryLimit,
				})
				active.Add(-1)
				if err != nil {
					firstErrOnce.Do(func() {
						firstErr = fmt.Errorf("query kubernetes runtime digest %q candidates: %w", plan.Digest, err)
						cancel()
					})
					continue
				}
				if len(graphRows) > plan.QueryLimit {
					graphRows = graphRows[:plan.QueryLimit]
				}
				result.slots[index].rawExhausted = len(graphRows) <= plan.Quota
				result.slots[index].candidates = kubernetesRuntimeCandidates(graphRows)
			}
		}()
	}
	workers.Wait()
	result.maxConcurrency = int(maximum.Load())
	if firstErr != nil {
		return kubernetesRuntimeProbeFanout{}, firstErr
	}
	if err := ctx.Err(); err != nil {
		return kubernetesRuntimeProbeFanout{}, err
	}
	for _, slot := range result.slots {
		result.candidates = append(result.candidates, slot.candidates...)
	}
	return result, nil
}

func updateKubernetesRuntimeProbeMaximum(maximum *atomic.Int32, current int32) {
	for {
		observed := maximum.Load()
		if current <= observed || maximum.CompareAndSwap(observed, current) {
			return
		}
	}
}
