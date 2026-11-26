// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// RouteCoverageOptions configures the live half of the route coverage sweep.
type RouteCoverageOptions struct {
	// BaseURL is the core API origin; MetricsURL is the core's Prometheus endpoint.
	BaseURL, MetricsURL string
	// APIKey is the gate's own key; GrantedAPIKey is a key whose principal holds a repository
	// grant, for the supports_grant request. Empty fails the run: a label with no request is
	// not covered.
	APIKey, GrantedAPIKey string
	Iterations, Runs      int
	Timeout               time.Duration
	Meter                 WorkMeter
	Context               context.Context
	// Rows overrides the declared requests; nil uses routeCoverageRows.
	Rows []routeCoverageRow
}

// SweepRouteCoverage runs each declared request through the metered sweep, one at a time, and
// scrapes the read-route counter before and after it. It returns one RouteLatency per request (its
// own latency and work row) and every coverage problem found; a non-empty problem list fails the
// gate. It also asserts the legacy route did not rise over the whole sweep.
func SweepRouteCoverage(opts RouteCoverageOptions) ([]RouteLatency, []string, error) {
	rows := opts.Rows
	if rows == nil {
		rows = routeCoverageRows()
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Iterations < 1 {
		return nil, nil, fmt.Errorf("route coverage sweep: iterations must be at least 1, got %d", opts.Iterations)
	}
	runs := opts.Runs
	if runs < 1 {
		runs = 1
	}
	client := &http.Client{Timeout: opts.Timeout}
	var problems []string
	results := make([]RouteLatency, 0, len(rows))

	sweepStart, err := scrapeRouteCounter(ctx, client, opts.MetricsURL)
	if err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		key := opts.APIKey
		if row.Caller == callerGranted {
			key = opts.GrantedAPIKey
			if key == "" {
				problems = append(problems, fmt.Sprintf("%s: no granted API key (pass -granted-api-key): the supports_grant label cannot be reached without a caller that holds a grant", row.Name))
				continue
			}
		}
		before, err := scrapeRouteCounter(ctx, client, opts.MetricsURL)
		if err != nil {
			return nil, nil, err
		}
		result, err := sweepRoute(ctx, client, row.Name, opts.BaseURL+row.Path, key, opts.Iterations, runs, opts.Meter, row.operation(), nil)
		if err != nil {
			return nil, nil, err
		}
		after, err := scrapeRouteCounter(ctx, client, opts.MetricsURL)
		if err != nil {
			return nil, nil, err
		}
		result.Method, result.Path = http.MethodGet, row.Path
		results = append(results, result)
		if !result.Exercised || result.HardFailed {
			problems = append(problems, fmt.Sprintf("%s: the request was not served (status %d): the label cannot be observed", row.Name, result.Status))
			continue
		}
		problems = append(problems, routeRiseProblems(row.Name, before, after, row.Declared)...)
	}
	sweepEnd, err := scrapeRouteCounter(ctx, client, opts.MetricsURL)
	if err != nil {
		return nil, nil, err
	}
	problems = append(problems, legacyRouteProblems(sweepStart, sweepEnd)...)
	return results, problems, nil
}

// sweepWithCoverage runs the generic route sweep and, when a metrics URL is set, the route coverage
// sweep after it: the read-route counter is read before the generic sweep so the legacy label can be
// held flat over the whole run, the coverage requests append their own latency and work rows, and the
// problems (an unreached label, an undeclared rise, a missing granted key, a legacy rise) come back
// for the gate to fail on. With no metrics URL it is the generic sweep alone.
func sweepWithCoverage(ctx context.Context, opts runOptions, meter WorkMeter, routes []string, operations map[string]Operation) ([]RouteLatency, []string, error) {
	var start map[string]float64
	if opts.coverage.metricsURL != "" {
		var err error
		if start, err = scrapeRouteCounter(ctx, &http.Client{Timeout: opts.requestTimeout}, opts.coverage.metricsURL); err != nil {
			return nil, nil, err
		}
	}
	results, err := SweepRoutes(SweepOptions{
		BaseURL:     opts.apiBaseURL,
		MCPBaseURL:  opts.mcpBaseURL,
		Operations:  operations,
		APIKey:      opts.apiKey,
		Routes:      routes,
		QueryArgs:   RouteQueryArgs,
		Iterations:  opts.iterations,
		Runs:        opts.runs,
		Timeout:     opts.requestTimeout,
		Meter:       meter,
		Context:     ctx,
		Diagnostics: opts.diagnostics,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("sweep routes: %w", err)
	}
	if opts.coverage.metricsURL == "" {
		return results, nil, nil
	}
	covered, problems, err := SweepRouteCoverage(RouteCoverageOptions{
		BaseURL: opts.apiBaseURL, MetricsURL: opts.coverage.metricsURL, APIKey: opts.apiKey, GrantedAPIKey: opts.coverage.grantedAPIKey,
		Iterations: opts.iterations, Runs: opts.runs, Timeout: opts.requestTimeout, Meter: meter, Context: ctx,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("route coverage sweep: %w", err)
	}
	end, err := scrapeRouteCounter(ctx, &http.Client{Timeout: opts.requestTimeout}, opts.coverage.metricsURL)
	if err != nil {
		return nil, nil, err
	}
	return append(results, covered...), append(problems, legacyRouteProblems(start, end)...), nil
}

// coverageFailures prints each coverage problem and returns it as a gate failure.
func coverageFailures(w io.Writer, problems []string) []string {
	failures := make([]string, 0, len(problems))
	for _, problem := range problems {
		_, _ = fmt.Fprintf(w, "read-api-latency-gate: route coverage: %s\n", problem)
		failures = append(failures, "route coverage: "+problem)
	}
	return failures
}

// coverageFlags are the two flags of the route coverage stage, registered on the default flag set.
type coverageFlags struct{ metricsURL, grantedAPIKey *string }

// coverageSettings are their parsed values: the stage runs only when metricsURL is set.
type coverageSettings struct{ metricsURL, grantedAPIKey string }

func registerCoverageFlags() coverageFlags {
	return coverageFlags{
		metricsURL:    flag.String("metrics-url", os.Getenv("READ_API_LATENCY_GATE_METRICS_URL"), "the core's Prometheus endpoint; when set the sweep also runs the impact route coverage (#10880 PR-C2 n)"),
		grantedAPIKey: flag.String("granted-api-key", os.Getenv("READ_API_LATENCY_GATE_GRANTED_API_KEY"), "bearer token of a principal holding a repository grant, for the supports_grant coverage request"),
	}
}

func (f coverageFlags) settings() coverageSettings {
	return coverageSettings{metricsURL: *f.metricsURL, grantedAPIKey: *f.grantedAPIKey}
}
