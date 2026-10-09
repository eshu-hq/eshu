// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"sort"
	"time"
)

// Operation is a seeded read request with a stable budget and report ID.
type Operation struct {
	Method string
	Path   string
	Body   string
	MCP    bool
}

// warmupRequests is the number of probes issued (and discarded) per route
// before the counted sample set starts. A cold connection and cold
// Postgres/NornicDB caches make the very first request(s) to a route
// unrepresentatively slow; without discarding them a single warmup request
// could dominate a small nearest-rank p95 sample (this is what a bare "run N requests, take p95" design misses).
const warmupRequests = 2

// hardFailedBodyCap bounds how much of a 5xx response body sweepRoute
// captures into RouteLatency.HardFailedBody — enough for an operator to see
// the error envelope, not so much that a pathological handler streaming a
// huge error body blows up the report.
const hardFailedBodyCap = 500

// SweepOptions configures SweepRoutes.
type SweepOptions struct {
	// BaseURL is the running eshu-api's base address, e.g. "http://localhost:18080".
	BaseURL string
	// MCPBaseURL is the running MCP HTTP transport address.
	MCPBaseURL string
	// APIKey authenticates every request as "Authorization: Bearer <APIKey>".
	APIKey string
	// Routes are operation IDs. Inventory routes use "METHOD /path"; selected
	// MCP calls use "MCP <tool>" and require an Operations fixture.
	Routes []string
	// Operations overrides the request shape for selected route IDs.
	Operations map[string]Operation
	// QueryArgs optionally supplies a raw query string (no leading "?") per
	// route, so a route that requires a selector (e.g. scope_id) can run its
	// real query instead of 400ing on a missing parameter. A route absent
	// from this map is requested with no query string.
	QueryArgs map[string]string
	// Iterations is the number of COUNTED requests issued per exercised
	// route — the sample set p95 is computed over. warmupRequests additional
	// requests are issued first and discarded (see its doc comment). A route
	// whose warmup probe returns a 4xx is not exercised and is not sampled
	// further.
	Iterations int
	// Runs is the number of independent counted sweeps per route: run 1 is
	// the COLD pass (immediately after warmupRequests, no additional warmup
	// before it), and runs 2..Runs are WARM passes (no warmup between them
	// either — the connection and caches are already hot from run 1). Zero
	// or one behaves exactly like the original single-pass gate: RouteLatency.P95
	// is computed from run 1's samples alone and WarmSamples/WarmRunP95s stay
	// empty, so a caller that never sets Runs (every existing caller, and CI
	// via -runs' default of 1) gets byte-for-byte unchanged output.
	Runs int
	// Timeout bounds each individual request.
	Timeout time.Duration
	// Meter, when set, measures the Postgres work of each exercised route's
	// counted requests: Reset after the warmup requests, Read after the last
	// counted one. A meter error aborts the run. Nil leaves routes unmetered.
	Meter WorkMeter
	// Context bounds meter calls; nil means context.Background().
	Context context.Context
}

// SweepRoutes issues warmupRequests discarded probes, then opts.Iterations
// counted requests, against each exercised route in opts.Routes, and
// returns one RouteLatency per route in the same order as opts.Routes. See
// RouteLatency's fields and sweepOne for what counts as a measured sample,
// a not-exercised route, a hard failure, or a sweep-aborting error.
func SweepRoutes(opts SweepOptions) ([]RouteLatency, error) {
	if opts.Iterations < 1 {
		return nil, fmt.Errorf("sweep: iterations must be at least 1, got %d (zero counted requests would pass every budget while measuring nothing)", opts.Iterations)
	}
	client := &http.Client{Timeout: opts.Timeout}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	runs := opts.Runs
	if runs < 1 {
		runs = 1
	}
	results := make([]RouteLatency, 0, len(opts.Routes))

	for _, route := range opts.Routes {
		op, ok := opts.Operations[route]
		if !ok {
			method, path, err := SplitRoute(route)
			if err != nil {
				return nil, err
			}
			op = Operation{Method: method, Path: path}
		}
		baseURL := opts.BaseURL
		if op.MCP {
			baseURL = opts.MCPBaseURL
		}
		if baseURL == "" {
			return nil, fmt.Errorf("sweep %s: base URL is empty", route)
		}
		url := baseURL + op.Path
		if q := opts.QueryArgs[route]; q != "" {
			url += "?" + q
		}

		result, err := sweepRoute(ctx, client, route, url, opts.APIKey, opts.Iterations, runs, opts.Meter, op)
		if err != nil {
			return nil, err
		}
		result.Method, result.Path, result.MCP = op.Method, op.Path, op.MCP
		results = append(results, result)
	}

	return results, nil
}

