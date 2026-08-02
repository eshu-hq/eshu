// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

// A blocking correlation that evaluates to zero currently reports one line:
// "count=0, want >= 1". That is true and nearly useless. Three different causes
// produce it -- the edge was never written, it was written between endpoints
// carrying different labels, or the read itself came back wrong -- and telling
// them apart afterwards has repeatedly cost hours, because the gate tears its
// stack down on exit and takes the evidence with it (#5717).
//
// These tests pin a diagnosis finding emitted alongside the failure, gathered
// while the graph is still up. It is advisory (Required=false) so it can never
// change a gate verdict: its only job is to make the next zero self-explaining.
//
// The three cases below are exactly the three the diagnosis must separate.

func zeroCorrelationSnapshot() goldengate.Snapshot {
	return goldengate.Snapshot{
		Graph: goldengate.GraphSnapshot{
			RequiredCorrelations: []goldengate.RequiredCorrelation{{
				ID:           "rc-test",
				FromLabel:    "CloudResource",
				Relationship: "AWS_ec2_instance_uses_ami",
				ToLabel:      "CloudResource",
				MinimumCount: 1,
			}},
		},
	}
}

func diagnosisDetail(t *testing.T, r *goldengate.Report) string {
	t.Helper()
	for _, f := range r.Findings {
		if strings.HasSuffix(f.Check, "/diagnosis") {
			if f.Required {
				t.Fatalf("diagnosis finding is Required=true; it must never change a verdict")
			}
			return f.Detail
		}
	}
	return ""
}

// Case 1: the relationship type does not exist in the graph at all. Nothing was
// written. This is the signature of a lost write.
func TestZeroCorrelationDiagnosisReportsAbsentRelationship(t *testing.T) {
	t.Parallel()

	c := fakeCounter{
		nodes: map[string]int64{"CloudResource": 118},
		edges: map[string]int64{}, // untyped count 0 -- no such edge, any labels
		corr:  map[string]int64{},
	}
	r := &goldengate.Report{}
	if err := checkGraph(context.Background(), c, zeroCorrelationSnapshot(), true,
		map[string]bool{"rc-test": true}, r); err != nil {
		t.Fatalf("checkGraph() error = %v, want nil", err)
	}

	detail := diagnosisDetail(t, r)
	if detail == "" {
		t.Fatal("no diagnosis finding emitted for a zero blocking correlation")
	}
	if !strings.Contains(detail, "untyped=0") {
		t.Errorf("diagnosis does not report the untyped edge count: %s", detail)
	}
	if !strings.Contains(detail, "no edge of this type exists") {
		t.Errorf("diagnosis does not name the absent-relationship cause: %s", detail)
	}
}

// Case 2: the edge exists, but not between the asserted labels. The write
// happened; the endpoints are not what the assertion expects.
func TestZeroCorrelationDiagnosisReportsLabelMismatch(t *testing.T) {
	t.Parallel()

	c := fakeCounter{
		nodes: map[string]int64{"CloudResource": 118},
		edges: map[string]int64{"AWS_ec2_instance_uses_ami": 1}, // exists untyped
		corr:  map[string]int64{},                               // but not between CloudResource endpoints
	}
	r := &goldengate.Report{}
	if err := checkGraph(context.Background(), c, zeroCorrelationSnapshot(), true,
		map[string]bool{"rc-test": true}, r); err != nil {
		t.Fatalf("checkGraph() error = %v, want nil", err)
	}

	detail := diagnosisDetail(t, r)
	if !strings.Contains(detail, "untyped=1") {
		t.Errorf("diagnosis does not report the untyped edge count: %s", detail)
	}
	if !strings.Contains(detail, "endpoint labels do not match") {
		t.Errorf("diagnosis does not name the label-mismatch cause: %s", detail)
	}
}

