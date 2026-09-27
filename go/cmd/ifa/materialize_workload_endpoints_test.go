// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

var errTestWorkloadEndpointsBackendDown = errors.New("test backend dial: connection refused")

type recordingWorkloadEndpointsBackend struct {
	executed []workloadEndpointsExecution
	workload int
	instance int
	edge     int
}

type workloadEndpointsExecution struct {
	cypher string
	params map[string]any
}

func (b *recordingWorkloadEndpointsBackend) ExecuteCypher(_ context.Context, cypher string, params map[string]any) error {
	b.executed = append(b.executed, workloadEndpointsExecution{cypher: cypher, params: params})
	return nil
}

func (b *recordingWorkloadEndpointsBackend) CountWorkload(_ context.Context, _ string) (int, error) {
	return b.workload, nil
}

func (b *recordingWorkloadEndpointsBackend) CountInstance(_ context.Context, _ string) (int, error) {
	return b.instance, nil
}

func (b *recordingWorkloadEndpointsBackend) CountInstanceOf(_ context.Context, _, _ string) (int, error) {
	return b.edge, nil
}

// TestMaterializeWorkloadEndpointsMergesExactNodes pins the seed the
// workload_cloud_relationship determinism cell depends on (#6228): the
// fixture Odu names anchors but no workload-domain facts, so the
// WorkloadInstance the writer MATCHes would never exist in the cell and the
// handler would fail instances_not_ready forever. The seed creates exactly
// the positive anchor's endpoints -- Workload, WorkloadInstance,
// INSTANCE_OF -- and nothing else: the negative and ambiguous anchors get
// no nodes, so the extractor's restraint (drop, never invent) still has
// something to prove live.
func TestMaterializeWorkloadEndpointsMergesExactNodes(t *testing.T) {
	t.Parallel()
	backend := &recordingWorkloadEndpointsBackend{workload: 1, instance: 1, edge: 1}
	instanceID, err := materializeWorkloadEndpoints(context.Background(), workloadEndpointsOptions{
		workloadID:  "workload:orders-api",
		environment: "prod",
	}, backend)
	if err != nil {
		t.Fatalf("materializeWorkloadEndpoints() error = %v", err)
	}
	// The instance id derivation is the single source of truth shared with
	// the guard mapper (materializededges): the writer MATCHes the instance,
	// so a seed that disagreed with the mapper would key nodes the edges
	// never attach to.
	const wantInstanceID = "workload-instance:orders-api:prod"
	if instanceID != wantInstanceID {
		t.Fatalf("instanceID = %q, want %q", instanceID, wantInstanceID)
	}
	if len(backend.executed) != 1 {
		t.Fatalf("ExecuteCypher calls = %d, want exactly 1 idempotent MERGE", len(backend.executed))
	}
	exec := backend.executed[0]
	for _, want := range []string{"MERGE (w:Workload {id: $workload_id})", "MERGE (i:WorkloadInstance {id: $instance_id})", "INSTANCE_OF"} {
		if !strings.Contains(exec.cypher, want) {
			t.Errorf("seed cypher missing %q:\n%s", want, exec.cypher)
		}
	}
	if got := exec.params["workload_id"]; got != "workload:orders-api" {
		t.Errorf("workload_id param = %v, want workload:orders-api", got)
	}
	if got := exec.params["instance_id"]; got != wantInstanceID {
		t.Errorf("instance_id param = %v, want %q", got, wantInstanceID)
	}
	if got := exec.params["environment"]; got != "prod" {
		t.Errorf("environment param = %v, want prod", got)
	}
}

// TestMaterializeWorkloadEndpointsFailsWhenVerifySeesNoInstance proves the
// graph postcondition is real: a seed whose write did not land (or landed
// elsewhere) must fail, never report success the live assert would then
// contradict with zero edges.
func TestMaterializeWorkloadEndpointsFailsWhenVerifySeesNoInstance(t *testing.T) {
	t.Parallel()
	backend := &recordingWorkloadEndpointsBackend{workload: 1, instance: 0, edge: 0}
	_, err := materializeWorkloadEndpoints(context.Background(), workloadEndpointsOptions{
		workloadID:  "workload:orders-api",
		environment: "prod",
	}, backend)
	if err == nil {
		t.Fatal("materializeWorkloadEndpoints() with 0 verified instances = nil, want error")
	}
	if !strings.Contains(err.Error(), "want 1/1/1") {
		t.Fatalf("error = %v, want it to report the failed graph postcondition", err)
	}
}

// TestMaterializeWorkloadEndpointsRejectsInvalidInputBeforeOpeningBackend
// proves flag validation precedes any backend I/O: a missing workload id or
// environment is a caller bug, not a graph outage, and must read that way.
func TestMaterializeWorkloadEndpointsRejectsInvalidInputBeforeOpeningBackend(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{},
		{"-workload-id", "workload:orders-api"},
		{"-environment", "prod"},
		{"-workload-id", "  ", "-environment", "prod"},
	} {
		if _, err := parseMaterializeWorkloadEndpointsFlags(args, io.Discard); err == nil {
			t.Errorf("parseMaterializeWorkloadEndpointsFlags(%v) = nil, want error", args)
		}
	}
}

// TestRunDispatchesMaterializeWorkloadEndpoints proves run() routes the new
// subcommand to its own flag set (a dispatch miss would surface as "unknown
// subcommand", silently dropping the seed the live cell depends on).
func TestRunDispatchesMaterializeWorkloadEndpoints(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{"materialize-workload-endpoints", "-bogus-flag"},
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("run(materialize-workload-endpoints) error = nil, want flag error")
	}
	if !strings.Contains(stderr.String(), "ifa materialize-workload-endpoints") {
		t.Fatalf("stderr = %q, want subcommand flag-set name", stderr.String())
	}
}

// TestRunMaterializeWorkloadEndpointsRedactsBackendConnectionTarget proves a
// down backend never leaks connection details into gate logs.
func TestRunMaterializeWorkloadEndpointsRedactsBackendConnectionTarget(t *testing.T) {
	t.Parallel()
	open := openWorkloadEndpointsBackend
	openWorkloadEndpointsBackend = func(_ context.Context) (workloadEndpointsBackend, func(), error) {
		return nil, func() {}, errTestWorkloadEndpointsBackendDown
	}
	defer func() { openWorkloadEndpointsBackend = open }()
	err := runMaterializeWorkloadEndpointsCommand(context.Background(), []string{"-workload-id", "workload:orders-api", "-environment", "prod"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("runMaterializeWorkloadEndpointsCommand() with down backend = nil, want error")
	}
	for _, leak := range []string{"bolt://", "7687", "nornicdb", "neo4j"} {
		if strings.Contains(strings.ToLower(err.Error()), leak) {
			t.Fatalf("error leaks connection detail %q: %v", leak, err)
		}
	}
}
