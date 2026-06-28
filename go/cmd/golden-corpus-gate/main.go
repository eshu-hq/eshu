// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command golden-corpus-gate is the assertion core of the B-7 golden
// end-to-end corpus gate (issue #3800). Given a Postgres DSN, a graph backend,
// and a running eshu-api, it diffs a live pipeline run against the B-12 golden
// snapshot (testdata/golden/e2e-20repo-snapshot.json) and asserts the four B-7
// acceptance buckets:
//
//	(a) drains  — fact_work_items residual and shared_projection_intents
//	              nonterminal rows both reach their snapshot bound. The
//	              shared_projection_intents check is the B-13 (#3859) gate:
//	              fact_work_items == 0 alone misses held projection intents.
//	(b) graph   — required correlations exist (rc-1 deployable-unit, rc-3
//	              DEPENDS_ON, ...); in the full 20-repo mode
//	              (-graph-required-only=false) the node/edge counts are asserted
//	              as required against the snapshot tolerances (#3866).
//	(c) query   — canonical HTTP responses carry their required shape.
//	(d) timing  — pipeline wall time stays within a budget multiplier.
//
// The orchestration that runs the pipeline (bootstrap, cassette collectors,
// reducer drain) lives in scripts/verify-golden-corpus-gate.sh; this command is
// the typed, unit-tested assertion step that script invokes.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "golden-corpus-gate:", err)
		os.Exit(1)
	}
}

type options struct {
	snapshotPath            string
	phase                   string
	apiBaseURL              string
	mcpBaseURL              string
	drainTimeout            time.Duration
	drainPoll               time.Duration
	budgetSeconds           float64
	budgetMultiplier        float64
	elapsedSeconds          float64
	graphRequiredOnly       bool
	requiredCorrelations    string
	requiredNodeLabels      string
	drainAdvisoryDomains    string
	requirePopulatedDomains string
	phaseTimingsPath        string
	phaseBaselinePath       string
	phaseRegressionBand     float64
	phaseRegressionAdvisory bool
}

func parseFlags(args []string) (options, error) {
	fs := flag.NewFlagSet("golden-corpus-gate", flag.ContinueOnError)
	var o options
	fs.StringVar(&o.snapshotPath, "snapshot", "testdata/golden/e2e-20repo-snapshot.json", "path to the B-12 golden snapshot")
	fs.StringVar(&o.phase, "phase", "all", "comma-separated phases to run: drains,graph,query,timing,all")
	fs.StringVar(&o.apiBaseURL, "api-base-url", "http://localhost:8080", "base URL of a running eshu-api for query truth")
	fs.StringVar(&o.mcpBaseURL, "mcp-base-url", "", "base URL of a running eshu-mcp-server (http transport); when set, the query phase also asserts the snapshot's MCP tool query shapes live (#3866)")
	fs.DurationVar(&o.drainTimeout, "drain-timeout", 10*time.Minute, "max time to wait for queues to drain")
	fs.DurationVar(&o.drainPoll, "drain-poll", 2*time.Second, "interval between drain polls")
	fs.Float64Var(&o.budgetSeconds, "budget-seconds", 0, "baseline pipeline wall-time budget in seconds (0 disables timing)")
	fs.Float64Var(&o.budgetMultiplier, "budget-multiplier", 2.0, "allowed multiple of the baseline budget")
	fs.Float64Var(&o.elapsedSeconds, "elapsed-seconds", 0, "observed pipeline wall time in seconds (from the orchestrator)")
	fs.BoolVar(&o.graphRequiredOnly, "graph-required-only", true, "assert only corpus-size-independent correlations/nodes; when false (full 20-repo corpus) the node/edge count tolerances are asserted as required")
	fs.StringVar(&o.requiredCorrelations, "required-correlations", "", "comma-separated correlation IDs that fail the gate; others are advisory (empty = all advisory until the corpus produces them)")
	fs.StringVar(&o.requiredNodeLabels, "required-node-labels", "Repository", "comma-separated node labels that must each have >=1 node (graph-populated smoke check)")
	fs.StringVar(&o.drainAdvisoryDomains, "drain-advisory-domains", "", "comma-separated shared_projection_intents domains whose nonterminal rows are advisory, not blocking")
	fs.StringVar(&o.requirePopulatedDomains, "require-populated-domains", "", "comma-separated shared_projection_intents domains the reducer must be observed to emit before a drain is accepted (guards against draining an unreduced pipeline)")
	fs.StringVar(&o.phaseTimingsPath, "phase-timings-file", "", "path to the observed per-phase phase-timings.json emitted by the orchestrator; when set, the timing phase also runs the B-11 macro per-phase regression check")
	fs.StringVar(&o.phaseBaselinePath, "phase-baseline-file", "testdata/golden/e2e-baseline.json", "path to the committed B-11 per-phase baseline (used with -phase-timings-file)")
	fs.Float64Var(&o.phaseRegressionBand, "phase-regression-band", 0, "override the baseline's regression band (0 = use the band in the baseline file)")
	fs.BoolVar(&o.phaseRegressionAdvisory, "phase-regression-advisory", false, "downgrade per-phase regression findings to advisory (use on shared CI runners whose hardware variance exceeds the band; the committed baseline is captured on the controlled validation host)")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	return o, nil
}
