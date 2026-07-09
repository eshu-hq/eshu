// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestGitSourceEmitsRepoHostedBackstageCatalogFacts(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	const catalogPath = "catalog-info.yaml"
	catalogBody := strings.Join([]string{
		"apiVersion: backstage.io/v1alpha1",
		"kind: Component",
		"metadata:",
		"  name: example-service",
		"  title: Example Service",
		"  annotations:",
		"    backstage.io/source-location: url:https://github.com/example/service/tree/main",
		"spec:",
		"  type: service",
		"  lifecycle: production",
		"  owner: group:default/platform",
		"",
	}, "\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, catalogPath), catalogBody)

	repo := testCollectorRepositoryMetadata(repoRoot)
	observedAt := time.Date(2026, time.June, 5, 10, 15, 0, 0, time.UTC)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		FileData: []map[string]any{{
			"lang": "yaml",
			"path": filepath.Join(repoRoot, catalogPath),
		}},
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: catalogPath,
			Digest:       "sha256:catalog",
			Language:     "yaml",
		}},
	}

	collected := buildStreamingGeneration(repoRoot, repo, "run-1", observedAt, snapshot, false)
	envelopes := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount(), len(envelopes); got != want {
		t.Fatalf("FactCount = %d, want emitted fact count %d", got, want)
	}

	if contentFacts := factsByKind(envelopes, "content"); len(contentFacts) != 1 {
		t.Fatalf("content fact count = %d, want 1", len(contentFacts))
	}

	entityFacts := factsByKind(envelopes, facts.ServiceCatalogEntityFactKind)
	ownershipFacts := factsByKind(envelopes, facts.ServiceCatalogOwnershipFactKind)
	repositoryLinkFacts := factsByKind(envelopes, facts.ServiceCatalogRepositoryLinkFactKind)
	if got, want := len(entityFacts), 1; got != want {
		t.Fatalf("service catalog entity fact count = %d, want %d", got, want)
	}
	if got, want := len(ownershipFacts), 1; got != want {
		t.Fatalf("service catalog ownership fact count = %d, want %d", got, want)
	}
	if got, want := len(repositoryLinkFacts), 1; got != want {
		t.Fatalf("service catalog repository_link fact count = %d, want %d", got, want)
	}

	entity := entityFacts[0]
	if got, want := entity.CollectorKind, servicecatalog.CollectorKind; got != want {
		t.Fatalf("entity CollectorKind = %q, want %q", got, want)
	}
	if got, want := entity.SourceRef.SourceSystem, servicecatalog.CollectorKind; got != want {
		t.Fatalf("entity SourceRef.SourceSystem = %q, want %q", got, want)
	}
	if got, want := entity.SourceRef.SourceURI, catalogPath; got != want {
		t.Fatalf("entity SourceRef.SourceURI = %q, want %q", got, want)
	}
	if strings.Contains(entity.SourceRef.SourceURI, repoRoot) {
		t.Fatalf("entity SourceRef.SourceURI = %q, want no absolute repo path", entity.SourceRef.SourceURI)
	}
	if got, want := entity.SchemaVersion, facts.ServiceCatalogSchemaVersionV1; got != want {
		t.Fatalf("entity SchemaVersion = %q, want %q", got, want)
	}
	if got, want := entity.Payload["entity_ref"], "component:default/example-service"; got != want {
		t.Fatalf("entity_ref = %#v, want %#v", got, want)
	}
	for _, forbidden := range []string{"repo_id", "service_id", "workload_id"} {
		if _, ok := entity.Payload[forbidden]; ok {
			t.Fatalf("entity payload unexpectedly includes %q: %#v", forbidden, entity.Payload)
		}
	}

	repositoryLink := repositoryLinkFacts[0]
	if got, want := repositoryLink.Payload["repository_url"], "https://github.com/example/service"; got != want {
		t.Fatalf("repository_url = %#v, want %#v", got, want)
	}
	if _, ok := repositoryLink.Payload["repository_id"]; ok {
		t.Fatalf("repository link payload unexpectedly includes repository_id: %#v", repositoryLink.Payload)
	}
}

