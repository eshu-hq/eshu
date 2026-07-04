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
	if phases["timing"] {
		if o.budgetSeconds > 0 {
			EvaluateTiming(
				time.Duration(o.elapsedSeconds*float64(time.Second)),
				time.Duration(o.budgetSeconds*float64(time.Second)),
				o.budgetMultiplier, &r)
		}
		// B-11 (#3804): when the orchestrator emits per-phase timings, assert each
		// phase against the committed baseline. Complements the total-wall-time
		// budget above — total catches a gross slowdown, per-phase catches a
		// regression localized to one phase that the 2x total budget would hide.
		if o.phaseTimingsPath != "" {
			observed, err := LoadPhaseTimings(o.phaseTimingsPath)
			if err != nil {
				return fmt.Errorf("timing phase: %w", err)
			}
			baseline, err := LoadPhaseBaseline(o.phaseBaselinePath)
			if err != nil {
				return fmt.Errorf("timing phase: %w", err)
			}
			evaluatePhaseTimings(observed, baseline, o.phaseRegressionBand, o.phaseRegressionAdvisory, &r)
		}
	}

	r.Write(stdout)
	if r.Failed() {
		return fmt.Errorf("gate failed: %d required check(s) did not pass", requiredFailures(r))
	}
	return nil
}

func runDrains(ctx context.Context, o options, getenv func(string) string, snap Snapshot, r *Report, stderr io.Writer) error {
	// Normalize the domain lists (trim each element) so a flag like "a, b" cannot
	// leave a leading space that fails to match a domain name.
	advisory := strings.Join(splitCSV(o.drainAdvisoryDomains), ",")
	populatedDomains := splitCSV(o.requirePopulatedDomains)
	populatedCSV := strings.Join(populatedDomains, ",")
	q, closeFn, err := openDrainQuerier(ctx, getenv, advisory, populatedCSV)
	if err != nil {
		return err
	}
	defer closeFn()

	counts, ok, err := pollUntilDrained(ctx, q, snap.DrainAssertions, len(populatedDomains), o.drainTimeout, o.drainPoll)
	if err != nil {
		return err
	}
	if !ok {
		_, _ = fmt.Fprintf(stderr, "drains: not satisfied after %s (fact residual=%d, required intents=%d, populated domains=%d/%d)\n",
			o.drainTimeout, counts.FactWorkItemsResidual, counts.SharedIntentsRequiredNonterminal,
			counts.PopulatedDomainsPresent, len(populatedDomains))
	}
	EvaluateDrains(counts, snap.DrainAssertions, len(populatedDomains), r)
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
	return checkGraph(ctx, counter, snap, o.graphRequiredOnly, resolveBlockingCorrelations(o.requiredCorrelations, snap.Graph.RequiredCorrelations), r)
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

// resolveBlockingCorrelations turns the -required-correlations flag into the
// set of correlation IDs whose failure blocks the gate (the rest are
// advisory). Three forms (#4596):
//
//   - "" (empty, the default): nothing blocks — all correlations are
//     advisory until explicitly promoted. Preserves the pre-#4596 default.
//   - "all": every ID in the snapshot's own required_correlations is
//     blocking. This is the single-source form: promoting an rc-N to
//     blocking becomes a one-file edit (the snapshot) instead of the
//     historical two-file hand-edit (the snapshot plus a duplicated
//     comma-separated mirror in scripts/verify-golden-corpus-gate.sh that had
//     to be kept in lockstep by hand on every addition).
//   - an explicit comma-separated id list: exactly those ids block,
//     independent of snapshot content. Kept as an escape hatch for staging a
//     newly-added rc as advisory-only before it is proven, or for blocking a
//     strict subset.
func resolveBlockingCorrelations(raw string, snapshotCorrelations []RequiredCorrelation) map[string]bool {
	if strings.TrimSpace(raw) == "all" {
		set := make(map[string]bool, len(snapshotCorrelations))
		for _, rc := range snapshotCorrelations {
			if id := strings.TrimSpace(rc.ID); id != "" {
				set[id] = true
			}
		}
		return set
	}
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
	if err := checkQuery(ctx, client, snap, r); err != nil {
		return err
	}
	EvaluateQuerySurfaceParity(snap, r)
	// When an MCP server URL is supplied, additionally assert the snapshot's MCP
	// tool query shapes live through the MCP tool layer (#3866 criterion 4), not
	// just the HTTP routes those tools proxy to.
	if strings.TrimSpace(o.mcpBaseURL) != "" {
		if err := checkMCPQuery(ctx, newMCPClient(o.mcpBaseURL, apiKey), snap, r); err != nil {
			return fmt.Errorf("mcp query shapes: %w", err)
		}
	}
	return nil
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
