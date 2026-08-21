// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

func TestRepoDependencyCassetteProductionAdmission(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "..", "testdata", "cassettes", "repodependency", "ifa-repo-dependency-family.json")
	source, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("cassette.NewSource: %v", err)
	}

	wantRepoIDs := []string{
		"repo-ifa-repo-dependency-source",
		"repo-ifa-repo-dependency-target-dependson",
		"repo-ifa-repo-dependency-target-deploysfrom",
		"repo-ifa-repo-dependency-target-discoversconfig",
		"repo-ifa-repo-dependency-target-provisions",
		"repo-ifa-repo-dependency-target-readsconfig",
		"repo-ifa-repo-dependency-target-usesmodule",
	}
	var gotRepoIDs []string
	generationCount := 0
	for {
		collected, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Source.Next: %v", err)
		}
		if !ok {
			break
		}
		generationCount++
		var envelopes []facts.Envelope
		for envelope := range collected.Facts {
			envelopes = append(envelopes, envelope)
		}
		projection, err := buildProjection(collected.Scope, collected.Generation, envelopes)
		if err != nil {
			t.Fatalf("buildProjection(%s): %v", collected.Scope.ScopeID, err)
		}
		if projection.canonical.Repository != nil {
			gotRepoIDs = append(gotRepoIDs, projection.canonical.Repository.RepoID)
		}

		isSource := collected.Scope.Metadata["repo_id"] == "repo-ifa-repo-dependency-source" ||
			(projection.canonical.Repository != nil && projection.canonical.Repository.RepoID == "repo-ifa-repo-dependency-source")
		if !isSource && len(projection.reducerIntents) != 0 {
			t.Errorf("target scope %q emitted reducer intents: %+v", collected.Scope.ScopeID, projection.reducerIntents)
		}
		if isSource {
			wantIntents := map[reducer.Domain]string{
				reducer.DomainWorkloadMaterialization: "workload:repo-dependency-family-source",
				reducer.DomainDeploymentMapping:       "deployment:repo-dependency-family-source",
			}
			if got, want := len(projection.reducerIntents), len(wantIntents); got != want {
				t.Errorf("source reducer intent count = %d, want exactly %d: %+v", got, want, projection.reducerIntents)
			}
			gotIntents := make(map[reducer.Domain]string)
			for _, intent := range projection.reducerIntents {
				if _, required := wantIntents[intent.Domain]; !required {
					t.Errorf("source emitted unexpected reducer domain %s", intent.Domain)
					continue
				}
				if _, duplicate := gotIntents[intent.Domain]; duplicate {
					t.Errorf("source emitted duplicate reducer intent for %s", intent.Domain)
				}
				if intent.FactID == "" {
					t.Errorf("source %s intent has empty fact ID", intent.Domain)
				}
				gotIntents[intent.Domain] = intent.EntityKey
			}
			if len(gotIntents) != len(wantIntents) {
				t.Errorf("source domains=%v, want workload_materialization and deployment_mapping", gotIntents)
			}
			for domain, wantKey := range wantIntents {
				if got := gotIntents[domain]; got != wantKey {
					t.Errorf("source %s entity key = %q, want %q", domain, got, wantKey)
				}
			}
		}
	}

	sort.Strings(gotRepoIDs)
	if generationCount != 7 {
		t.Errorf("generation count = %d, want 7", generationCount)
	}
	if len(gotRepoIDs) != len(wantRepoIDs) {
		t.Fatalf("canonical repositories = %v, want %v", gotRepoIDs, wantRepoIDs)
	}
	for i := range wantRepoIDs {
		if gotRepoIDs[i] != wantRepoIDs[i] {
			t.Errorf("canonical repositories = %v, want %v", gotRepoIDs, wantRepoIDs)
			break
		}
	}
}