// sweepRoute runs the warmup-then-counted sweep for one route: warmupRequests
// discarded probes, then `runs` independent passes of `iterations` counted
// requests each, with no additional warmup between passes. Run 1 follows
// warmupRequests probes (RouteLatency.Samples); runs 2..runs are later passes
// (RouteLatency.WarmSamples/WarmRunP95s). See SweepOptions.Runs.
func sweepRoute(ctx context.Context, client *http.Client, route, url, apiKey string, iterations, runs int, meter WorkMeter, op Operation) (RouteLatency, error) {
	for i := 0; i < warmupRequests; i++ {
		_, status, _, err := sweepOperation(client, url, apiKey, op)
		if err != nil {
			return RouteLatency{}, fmt.Errorf("sweep %s (warmup %d/%d): %w", route, i+1, warmupRequests, err)
		}
		if status >= 400 && status < 500 {
			return RouteLatency{Route: route, Exercised: false, Status: status}, nil
		}
	}

	if meter != nil {
		if err := meter.Reset(ctx); err != nil {
			return RouteLatency{}, fmt.Errorf("sweep %s: reset work meter: %w", route, err)
		}
	}

	var coldSamples []time.Duration
	var warmSamples []time.Duration
	var warmRunP95s []time.Duration
	hardFailed := false
	clientFailed := false
	hardFailedBody := ""
	status := 0

	for run := 0; run < runs; run++ {
		runSamples := make([]time.Duration, 0, iterations)
		for i := 0; i < iterations; i++ {
			d, s, body, err := sweepOperation(client, url, apiKey, op)
			if err != nil {
				return RouteLatency{}, fmt.Errorf("sweep %s (run %d/%d, iteration %d/%d): %w", route, run+1, runs, i+1, iterations, err)
			}
			status = s
			if s >= 400 && s < 500 {
				clientFailed = true
			}
			if s >= 500 {
				if !hardFailed {
					hardFailedBody = body
				}
				hardFailed = true
			}
			runSamples = append(runSamples, d)
		}
		if run == 0 {
			coldSamples = runSamples
			continue
		}
		warmSamples = append(warmSamples, runSamples...)
		// percentile sorts in place; runSamples is this run's own slice, not
		// shared with warmSamples' backing array (warmSamples grows by
		// append, copying), so sorting it here does not disturb pooling
		// order above.
		warmRunP95s = append(warmRunP95s, p95(runSamples))
	}

	// official is the sample set RouteLatency.P95 is computed from: the first
	// pass alone when there is no later data (Runs<=1, so this is
	// byte-for-byte what the gate has always computed), otherwise the pooled
	// warm samples — the cold pass's cache-cold connection makes it
	// unrepresentative of steady state, the same reasoning warmupRequests
	// already applies one level up.
	official := coldSamples
	if len(warmSamples) > 0 {
		official = warmSamples
	}
	result := RouteLatency{
		Route:          route,
		P95:            p95(append([]time.Duration(nil), official...)),
		Exercised:      !clientFailed,
		Status:         status,
		HardFailed:     hardFailed,
		HardFailedBody: hardFailedBody,
		Samples:        coldSamples,
		WarmSamples:    warmSamples,
		WarmRunP95s:    warmRunP95s,
	}
	if meter != nil {
		counters, err := meter.Read(ctx)
		if err != nil {
			return RouteLatency{}, fmt.Errorf("sweep %s: read work meter: %w", route, err)
		}
		n := float64(iterations * runs)
		result.Metered = true
		result.Work = WorkPerRequest{
			Calls: float64(counters.Calls) / n,
			Rows:  float64(counters.Rows) / n,
			Blks:  float64(counters.Blks) / n,
		}
	}
	return result, nil
}

