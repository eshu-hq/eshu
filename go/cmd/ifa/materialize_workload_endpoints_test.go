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
//
// The seed keys the ladder-resolved scoped identity (#8516), not the
// cassette's bare anchor: after the #5385 flip the writer only ever MATCHes
// resolved scoped ids, so a bare-handle node is one the edges never attach
// to. The name and repo_id properties are what the sibling lookup reads to
// resolve the cassette's bare anchor to this node.
func TestMaterializeWorkloadEndpointsMergesExactNodes(t *testing.T) {
	t.Parallel()
	backend := &recordingWorkloadEndpointsBackend{workload: 1, instance: 1, edge: 1}
	instanceID, err := materializeWorkloadEndpoints(context.Background(), workloadEndpointsOptions{
		workloadID:   "workload:repo-orders:orders-api",
		workloadName: "orders-api",
		repoID:       "repo-orders",
		environment:  "prod",
	}, backend)
	if err != nil {
		t.Fatalf("materializeWorkloadEndpoints() error = %v", err)
	}
	// The instance id derivation is the single source of truth shared with
	// the guard mapper (materializededges): the writer MATCHes the instance,
	// so a seed that disagreed with the mapper would key nodes the edges
	// never attach to.
	const wantInstanceID = "workload-instance:repo-orders:orders-api:prod"
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
	// The sibling lookup resolves the cassette's bare anchor by name over
	// candidates carrying id, name, and repo_id: a seeded node missing any
	// of the three is invisible to the ladder and the row stays a bare
	// handle the writer never matches.
	for _, want := range []string{"w.name = $workload_name", "w.repo_id = $repo_id"} {
		if !strings.Contains(exec.cypher, want) {
			t.Errorf("seed cypher missing %q:\n%s", want, exec.cypher)
		}
	}
	if got := exec.params["workload_id"]; got != "workload:repo-orders:orders-api" {
		t.Errorf("workload_id param = %v, want workload:repo-orders:orders-api", got)
	}
	if got := exec.params["workload_name"]; got != "orders-api" {
		t.Errorf("workload_name param = %v, want orders-api", got)
	}
	if got := exec.params["repo_id"]; got != "repo-orders" {
		t.Errorf("repo_id param = %v, want repo-orders", got)
	}
	if got := exec.params["instance_id"]; got != wantInstanceID {
		t.Errorf("instance_id param = %v, want %q", got, wantInstanceID)
	}
	if got := exec.params["environment"]; got != "prod" {
		t.Errorf("environment param = %v, want prod", got)
	}
}

// TestParseMaterializeWorkloadEndpointsFlagsRejectsBareHandle proves the
// seed fails closed on the legacy shape (#8516): a bare handle carries no
// repository segment, so the seeded node is not an id the post-#5385 writer
// ever matches and the cell would fail later with zero edges and no pointer
// back here. The rejection happens at flag parse, before any backend I/O.
func TestParseMaterializeWorkloadEndpointsFlagsRejectsBareHandle(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"workload:orders-api", "orders-api", "workload:", "workload::orders-api", "workload:repo-orders:", "workload:repo-orders:or:ders"} {
		options, err := parseMaterializeWorkloadEndpointsFlags([]string{"-workload-id", id, "-environment", "prod"}, io.Discard)
		if err == nil {
			t.Errorf("parseMaterializeWorkloadEndpointsFlags(-workload-id %q) = %+v, want scoped-id error", id, options)
			continue
		}
		if !strings.Contains(err.Error(), "workload:<repo_id>:<name>") {
			t.Errorf("parseMaterializeWorkloadEndpointsFlags(-workload-id %q) error = %v, want it to name the scoped shape", id, err)
		}
	}
}

// TestParseMaterializeWorkloadEndpointsFlagsSplitsScopedID proves a scoped
// id parses to the repository and name segments the seed cypher SETs on the
// node for the sibling lookup.
func TestParseMaterializeWorkloadEndpointsFlagsSplitsScopedID(t *testing.T) {
	t.Parallel()
	options, err := parseMaterializeWorkloadEndpointsFlags([]string{"-workload-id", "workload:repo-orders:orders-api", "-environment", "prod"}, io.Discard)
	if err != nil {
		t.Fatalf("parseMaterializeWorkloadEndpointsFlags(scoped) error = %v", err)
	}
	if options.repoID != "repo-orders" || options.workloadName != "orders-api" {
		t.Fatalf("options = %+v, want repoID repo-orders workloadName orders-api", options)
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
		workloadID:   "workload:repo-orders:orders-api",
		workloadName: "orders-api",
		repoID:       "repo-orders",
		environment:  "prod",
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
		{"-workload-id", "workload:repo-orders:orders-api"},
		{"-environment", "prod"},
		{"-workload-id", "  ", "-environment", "prod"},
		{"-workload-id", "workload:orders-api", "-environment", "prod"},
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
	// No t.Parallel: this test swaps the package-global
	// openWorkloadEndpointsBackend, which would race under -race with any
	// future parallel test touching that global (matches the precedent
	// file's no-Parallel swap tests).
	open := openWorkloadEndpointsBackend
	openWorkloadEndpointsBackend = func(_ context.Context) (workloadEndpointsBackend, func(), error) {
		return nil, func() {}, errTestWorkloadEndpointsBackendDown
	}
	defer func() { openWorkloadEndpointsBackend = open }()
	err := runMaterializeWorkloadEndpointsCommand(context.Background(), []string{"-workload-id", "workload:repo-orders:orders-api", "-environment", "prod"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("runMaterializeWorkloadEndpointsCommand() with down backend = nil, want error")
	}
	for _, leak := range []string{"bolt://", "7687", "nornicdb", "neo4j"} {
		if strings.Contains(strings.ToLower(err.Error()), leak) {
			t.Fatalf("error leaks connection detail %q: %v", leak, err)
		}
	}
}
