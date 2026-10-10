// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
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
	tracePath := os.Getenv("ESHU_DIAG_HTTPTRACE_REPORT")
	stageDir := os.Getenv("ESHU_DIAG_STAGE_DIR")
	var traceRecords []diagnosticHTTPRecord
	if tracePath != "" {
		traceRecords = make([]diagnosticHTTPRecord, 0, len(ids)*requests)
	}
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
		var captures []diagnosticHTTPTrace
		if tracePath != "" {
			captures = make([]diagnosticHTTPTrace, requests)
		}
		jobs := make(chan int)
		var group sync.WaitGroup
		var active, peak atomic.Int32
		if stageDir != "" && id == "GET /api/v0/status/ingesters/{ingester}" {
			if err := writeDiagnosticStageMarker(stageDir, "get_start"); err != nil {
				return nil, err
			}
		}
		started := time.Now()
		for worker := 0; worker < workers; worker++ {
			group.Add(1)
			go func() {
				defer group.Done()
				for index := range jobs {
					inFlight := active.Add(1)
					for previous := peak.Load(); inFlight > previous && !peak.CompareAndSwap(previous, inFlight); previous = peak.Load() {
					}
					if tracePath == "" {
						samples[index], statuses[index], bodies[index], errors[index] = sweepOperation(client, base+op.Path, opts.APIKey, op)
					} else {
						samples[index], statuses[index], bodies[index], errors[index] = sweepOperationTrace(client, base+op.Path, opts.APIKey, op, &captures[index])
					}
					active.Add(-1)
				}
			}()
		}
		for index := range requests {
			jobs <- index
		}
		close(jobs)
		group.Wait()
		if stageDir != "" && id == "GET /api/v0/status/ingesters/{ingester}" {
			if err := writeDiagnosticStageMarker(stageDir, "get_end"); err != nil {
				return nil, err
			}
		}
		result := RouteLatency{Route: id, Method: op.Method, Path: op.Path, MCP: op.MCP, Exercised: true, Samples: samples, P95: p95(append([]time.Duration(nil), samples...)), Requested: requests, Workers: workers, PeakInFlight: int(peak.Load()), Wall: time.Since(started), Statuses: statuses}
		if tracePath != "" {
			for index := range requests {
				traceRecords = append(traceRecords, captures[index].snapshot(id, index, samples[index], statuses[index]))
			}
			if err := writeDiagnosticHTTPTrace(tracePath, traceRecords); err != nil {
				return nil, fmt.Errorf("diagnostic httptrace report: %w", err)
			}
		}
		for index := range requests {
			if errors[index] != nil {
				return nil, fmt.Errorf("concurrent sweep %s request %d: %w", id, index+1, errors[index])
			}
			result.Status = statuses[index]
			if statuses[index] == http.StatusOK {
				result.Succeeded++
			}
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
