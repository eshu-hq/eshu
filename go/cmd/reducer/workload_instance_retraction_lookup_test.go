// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestWorkloadInstanceRetractionLookupRendersDistinctUnwindVariable pins the
// fix for issue #6786 shape X9: an UNWIND variable name that equals a RETURN
// alias, with a MATCH in between. On NornicDB v1.3.3 that collision makes
// the RETURN's first column come back named after the first UNWIND value
// instead of its declared alias, so the production Go code's
// query.StringVal(row, "repo_id") always read "" and every row was
// silently dropped. The UNWIND binding must stay named requested_repo_id,
// distinct from the RETURN alias repo_id, and the MATCH must anchor on that
// binding, not on a re-declared "repo_id".
func TestWorkloadInstanceRetractionLookupRendersDistinctUnwindVariable(t *testing.T) {
	t.Parallel()

	reader := &recordingWorkloadDependencyGraphReader{
		rows: []map[string]any{
			{"repo_id": "repository:a", "instance_id": "workload-instance:a:prod"},
		},
	}
	lookup := neo4jWorkloadInstanceRetractionLookup{reader: reader}

	instances, err := lookup.ListWorkloadInstances(context.Background(), []string{"repository:a"}, reducer.EvidenceSourceWorkloads)
	if err != nil {
		t.Fatalf("ListWorkloadInstances() error = %v", err)
	}
	if got, want := len(instances), 1; got != want {
		t.Fatalf("len(instances) = %d, want %d", got, want)
	}
	if got, want := instances[0], (reducer.ExistingWorkloadInstance{RepoID: "repository:a", InstanceID: "workload-instance:a:prod"}); got != want {
		t.Fatalf("instances[0] = %#v, want %#v", got, want)
	}

	if !strings.Contains(reader.cypher, "UNWIND $repo_ids AS requested_repo_id") {
		t.Fatalf("cypher = %q, want the UNWIND binding named requested_repo_id (issue #6786 X9)", reader.cypher)
	}
	if !strings.Contains(reader.cypher, "MATCH (i:WorkloadInstance {repo_id: requested_repo_id})") {
		t.Fatalf("cypher = %q, want the MATCH anchored on the renamed UNWIND binding", reader.cypher)
	}
	if strings.Contains(reader.cypher, "AS repo_id") && strings.Contains(reader.cypher, "UNWIND $repo_ids AS repo_id") {
		t.Fatalf("cypher = %q, must not reintroduce an UNWIND variable that equals the RETURN alias repo_id (issue #6786 X9)", reader.cypher)
	}
	if !strings.Contains(reader.cypher, "RETURN DISTINCT i.repo_id AS repo_id, i.id AS instance_id") {
		t.Fatalf("cypher = %q, want the RETURN alias unchanged (repo_id, instance_id)", reader.cypher)
	}
}

// TestWorkloadInstanceRetractionLookupDropsRowsMissingRequiredFields proves
// the empty-field guard that made X9 a silent (not crashing) failure still
// works for genuinely incomplete rows -- it is the guard's presence in
// combination with the UNWIND/RETURN alias collision that made X9 silent
// rather than an obvious error; the guard itself is correct and must stay.
func TestWorkloadInstanceRetractionLookupDropsRowsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	reader := &recordingWorkloadDependencyGraphReader{
		rows: []map[string]any{
			{"repo_id": "", "instance_id": "workload-instance:a:prod"},
			{"repo_id": "repository:a", "instance_id": ""},
			{"repo_id": "repository:b", "instance_id": "workload-instance:b:prod"},
		},
	}
	lookup := neo4jWorkloadInstanceRetractionLookup{reader: reader}

	instances, err := lookup.ListWorkloadInstances(context.Background(), []string{"repository:a", "repository:b"}, reducer.EvidenceSourceWorkloads)
	if err != nil {
		t.Fatalf("ListWorkloadInstances() error = %v", err)
	}
	if got, want := len(instances), 1; got != want {
		t.Fatalf("len(instances) = %d, want %d (rows missing repo_id or instance_id must be dropped)", got, want)
	}
	if got, want := instances[0], (reducer.ExistingWorkloadInstance{RepoID: "repository:b", InstanceID: "workload-instance:b:prod"}); got != want {
		t.Fatalf("instances[0] = %#v, want %#v", got, want)
	}
}

// TestWorkloadInstanceRetractionLookupNilReaderOrEmptyRepoIDs proves the
// lookup degrades to no instances (not a panic or a graph read) when it has
// no reader or nothing to look up.
func TestWorkloadInstanceRetractionLookupNilReaderOrEmptyRepoIDs(t *testing.T) {
	t.Parallel()

	t.Run("nil reader", func(t *testing.T) {
		lookup := neo4jWorkloadInstanceRetractionLookup{}
		instances, err := lookup.ListWorkloadInstances(context.Background(), []string{"repository:a"}, reducer.EvidenceSourceWorkloads)
		if err != nil || instances != nil {
			t.Fatalf("ListWorkloadInstances() = (%#v, %v), want (nil, nil)", instances, err)
		}
	})

	t.Run("empty repo ids", func(t *testing.T) {
		reader := &recordingWorkloadDependencyGraphReader{}
		lookup := neo4jWorkloadInstanceRetractionLookup{reader: reader}
		instances, err := lookup.ListWorkloadInstances(context.Background(), nil, reducer.EvidenceSourceWorkloads)
		if err != nil || instances != nil {
			t.Fatalf("ListWorkloadInstances() = (%#v, %v), want (nil, nil)", instances, err)
		}
		if reader.cypher != "" {
			t.Fatalf("cypher = %q, want no graph read for an empty repo id list", reader.cypher)
		}
	})
}
