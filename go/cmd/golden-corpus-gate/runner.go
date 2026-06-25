// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// run is the gate entrypoint: parse flags, load the snapshot, execute the
// requested phases, write the report, and fail if any required finding failed.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	o, err := parseFlags(args)
	if err != nil {
		return err
	}
	snap, err := LoadSnapshot(o.snapshotPath)
	if err != nil {
		return err
	}

	phases := phaseSet(o.phase)
	var r Report

	if phases["drains"] {
		if err := runDrains(ctx, o, getenv, snap, &r, stderr); err != nil {
			return fmt.Errorf("drains phase: %w", err)
		}
	}
	if phases["graph"] {
		if err := runGraph(ctx, o, getenv, snap, &r); err != nil {
			return fmt.Errorf("graph phase: %w", err)
		}
	}
	if phases["query"] {
		if err := runQuery(ctx, o, getenv, snap, &r); err != nil {
			return fmt.Errorf("query phase: %w", err)
		}
	}
	if phases["timing"] && o.budgetSeconds > 0 {
		evaluateTiming(
			time.Duration(o.elapsedSeconds*float64(time.Second)),
			time.Duration(o.budgetSeconds*float64(time.Second)),
			o.budgetMultiplier, &r)
	}

	r.Write(stdout)
	if r.Failed() {
		return fmt.Errorf("gate failed: %d required check(s) did not pass", requiredFailures(r))
	}
	return nil
}

func runDrains(ctx context.Context, o options, getenv func(string) string, snap Snapshot, r *Report, stderr io.Writer) error {
	q, closeFn, err := openDrainQuerier(ctx, getenv, o.drainAdvisoryDomains)
	if err != nil {
		return err
	}
	defer closeFn()

	counts, drained, err := pollUntilDrained(ctx, q, snap.DrainAssertions, o.drainTimeout, o.drainPoll)
	if err != nil {
		return err
	}
	if !drained {
		fmt.Fprintf(stderr, "drains: timed out after %s with residual fact=%d intents=%d\n",
			o.drainTimeout, counts.FactWorkItemsResidual, counts.SharedIntentsNonterminal)
	}
	evaluateDrains(counts, snap.DrainAssertions, r)
	return nil
}

func runGraph(ctx context.Context, o options, getenv func(string) string, snap Snapshot, r *Report) error {
	counter, closeFn, err := openGraphCounter(ctx, getenv)
	if err != nil {
		return err
	}
	defer closeFn()
	if err := checkRequiredNodes(ctx, counter, splitCSV(o.requiredNodeLabels), r); err != nil {
		return err
	}
	return checkGraph(ctx, counter, snap, o.graphRequiredOnly, correlationSet(o.requiredCorrelations), r)
}

// splitCSV splits a comma-separated flag into trimmed, non-empty values.
func splitCSV(raw string) []string {
	out := []string{}
	for _, v := range strings.Split(raw, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// correlationSet parses the comma-separated blocking-correlation flag into a set.
func correlationSet(raw string) map[string]bool {
	set := map[string]bool{}
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			set[id] = true
		}
	}
	return set
}

func runQuery(ctx context.Context, o options, getenv func(string) string, snap Snapshot, r *Report) error {
	apiKey := strings.TrimSpace(getenv("ESHU_API_KEY"))
	client := newQueryClient(o.apiBaseURL, apiKey)
	return checkQuery(ctx, client, snap, r)
}

// phaseSet expands the comma-separated phase flag, treating "all" as every phase.
func phaseSet(raw string) map[string]bool {
	all := map[string]bool{"drains": true, "graph": true, "query": true, "timing": true}
	out := map[string]bool{}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "all" {
			return all
		}
		if all[p] {
			out[p] = true
		}
	}
	return out
}

func requiredFailures(r Report) int {
	n := 0
	for _, f := range r.Findings {
		if f.Required && !f.OK {
			n++
		}
	}
	return n
}
