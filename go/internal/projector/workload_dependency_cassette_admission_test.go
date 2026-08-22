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

func TestWorkloadDependencyCassetteProductionAdmission(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "..", "testdata", "cassettes", "workloaddependency", "ifa-workload-dependency-family.json")
	loaded, err := cassette.LoadFile(path)
	if err != nil {
		t.Fatalf("cassette.LoadFile: %v", err)
	}
	if got, want := len(loaded.Scopes), 6; got != want {
		t.Fatalf("cassette scopes = %d, want %d (one repository per production scope)", got, want)
	}

	source, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("cassette.NewSource: %v", err)
	}
	wantRepoIDs := []string{
		"repo-ifa-workload-dependency-multi-source",
		"repo-ifa-workload-dependency-multi-target",
		"repo-ifa-workload-dependency-orphan-source",
		"repo-ifa-workload-dependency-orphan-target",
		"repo-ifa-workload-dependency-source",
		"repo-ifa-workload-dependency-target",
	}
	wantWorkloadRepoIDs := []string{
		"repo-ifa-workload-dependency-multi-source",
		"repo-ifa-workload-dependency-multi-target",
		"repo-ifa-workload-dependency-source",
		"repo-ifa-workload-dependency-target",
	}
	wantWorkloadRepos := make(map[string]struct{}, len(wantWorkloadRepoIDs))
	for _, repoID := range wantWorkloadRepoIDs {
		wantWorkloadRepos[repoID] = struct{}{}
	}
	wantIntentsByRepo := map[string]map[reducer.Domain]string{
		"repo-ifa-workload-dependency-source": {
			reducer.DomainDeploymentMapping:       "workload:workload-dependency-source",
			reducer.DomainWorkloadMaterialization: "workload:workload-dependency-source",
		},
		"repo-ifa-workload-dependency-target": {
			reducer.DomainWorkloadMaterialization: "workload:workload-dependency-target",
		},
		"repo-ifa-workload-dependency-multi-source": {
			reducer.DomainDeploymentMapping:       "workload:workload-dependency-multi-source",
			reducer.DomainWorkloadMaterialization: "workload:workload-dependency-multi-source",
		},
		"repo-ifa-workload-dependency-multi-target": {
			reducer.DomainWorkloadMaterialization: "workload:workload-dependency-multi-target",
		},
		"repo-ifa-workload-dependency-orphan-source": {
			reducer.DomainDeploymentMapping: "workload:workload-dependency-orphan-source",
		},
		"repo-ifa-workload-dependency-orphan-target": {},
	}

	var allEnvelopes []facts.Envelope
	var gotRepoIDs, gotFileRepoIDs []string
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
		allEnvelopes = append(allEnvelopes, envelopes...)

		projection, err := buildProjection(collected.Scope, collected.Generation, envelopes)
		if err != nil {
			t.Fatalf("buildProjection(%s): %v", collected.Scope.ScopeID, err)
		}
		if projection.canonical.Repository == nil {
			t.Errorf("scope %q emitted no canonical repository", collected.Scope.ScopeID)
			continue
		}
		repoID := projection.canonical.Repository.RepoID
		gotRepoIDs = append(gotRepoIDs, repoID)
		if got := collected.Scope.Metadata["repo_id"]; got != repoID {
			t.Errorf("scope %q metadata repo_id = %q, canonical repository = %q", collected.Scope.ScopeID, got, repoID)
		}
		for _, file := range projection.canonical.Files {
			gotFileRepoIDs = append(gotFileRepoIDs, file.RepoID)
			if file.RepoID != repoID {
				t.Errorf("scope %q canonical file repo_id = %q, repository = %q", collected.Scope.ScopeID, file.RepoID, repoID)
			}
		}
		scopeCandidates, scopeDeploymentEnvironments := reducer.ExtractWorkloadCandidates(envelopes)
		scopeWorkloads := reducer.BuildProjectionRows(scopeCandidates, scopeDeploymentEnvironments)
		_, wantWorkload := wantWorkloadRepos[repoID]
		if wantWorkload {
			if got, want := len(scopeWorkloads.RepoDescriptors), 1; got != want {
				t.Errorf("repository %q scope workload descriptors = %d, want %d", repoID, got, want)
			} else if got := scopeWorkloads.RepoDescriptors[0].RepoID; got != repoID {
				t.Errorf("repository %q scope workload descriptor repo_id = %q", repoID, got)
			}
		} else if len(scopeWorkloads.RepoDescriptors) != 0 {
			t.Errorf("repository %q unexpectedly admitted scope workloads: %+v", repoID, scopeWorkloads.RepoDescriptors)
		}

		wantIntents := wantIntentsByRepo[repoID]
		if got, want := len(projection.reducerIntents), len(wantIntents); got != want {
			t.Errorf("repository %q reducer intent count = %d, want exactly %d: %+v", repoID, got, want, projection.reducerIntents)
		}
		seenIntents := make(map[reducer.Domain]struct{}, len(projection.reducerIntents))
		for _, intent := range projection.reducerIntents {
			wantKey, ok := wantIntents[intent.Domain]
			if !ok {
				t.Errorf("repository %q emitted unexpected reducer domain %s", repoID, intent.Domain)
				continue
			}
			if _, duplicate := seenIntents[intent.Domain]; duplicate {
				t.Errorf("repository %q emitted duplicate reducer domain %s", repoID, intent.Domain)
			}
			seenIntents[intent.Domain] = struct{}{}
			if intent.EntityKey != wantKey {
				t.Errorf("repository %q %s entity key = %q, want %q", repoID, intent.Domain, intent.EntityKey, wantKey)
			}
			if intent.FactID == "" {
				t.Errorf("repository %q %s intent has empty fact ID", repoID, intent.Domain)
			}
		}
	}

	sort.Strings(gotRepoIDs)
	sort.Strings(gotFileRepoIDs)
	if generationCount != 6 {
		t.Errorf("generation count = %d, want 6", generationCount)
	}
	assertSortedStringsEqual(t, "canonical repositories", gotRepoIDs, wantRepoIDs)
	assertSortedStringsEqual(t, "canonical workload files", gotFileRepoIDs, wantWorkloadRepoIDs)

	candidates, deploymentEnvironments := reducer.ExtractWorkloadCandidates(allEnvelopes)
	projection := reducer.BuildProjectionRows(candidates, deploymentEnvironments)
	gotDescriptorRepoIDs := make([]string, 0, len(projection.RepoDescriptors))
	for _, descriptor := range projection.RepoDescriptors {
		gotDescriptorRepoIDs = append(gotDescriptorRepoIDs, descriptor.RepoID)
	}
	sort.Strings(gotDescriptorRepoIDs)
	assertSortedStringsEqual(t, "workload repository descriptors", gotDescriptorRepoIDs, wantWorkloadRepoIDs)
}

func assertSortedStringsEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", label, got, want)
		}
	}
}
