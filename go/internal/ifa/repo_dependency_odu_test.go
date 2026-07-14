// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

const (
	testRepoDependencyConcurrencyOduName = "odu:repo-dependency-concurrency"
	testRepoDependencySourceCount        = 8
	testRepoDependencyContentPath        = "env/ifa-prod-proof/main.tf"
)

func TestRepoDependencyConcurrencyOduProductionEvidence(t *testing.T) {
	t.Parallel()

	odu, ok := CatalogByName()[testRepoDependencyConcurrencyOduName]
	if !ok {
		t.Fatalf("CatalogByName() is missing %q", testRepoDependencyConcurrencyOduName)
	}
	if got, want := len(odu.Facts), 29; got != want {
		t.Fatalf("len(Facts) = %d, want %d", got, want)
	}

	catalog := RepositoryCatalog(odu.Facts)
	if got, want := len(catalog), 11; got != want {
		t.Fatalf("len(RepositoryCatalog(Facts)) = %d, want %d", got, want)
	}
	registry := make(map[string]facts.FactKindRegistryEntry)
	for _, entry := range facts.FactKindRegistry() {
		registry[entry.Kind] = entry
	}
	if err := ValidateOduPayloads(odu, registry); err != nil {
		t.Fatalf("ValidateOduPayloads() error = %v", err)
	}

	assertRepoDependencySourceFacts(t, odu)
	assertRepoDependencyHostileAliases(t, odu)
	assertRepoDependencyEvidence(t, DiscoveredEvidence(odu))
}

func assertRepoDependencyHostileAliases(t *testing.T, odu Odu) {
	t.Helper()
	want := map[string]string{
		"env/ifa-prod-proof/self.tf":   `app_repo = "source-01"` + "\n",
		"env/ifa-prod-proof/prefix.tf": `app_repo = "target-07-extra"` + "\n",
	}
	for _, fact := range odu.Facts {
		if fact.FactKind != contentFactKind {
			continue
		}
		path := strings.TrimSpace(fmt.Sprint(fact.Payload["content_path"]))
		body := fmt.Sprint(fact.Payload["content_body"])
		if wantBody, ok := want[path]; ok {
			if body != wantBody {
				t.Fatalf("hostile alias content %q = %q, want %q", path, body, wantBody)
			}
			delete(want, path)
		}
	}
	if len(want) != 0 {
		t.Fatalf("Odù is missing hostile self/prefix alias facts: %v", want)
	}
}

func assertRepoDependencySourceFacts(t *testing.T, odu Odu) {
	t.Helper()

	coordinates := make(map[string]string, testRepoDependencySourceCount)
	kindsBySource := make(map[string][]string, testRepoDependencySourceCount)
	for _, fact := range odu.Facts {
		repoID, _ := fact.Payload["repo_id"].(string)
		if !strings.HasPrefix(repoID, "repository:source-") {
			continue
		}
		if strings.TrimSpace(fact.ScopeID) == "" || strings.TrimSpace(fact.GenerationID) == "" {
			t.Fatalf("source fact %q has blank scope or generation: %+v", repoID, fact)
		}
		coordinate := fact.ScopeID + "\x00" + fact.GenerationID
		if existing, ok := coordinates[repoID]; ok && existing != coordinate {
			t.Fatalf("source %q spans coordinates %q and %q", repoID, existing, coordinate)
		}
		coordinates[repoID] = coordinate
		kindsBySource[repoID] = append(kindsBySource[repoID], fact.FactKind)
	}

	if got, want := len(coordinates), testRepoDependencySourceCount; got != want {
		t.Fatalf("source coordinates = %d, want %d: %#v", got, want, coordinates)
	}
	uniqueCoordinates := make(map[string]struct{}, len(coordinates))
	for source, coordinate := range coordinates {
		if _, duplicate := uniqueCoordinates[coordinate]; duplicate {
			t.Fatalf("source %q reuses coordinate %q", source, coordinate)
		}
		uniqueCoordinates[coordinate] = struct{}{}

		kinds := kindsBySource[source]
		sort.Strings(kinds)
		wantKinds := []string{contentFactKind, repositoryFactKind, "shared_followup"}
		if source == "repository:source-01" || source == "repository:source-07" {
			wantKinds = []string{contentFactKind, contentFactKind, repositoryFactKind, "shared_followup"}
		}
		if !reflect.DeepEqual(kinds, wantKinds) {
			t.Fatalf("source %q fact kinds = %v, want %v", source, kinds, wantKinds)
		}
	}
}

func assertRepoDependencyEvidence(t *testing.T, evidence []relationships.EvidenceFact) {
	t.Helper()

	if got, want := len(evidence), testRepoDependencySourceCount; got != want {
		t.Fatalf("len(DiscoveredEvidence) = %d, want %d: %+v", got, want, evidence)
	}

	wantEdges := map[string]string{
		"repository:source-01": "repository:target-hub",
		"repository:source-02": "repository:target-hub",
		"repository:source-03": "repository:target-hub",
		"repository:source-04": "repository:target-hub",
		"repository:source-05": "repository:source-06",
		"repository:source-06": "repository:source-05",
		"repository:source-07": "repository:target-07",
		"repository:source-08": "repository:target-08",
	}
	gotEdges := make(map[string]string, len(evidence))
	targetFanIn := make(map[string]int)
	for _, item := range evidence {
		if item.EvidenceKind != relationships.EvidenceKindTerraformAppRepo {
			t.Errorf("evidence kind = %q, want %q", item.EvidenceKind, relationships.EvidenceKindTerraformAppRepo)
		}
		if item.RelationshipType != relationships.RelProvisionsDependencyFor {
			t.Errorf("relationship type = %q, want %q", item.RelationshipType, relationships.RelProvisionsDependencyFor)
		}
		if item.SourceRepoID == item.TargetRepoID {
			t.Errorf("self relationship escaped fixture: %s -> %s", item.SourceRepoID, item.TargetRepoID)
		}
		if got := strings.TrimSpace(fmt.Sprint(item.Details["path"])); got != testRepoDependencyContentPath {
			t.Errorf("evidence path = %q, want %q", got, testRepoDependencyContentPath)
		}
		if _, duplicate := gotEdges[item.SourceRepoID]; duplicate {
			t.Errorf("source %q emitted duplicate evidence", item.SourceRepoID)
		}
		gotEdges[item.SourceRepoID] = item.TargetRepoID
		targetFanIn[item.TargetRepoID]++
	}

	if !reflect.DeepEqual(gotEdges, wantEdges) {
		t.Fatalf("evidence edges = %#v, want %#v", gotEdges, wantEdges)
	}
	if got, want := targetFanIn["repository:target-hub"], 4; got != want {
		t.Fatalf("target-hub fan-in = %d, want %d", got, want)
	}
}