func TestGitSourceEmitsRepoHostedServiceCatalogFactsFromLegacyContentFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	const manifestPath = "opslevel.yml"
	manifestBody := strings.Join([]string{
		"version: 1",
		"component:",
		"  name: Example Service",
		"  type: service",
		"  lifecycle: generally_available",
		"  owner: team_platform",
		"  aliases:",
		"    - example-service",
		"  repositories:",
		"    - name: example/service",
		"      provider: github",
		"",
	}, "\n")

	repo := testCollectorRepositoryMetadata(repoRoot)
	observedAt := time.Date(2026, time.June, 5, 11, 15, 0, 0, time.UTC)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFiles: []ContentFileSnapshot{{
			RelativePath: manifestPath,
			Body:         manifestBody,
			Digest:       "sha256:opslevel",
			Language:     "yaml",
		}},
	}

	collected := buildStreamingGeneration(repoRoot, repo, "run-1", observedAt, snapshot, false)
	envelopes := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount(), len(envelopes); got != want {
		t.Fatalf("FactCount = %d, want emitted fact count %d", got, want)
	}
	if got, want := len(factsByKind(envelopes, facts.ServiceCatalogEntityFactKind)), 1; got != want {
		t.Fatalf("service catalog entity fact count = %d, want %d", got, want)
	}
	repositoryLinks := factsByKind(envelopes, facts.ServiceCatalogRepositoryLinkFactKind)
	if got, want := len(repositoryLinks), 1; got != want {
		t.Fatalf("service catalog repository_link fact count = %d, want %d", got, want)
	}
	if got, want := repositoryLinks[0].SourceRef.SourceURI, manifestPath; got != want {
		t.Fatalf("repository_link SourceRef.SourceURI = %q, want %q", got, want)
	}
}

func TestServiceCatalogProviderForPathIsNarrow(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		relativePath string
		wantProvider servicecatalog.Provider
		wantOK       bool
	}{
		{
			name:         "backstage yaml",
			relativePath: "catalog-info.yaml",
			wantProvider: servicecatalog.ProviderBackstage,
			wantOK:       true,
		},
		{
			name:         "backstage yml nested",
			relativePath: "services/api/catalog-info.yml",
			wantProvider: servicecatalog.ProviderBackstage,
			wantOK:       true,
		},
		{
			name:         "opslevel",
			relativePath: "opslevel.yml",
			wantProvider: servicecatalog.ProviderOpsLevel,
			wantOK:       true,
		},
		{
			name:         "cortex",
			relativePath: "cortex.yaml",
			wantProvider: servicecatalog.ProviderCortex,
			wantOK:       true,
		},
		{
			name:         "ordinary yaml",
			relativePath: "deploy/service.yaml",
			wantOK:       false,
		},
		{
			name:         "scorecard stays carried future slice",
			relativePath: "cortex_scorecard.yaml",
			wantOK:       false,
		},
		{
			name:         "absolute backstage path rejected",
			relativePath: "/catalog-info.yaml",
			wantOK:       false,
		},
		{
			name:         "parent traversal backstage path rejected",
			relativePath: "../catalog-info.yaml",
			wantOK:       false,
		},
		{
			name:         "cleaned parent traversal rejected",
			relativePath: "services/../../catalog-info.yaml",
			wantOK:       false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotProvider, gotOK := serviceCatalogProviderForPath(testCase.relativePath)
			if gotOK != testCase.wantOK {
				t.Fatalf("ok = %t, want %t", gotOK, testCase.wantOK)
			}
			if gotProvider != testCase.wantProvider {
				t.Fatalf("provider = %q, want %q", gotProvider, testCase.wantProvider)
			}
		})
	}
}
