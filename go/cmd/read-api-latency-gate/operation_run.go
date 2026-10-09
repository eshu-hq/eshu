// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

func selectedOperations(inventory capabilitycatalog.SurfaceInventory, includePilots bool) ([]string, map[string]Operation, error) {
	routes := NoArgGetRoutes(inventory)
	if len(routes) == 0 {
		return nil, nil, fmt.Errorf("no-arg GET routes: found none in the surface inventory")
	}
	operations := map[string]Operation{}
	if includePilots {
		var err error
		operations, err = PilotOperations(inventory)
		if err != nil {
			return nil, nil, err
		}
		for id := range operations {
			routes = append(routes, id)
		}
		sort.Strings(routes)
	}
	return routes, operations, nil
}

func validateOperationOptions(opts runOptions) error {
	if opts.concurrentReport != "" && opts.concurrentWorkers == 0 {
		return fmt.Errorf("-concurrent-report requires -concurrent-workers")
	}
	if opts.concurrentWorkers != 0 && opts.mcpBaseURL == "" {
		return fmt.Errorf("-concurrent-workers requires -mcp-base-url")
	}
	return nil
}

func pilotFailures(results []RouteLatency, operations map[string]Operation) []string {
	var failures []string
	for _, result := range results {
		if _, selected := operations[result.Route]; selected && (!result.Exercised || result.HardFailed) {
			failures = append(failures, fmt.Sprintf("pilot operation %s was not successfully exercised (HTTP %d)", result.Route, result.Status))
		}
	}
	return failures
}

func runConcurrentPilot(opts runOptions, operations map[string]Operation, budgets RouteBudgets) error {
	if opts.concurrentWorkers == 0 {
		return nil
	}
	if len(operations) == 0 {
		return fmt.Errorf("concurrent pilot requires -mcp-base-url")
	}
	concurrent, err := SweepConcurrentOperations(SweepOptions{BaseURL: opts.apiBaseURL, MCPBaseURL: opts.mcpBaseURL, APIKey: opts.apiKey, Timeout: opts.requestTimeout}, operations, opts.concurrentWorkers, opts.concurrentRequests)
	if err != nil {
		return err
	}
	if err := writeConcurrentReport(opts.concurrentReport, opts, concurrent); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "read-api-latency-gate: concurrent pilot workers=%d requests/operation=%d (unmetered after sequential metered sweep)\n", opts.concurrentWorkers, opts.concurrentRequests)
	for _, result := range concurrent {
		fmt.Fprintf(os.Stderr, "  %s method=%s path=%s p95=%s status=%d exercised=%t hard_failed=%t\n", result.Route, result.Method, result.Path, result.P95, result.Status, result.Exercised, result.HardFailed)
		if !result.Exercised || result.HardFailed || result.Succeeded != result.Requested || len(EvaluateBudgets([]RouteLatency{result}, budgets)) > 0 {
			return fmt.Errorf("concurrent pilot %s failed correctness or latency budget", result.Route)
		}
	}
	return nil
}
