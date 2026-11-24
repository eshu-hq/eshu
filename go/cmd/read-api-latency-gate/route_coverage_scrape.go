// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// impactReadRouteCounter is the exposition name of the per-route read counter the impact store
// records (go/internal/telemetry, eshu_dp_supply_chain_impact_read_route_total).
const impactReadRouteCounter = "eshu_dp_supply_chain_impact_read_route_total"

var (
	counterLine = regexp.MustCompile(`^` + impactReadRouteCounter + `(?:\{([^}]*)\})?\s+(\S+)`)
	routeLabel  = regexp.MustCompile(`(?:^|,)route="([^"]*)"`)
)

// parseRouteCounter reads the impact read-route counter out of Prometheus text exposition and
// returns the value per route label, summing series that differ only in other labels. A label
// with no series is absent from the map and reads as zero.
func parseRouteCounter(text string) map[string]float64 {
	out := map[string]float64{}
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		match := counterLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		label := routeLabel.FindStringSubmatch(match[1])
		if label == nil {
			continue
		}
		value, err := strconv.ParseFloat(match[2], 64)
		if err != nil {
			continue
		}
		out[label[1]] += value
	}
	return out
}

// scrapeRouteCounter fetches metricsURL and parses the read-route counter.
func scrapeRouteCounter(ctx context.Context, client *http.Client, metricsURL string) (map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build metrics request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scrape %s: %w", metricsURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrape %s: status %d", metricsURL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", metricsURL, err)
	}
	return parseRouteCounter(string(body)), nil
}

// routeRiseProblems compares the counter before and after one request: every declared label must
// have risen by at least one, and every other label by zero. An absent series reads as zero.
func routeRiseProblems(name string, before, after map[string]float64, declared []impact.ReadRoute) []string {
	want := map[string]bool{}
	for _, label := range declared {
		want[string(label)] = true
	}
	labels := map[string]bool{}
	for label := range before {
		labels[label] = true
	}
	for label := range after {
		labels[label] = true
	}
	for label := range want {
		labels[label] = true
	}
	names := make([]string, 0, len(labels))
	for label := range labels {
		names = append(names, label)
	}
	sort.Strings(names)
	var problems []string
	for _, label := range names {
		rise := after[label] - before[label]
		switch {
		case want[label] && rise < 1:
			problems = append(problems, fmt.Sprintf("%s: declared route %q rose by %g, want at least 1", name, label, rise))
		case !want[label] && rise != 0:
			problems = append(problems, fmt.Sprintf("%s: undeclared route %q rose by %g, want 0", name, label, rise))
		}
	}
	return problems
}

// legacyRouteProblems requires the legacy route not to rise over the whole sweep: direct evidence
// for the PR-C2 m inventory that nothing still reaches the legacy read.
func legacyRouteProblems(before, after map[string]float64) []string {
	if rise := after[string(impact.RouteLegacy)] - before[string(impact.RouteLegacy)]; rise != 0 {
		return []string{fmt.Sprintf("route %q rose by %g over the sweep, want 0: a call still reaches the legacy read", impact.RouteLegacy, rise)}
	}
	return nil
}
