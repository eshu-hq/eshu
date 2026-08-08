// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

// A zero correlation currently reports what the GRAPH looks like: whether the
// edge type exists untyped, whether the endpoint labels have nodes, whether a
// retry agrees. That separates three causes and leaves a fourth unaddressed --
// the pipeline that writes the edge may never have run to completion.
//
// #5717 spent a day inside that gap. The diagnosis said "no edge of this type
// exists in the graph, nothing was written", both endpoint nodes passed their
// own assertions, and the read was stable. Every graph-side cause was excluded
// and the answer still was not in the output, because the question "did the
// producer finish?" is a Postgres question and the diagnosis only asked the
// graph.
//
// These tests pin the Postgres half: on a zero correlation, report whether the
// work items reached a terminal successful state, and if not, which domains did
// not. It stays advisory and failure-path only, exactly like its graph sibling.

type fakePipelineQuerier struct {
	rows []residualRow
	err  error
	// calls records invocations so a test can prove the read is not issued on a
	// passing correlation.
	calls int
}

func (f *fakePipelineQuerier) NonTerminalWorkItems(_ context.Context) ([]residualRow, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

// The case #5717 hit: every work item succeeded, so the producer DID run to
// completion and the edge is still absent. That narrows the fault to the write
// path itself, which is a different owner and a different fix than a stalled
// queue. Saying so is the whole point.
func TestPipelineDiagnosisReportsAllProducersCompleted(t *testing.T) {
	t.Parallel()

	q := &fakePipelineQuerier{rows: nil}
	got := diagnoseZeroCorrelationPipeline(context.Background(), q, "AWS_ec2_instance_uses_ami")

	if !strings.Contains(got, "every work item reached a terminal success") {
		t.Errorf("does not state that the producers completed: %s", got)
	}
	if !strings.Contains(got, "write path") {
		t.Errorf("does not point the reader at the write path: %s", got)
	}
}

// The opposite case: a producer never finished, so the missing edge is a
// consequence rather than a defect in the writer. Naming the domain is what
// makes it actionable.
func TestPipelineDiagnosisNamesTheUnfinishedDomain(t *testing.T) {
	t.Parallel()

	q := &fakePipelineQuerier{rows: []residualRow{
		{Domain: "aws_relationship_materialization", Status: "retrying", FailureClass: "nodes_not_ready", Count: 1},
	}}
	got := diagnoseZeroCorrelationPipeline(context.Background(), q, "AWS_ec2_instance_uses_ami")

	for _, want := range []string{"aws_relationship_materialization", "retrying", "nodes_not_ready"} {
		if !strings.Contains(got, want) {
			t.Errorf("breakdown missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "every work item reached a terminal success") {
		t.Errorf("claims completion while a domain is unfinished: %s", got)
	}
}

// Several unfinished domains must all be named. Reporting only the first would
// send the reader to fix one and rediscover the rest one run at a time.
func TestPipelineDiagnosisNamesEveryUnfinishedDomain(t *testing.T) {
	t.Parallel()

	q := &fakePipelineQuerier{rows: []residualRow{
		{Domain: "aws_resource_materialization", Status: "pending", Count: 2},
		{Domain: "aws_relationship_materialization", Status: "retrying", FailureClass: "nodes_not_ready", Count: 1},
	}}
	got := diagnoseZeroCorrelationPipeline(context.Background(), q, "AWS_ec2_instance_uses_ami")

	for _, want := range []string{"aws_resource_materialization", "aws_relationship_materialization"} {
		if !strings.Contains(got, want) {
			t.Errorf("breakdown missing %q: %s", want, got)
		}
	}
}

// A failed diagnostic read must degrade the message, never the verdict. The
// diagnosis exists to explain a failure that has already been decided; it must
// not introduce a second one.
func TestPipelineDiagnosisReportsItsOwnReadFailureInline(t *testing.T) {
	t.Parallel()

	q := &fakePipelineQuerier{err: errors.New("connection refused")}
	got := diagnoseZeroCorrelationPipeline(context.Background(), q, "AWS_ec2_instance_uses_ami")

	if !strings.Contains(got, "connection refused") {
		t.Errorf("swallows the read error instead of reporting it: %s", got)
	}
	if strings.Contains(got, "every work item reached a terminal success") {
		t.Errorf("asserts completion it could not verify: %s", got)
	}
}

// A nil querier means the gate has no Postgres handle in this phase. Skip
// silently rather than emit a confusing half-answer.
func TestPipelineDiagnosisSkipsWithoutAQuerier(t *testing.T) {
	t.Parallel()

	if got := diagnoseZeroCorrelationPipeline(context.Background(), nil, "AWS_x"); got != "" {
		t.Errorf("diagnoseZeroCorrelationPipeline(nil) = %q, want empty", got)
	}
}

// The wiring test. Everything above proves the formatter; this proves the
// formatter is reached. A zero correlation with a querier supplied must emit a
// "<rc-id>/pipeline" finding, and it must be advisory.
func TestCheckGraphEmitsThePipelineFindingOnAZeroCorrelation(t *testing.T) {
	t.Parallel()

	c := fakeCounter{
		nodes: map[string]int64{"CloudResource": 118},
		edges: map[string]int64{},
		corr:  map[string]int64{},
	}
	q := &fakePipelineQuerier{rows: []residualRow{
		{Domain: "aws_relationship_materialization", Status: "retrying", FailureClass: "nodes_not_ready", Count: 1},
	}}
	r := &goldengate.Report{}
	if err := checkGraph(context.Background(), c, zeroCorrelationSnapshot(), true,
		map[string]bool{"rc-test": true}, q, r); err != nil {
		t.Fatalf("checkGraph() error = %v", err)
	}

	var found *goldengate.Finding
	for i := range r.Findings {
		if strings.HasSuffix(r.Findings[i].Check, "/pipeline") {
			found = &r.Findings[i]
		}
	}
	if found == nil {
		t.Fatal("no /pipeline finding emitted — the diagnosis is not wired into checkGraph")
	}
	if found.Required {
		t.Error("/pipeline finding is Required=true; it must never change a verdict")
	}
	if !strings.Contains(found.Detail, "aws_relationship_materialization") {
		t.Errorf("/pipeline finding does not name the unfinished domain: %s", found.Detail)
	}
	if q.calls != 1 {
		t.Errorf("querier called %d times, want exactly 1", q.calls)
	}
}

// And it must NOT be reached on a passing correlation.
func TestCheckGraphSkipsThePipelineFindingWhenTheCorrelationPasses(t *testing.T) {
	t.Parallel()

	c := fakeCounter{
		nodes: map[string]int64{"CloudResource": 118},
		edges: map[string]int64{"AWS_ec2_instance_uses_ami": 1},
		corr:  map[string]int64{"CloudResource|AWS_ec2_instance_uses_ami|CloudResource": 1},
	}
	q := &fakePipelineQuerier{}
	r := &goldengate.Report{}
	if err := checkGraph(context.Background(), c, zeroCorrelationSnapshot(), true,
		map[string]bool{"rc-test": true}, q, r); err != nil {
		t.Fatalf("checkGraph() error = %v", err)
	}
	if q.calls != 0 {
		t.Errorf("querier called %d times on a passing correlation, want 0", q.calls)
	}
}
