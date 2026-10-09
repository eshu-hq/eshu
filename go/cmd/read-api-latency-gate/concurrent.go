// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

// SweepConcurrentOperations measures selected pilot reads after the sequential
// metered pass. It never accepts a work meter: overlapping requests would
// attribute one operation's Postgres statements to another operation.
func SweepConcurrentOperations(opts SweepOptions, operations map[string]Operation, workers, requests int) ([]RouteLatency, error) {
	if workers < 2 || workers > 16 || requests < workers || requests > 1000 {
		return nil, fmt.Errorf("concurrent sweep requires 2..16 workers and workers..1000 requests")
	}
	if opts.Meter != nil {
		return nil, fmt.Errorf("concurrent sweep must not use a work meter")
	}
	ids := make([]string, 0, len(operations))
	for id := range operations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	client := &http.Client{Timeout: opts.Timeout}
	results := make([]RouteLatency, 0, len(ids))
	for _, id := range ids {
		op := operations[id]
		base := opts.BaseURL
		if op.MCP {
			base = opts.MCPBaseURL
		}
		if base == "" {
			return nil, fmt.Errorf("concurrent sweep %s: empty base URL", id)
		}
		samples := make([]time.Duration, requests)
		statuses := make([]int, requests)
		bodies := make([]string, requests)
		errors := make([]error, requests)
		jobs := make(chan int)
		var group sync.WaitGroup
		for worker := 0; worker < workers; worker++ {
			group.Add(1)
			go func() {
				defer group.Done()
				for index := range jobs {
					samples[index], statuses[index], bodies[index], errors[index] = sweepOperation(client, base+op.Path, opts.APIKey, op)
				}
			}()
		}
		for index := range requests {
			jobs <- index
		}
		close(jobs)
		group.Wait()
		result := RouteLatency{Route: id, Method: op.Method, Path: op.Path, MCP: op.MCP, Exercised: true, Samples: samples, P95: p95(append([]time.Duration(nil), samples...))}
		for index := range requests {
			if errors[index] != nil {
				return nil, fmt.Errorf("concurrent sweep %s request %d: %w", id, index+1, errors[index])
			}
			result.Status = statuses[index]
			if statuses[index] >= 400 && statuses[index] < 500 {
				result.Exercised = false
			}
			if statuses[index] >= 500 || statuses[index] == 0 {
				if !result.HardFailed {
					result.HardFailedBody = bodies[index]
				}
				result.HardFailed = true
			}
		}
		results = append(results, result)
	}
	return results, nil
}