// sweepOperation issues one request and returns its wall-clock duration and
// status code (0 for a timeout, which has no response).
//
// A response's status code does not fail the sweep, whatever it is: a
// no-arg GET route can legitimately answer 400 (a required query selector
// was omitted), 401/403 (this gate's key lacks that scope), or 404 (an
// unconfigured OAuth callback) without that being a defect in the route or
// this gate. sweepRoute uses the returned status to decide whether the
// route was exercised and whether it hard-failed, not to fail here.
//
// A client-side timeout is measured the same way, as a (large) latency
// sample: the #6793 infra-resource-aggregate regression manifested on the
// live instance as a hung connection (status 000, ~40s) rather than an error
// status, and that shape has to flow through to a budget breach, not abort
// the run before it is ever compared against a budget.
//
// Only a connection-level failure (nothing is listening, DNS failed) returns
// an error: that means eshu-api itself never came up, which is a gate setup
// problem, not a per-route latency signal.
func sweepOperation(client *http.Client, url, apiKey string, op Operation) (time.Duration, int, string, error) {
	req, err := http.NewRequest(op.Method, url, bytes.NewBufferString(op.Body))
	if err != nil {
		return 0, 0, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if op.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return elapsed, 0, "", nil
		}
		return 0, 0, "", fmt.Errorf("request %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := ""
	if op.MCP {
		const maxMCPResponseBytes = 8 << 20
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, maxMCPResponseBytes+1))
		elapsed = time.Since(start)
		if readErr != nil || len(b) > maxMCPResponseBytes {
			return elapsed, http.StatusInternalServerError, "MCP response body could not be read within 8 MiB", nil
		}
		body = string(b)
	} else if resp.StatusCode >= 500 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, hardFailedBodyCap))
		body = string(b)
	}
	if op.MCP && resp.StatusCode < 400 {
		var envelope struct {
			JSONRPC string `json:"jsonrpc"`
			Result  *struct {
				IsError bool `json:"isError"`
			} `json:"result"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(body), &envelope); err != nil || envelope.JSONRPC != "2.0" || envelope.Result == nil || len(envelope.Error) > 0 || envelope.Result.IsError {
			return elapsed, http.StatusInternalServerError, body, nil
		}
	}

	if op.MCP && len(body) > hardFailedBodyCap {
		body = body[:hardFailedBodyCap]
	}
	return elapsed, resp.StatusCode, body, nil
}

// percentile returns the nearest-rank p'th percentile of durations: the
// smallest value v such that at least p of samples are <= v, i.e. the
// ceil(p*n)'th smallest sample (1-indexed). durations is sorted in place.
// For p=0.95, n=20 this is index 18 (0-indexed) — the SECOND-highest sample,
// not the maximum — so a single outlier does not dominate the statistic.
func percentile(durations []time.Duration, p float64) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	rank := int(math.Ceil(p * float64(len(durations))))
	idx := rank - 1
	if idx >= len(durations) {
		idx = len(durations) - 1
	}
	if idx < 0 {
		idx = 0
	}
	return durations[idx]
}

// p95 returns percentile(durations, 0.95); kept as a named wrapper because
// "p95" is the term every doc comment, test name, and budget-table column in
// this package already uses.
func p95(durations []time.Duration) time.Duration {
	return percentile(durations, 0.95)
}
