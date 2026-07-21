// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package conformance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"sigs.k8s.io/yaml"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// Run is the full contributor conformance flow: replay the cassette at
// cassettePath offline (zero credentials, zero Docker), derive the projected
// graph observation in memory, load the spec at specPath, and evaluate the
// observation against it with the shared goldengate assertions. The returned
// Report.Failed() is the pass/fail verdict; Report.Write renders the findings.
func Run(cassettePath, specPath string) (goldengate.Report, error) {
	envelopes, err := replayFacts(cassettePath)
	if err != nil {
		return goldengate.Report{}, err
	}
	obs, err := Observe(envelopes)
	if err != nil {
		return goldengate.Report{}, err
	}
	snap, err := LoadSpec(specPath)
	if err != nil {
		return goldengate.Report{}, err
	}
	return Evaluate(obs, snap), nil
}

// replayFacts replays every scope in the cassette through the shared
// cassette.Source — the same credential-free replay primitive the in-repo replay
// tiers use — and returns the flat envelope slice the observation consumes.
func replayFacts(cassettePath string) ([]facts.Envelope, error) {
	src, err := cassette.NewSource(cassettePath)
	if err != nil {
		return nil, fmt.Errorf("load cassette: %w", err)
	}
	ctx := context.Background()
	var out []facts.Envelope
	for {
		gen, ok, err := src.Next(ctx)
		if err != nil {
			return nil, fmt.Errorf("replay cassette: %w", err)
		}
		if !ok {
			break // all scopes drained (Source resets itself for the next poll)
		}
		for env := range gen.Facts {
			out = append(out, env)
		}
		if gen.FactStreamErr != nil {
			if err := gen.FactStreamErr(); err != nil {
				return nil, fmt.Errorf("cassette fact stream: %w", err)
			}
		}
	}
	return out, nil
}

// LoadSpec reads the contributor conformance spec (YAML) at path into the same
// goldengate.Snapshot contract the in-repo B-12 golden snapshot uses. It is
// parsed via sigs.k8s.io/yaml, which converts YAML to JSON and honours the
// snapshot's existing json tags, so one struct serves both the JSON golden
// snapshot and this YAML spec. A spec with no schema_version is a loud error,
// not a silently-empty contract that would make every assertion pass vacuously.
func LoadSpec(path string) (goldengate.Snapshot, error) {
	// #nosec G304 -- path is the contributor-supplied spec path (a test/CLI
	// argument), not user- or request-derived input.
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return goldengate.Snapshot{}, fmt.Errorf("read spec %q: %w", path, err)
	}
	var snap goldengate.Snapshot
	if err := yaml.Unmarshal(data, &snap); err != nil {
		return goldengate.Snapshot{}, fmt.Errorf("parse spec %q: %w", path, err)
	}
	if snap.SchemaVersion == "" {
		return goldengate.Snapshot{}, fmt.Errorf("spec %q: missing schema_version", path)
	}
	return snap, nil
}

// Evaluate runs the shared goldengate assertions over an observation against a
// spec snapshot and returns the accumulated report. It is the contributor
// analogue of the in-repo gate's checkGraph: it feeds in-memory observed values
// into the SAME Evaluate* functions instead of values read from a live graph, so
// there is no forked assertion logic. It mirrors checkGraph's qualifier handling
// exactly — a required correlation is narrowed by its evidence_kinds and each of
// its required_edge_properties is checked over the narrowed edges — so a
// contributor spec using those shared snapshot fields is asserted the same way
// offline as the live gate, never passing here on an edge the gate would reject.
// Node and edge count tolerances are asserted as required; required correlations,
// required nodes (with any property floor), and required self-loops (the two-
// sided [min,max] bound the live gate's checkRequiredSelfLoops enforces) are all
// blocking (no advisory tier offline).
func Evaluate(obs Observation, snap goldengate.Snapshot) goldengate.Report {
	var r goldengate.Report
	g := snap.Graph

	for _, label := range sortedRangeKeys(g.NodeCounts) {
		r.Add(goldengate.EvaluateNodeCount(label, g.NodeCounts[label], obs.NodeCounts[label], true))
	}
	for _, rel := range sortedRangeKeys(g.EdgeCounts) {
		r.Add(goldengate.EvaluateEdgeCount(rel, g.EdgeCounts[rel], obs.EdgeCounts[rel], true))
	}
	for _, rc := range g.RequiredCorrelations {
		// Narrow by evidence_kinds (when present) before counting and before
		// reading edge properties — the same edges the gate would count.
		matches := obs.matchingCorrelationEdges(rc)
		r.Add(goldengate.EvaluateRequiredCorrelation(rc, int64(len(matches)), true))
		for _, prop := range rc.RequiredEdgeProperties {
			values := edgePropertyValues(matches, prop)
			r.Add(goldengate.EvaluateEdgeProperty(rc, prop, values, rc.AllowedEdgePropertyValues[prop], true))
		}
	}
	for _, rn := range g.RequiredNodes {
		r.Add(goldengate.EvaluateRequiredNode(rn, obs.NodeCounts[rn.Label]))
		for _, prop := range rn.RequiredNodeProperties {
			values := obs.NodeProperty(rn.Label, prop)
			r.Add(goldengate.EvaluateNodeProperty(rn, prop, values, rn.AllowedNodePropertyValues[prop]))
		}
	}
	for _, rsl := range g.RequiredSelfLoops {
		// Feed the offline observed self-loop count into the SAME shared bound the
		// live gate uses, so a self-loop assertion is enforced (not silently
		// dropped) on the offline path too.
		r.Add(goldengate.EvaluateRequiredSelfLoop(rsl, obs.selfLoopCount(rsl)))
	}

	return r
}

// sortedRangeKeys returns the map keys in deterministic order so the report's
// finding order is stable across runs.
func sortedRangeKeys(m map[string]goldengate.CountRange) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