// Case 3: an endpoint label has no nodes at all, so the correlation could never
// match regardless of the edge. Distinct from case 2 and worth naming, because
// it points at the node writer rather than the edge writer.
func TestZeroCorrelationDiagnosisReportsMissingEndpointNodes(t *testing.T) {
	t.Parallel()

	snap := zeroCorrelationSnapshot()
	snap.Graph.RequiredCorrelations[0].ToLabel = "MachineImage"
	c := fakeCounter{
		nodes: map[string]int64{"CloudResource": 118, "MachineImage": 0},
		edges: map[string]int64{},
		corr:  map[string]int64{},
	}
	r := &goldengate.Report{}
	if err := checkGraph(context.Background(), c, snap, true,
		map[string]bool{"rc-test": true}, r); err != nil {
		t.Fatalf("checkGraph() error = %v, want nil", err)
	}

	detail := diagnosisDetail(t, r)
	if !strings.Contains(detail, "MachineImage=0") {
		t.Errorf("diagnosis does not report the empty endpoint label count: %s", detail)
	}
}

// A passing correlation must emit no diagnosis. The diagnosis costs extra graph
// reads, so it runs only where it earns them.
func TestZeroCorrelationDiagnosisSkippedWhenCorrelationPasses(t *testing.T) {
	t.Parallel()

	c := fakeCounter{
		nodes: map[string]int64{"CloudResource": 118},
		edges: map[string]int64{"AWS_ec2_instance_uses_ami": 1},
		corr:  map[string]int64{"CloudResource|AWS_ec2_instance_uses_ami|CloudResource": 1},
	}
	r := &goldengate.Report{}
	if err := checkGraph(context.Background(), c, zeroCorrelationSnapshot(), true,
		map[string]bool{"rc-test": true}, r); err != nil {
		t.Fatalf("checkGraph() error = %v, want nil", err)
	}

	if detail := diagnosisDetail(t, r); detail != "" {
		t.Errorf("diagnosis emitted for a passing correlation: %s", detail)
	}
}

// An advisory (non-blocking) correlation reading zero also gets a diagnosis --
// an advisory zero is exactly how a real regression first shows up, and the
// reads are cheap. What it must NOT do is become required.
func TestZeroCorrelationDiagnosisStaysAdvisoryForNonBlocking(t *testing.T) {
	t.Parallel()

	c := fakeCounter{
		nodes: map[string]int64{"CloudResource": 118},
		edges: map[string]int64{},
		corr:  map[string]int64{},
	}
	r := &goldengate.Report{}
	if err := checkGraph(context.Background(), c, zeroCorrelationSnapshot(), true,
		map[string]bool{}, r); err != nil {
		t.Fatalf("checkGraph() error = %v, want nil", err)
	}

	if detail := diagnosisDetail(t, r); detail == "" {
		t.Fatal("no diagnosis emitted for a zero advisory correlation")
	}
}

// When the assertion was evidence-filtered, the retry must use the SAME query.
// Dropping the filter reads a shared relationship's other evidence kinds and
// reports read instability where the real finding is a stable evidence-kind
// regression (codex/Copilot review of #5902).
func TestZeroCorrelationDiagnosisRetriesWithTheEvidenceFilter(t *testing.T) {
	t.Parallel()

	snap := zeroCorrelationSnapshot()
	snap.Graph.RequiredCorrelations[0].Relationship = "DEPLOYS_FROM"
	snap.Graph.RequiredCorrelations[0].EvidenceKinds = []string{"argocd"}

	c := fakeCounter{
		nodes: map[string]int64{"CloudResource": 118},
		edges: map[string]int64{"DEPLOYS_FROM": 4},
		// Unfiltered count is nonzero (other evidence kinds exist)...
		corr: map[string]int64{"CloudResource|DEPLOYS_FROM|CloudResource": 4},
		// ...but the asserted evidence kind genuinely has none.
		corrEv: map[string]int64{},
	}
	r := &goldengate.Report{}
	if err := checkGraph(context.Background(), c, snap, true,
		map[string]bool{"rc-test": true}, r); err != nil {
		t.Fatalf("checkGraph() error = %v, want nil", err)
	}

	detail := diagnosisDetail(t, r)
	if strings.Contains(detail, "read instability") {
		t.Errorf("retry dropped the evidence filter and misread a stable regression: %s", detail)
	}
	if !strings.Contains(detail, "evidence") {
		t.Errorf("diagnosis does not name the evidence-kind narrowing as the cause: %s", detail)
	}
}
