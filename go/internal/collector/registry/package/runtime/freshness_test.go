// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/registry/package"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// TestIdenticalDocumentsShareFreshnessHintAndSkip proves two collections over
// identical registry metadata share one non-empty freshness hint, so the
// store can skip the second generation instead of minting per tick (#8003).
// The hint covers the emitted envelopes' keyed payloads, not the raw
// document body, which can carry out-of-window content.
func TestIdenticalDocumentsShareFreshnessHintAndSkip(t *testing.T) {
	t.Parallel()

	document := []byte(`{
		"provider":"jfrog",
		"repository":"generic-local",
		"repository_type":"local",
		"name":"team-api",
		"namespace":"payments",
		"version":"1.2.3",
		"visibility":"private",
		"source_url":"https://github.com/eshu-hq/team-api",
		"artifacts":[{"key":"team-api-1.2.3.tgz","type":"tgz","sha256":"abc123"}]
	}`)
	first := collectPackageTick(t, document, "package_registry:generation-1")
	second := collectPackageTick(t, document, "package_registry:generation-2")

	if strings.TrimSpace(first.Generation.FreshnessHint) == "" {
		t.Fatal("first FreshnessHint is blank, want stable envelope digest")
	}
	if got, want := second.Generation.FreshnessHint, first.Generation.FreshnessHint; got != want {
		t.Fatalf("FreshnessHint changed for unchanged metadata: got %q, want %q", got, want)
	}
	candidate := backfill.UnchangedGenerationCandidate{
		GenerationID:  "package_registry:generation-1",
		FreshnessHint: first.Generation.FreshnessHint,
		Status:        scope.GenerationStatusActive,
	}
	action, _ := backfill.DecideUnchangedGeneration(second.Generation, candidate, true)
	if action != backfill.UnchangedActionSkip {
		t.Fatalf("DecideUnchangedGeneration() = %d, want skip (%d)", action, backfill.UnchangedActionSkip)
	}
}

// TestYankedVersionStoresNewGeneration proves a yank-flag flip mints a new
// generation even though the version identity (and therefore every stable
// key) is unchanged (#8003 F2). A key-only hint would skip over yanked,
// deprecated, retracted, checksum, URL, published_at, visibility, and
// warning-text drift that keeps the same keys.
func TestYankedVersionStoresNewGeneration(t *testing.T) {
	t.Parallel()

	before := []byte(`{
		"info": {"name": "team-api", "version": "1.2.3"},
		"releases": {
			"1.2.3": [{
				"packagetype": "bdist_wheel",
				"url": "https://files.pythonhosted.org/packages/team-api-1.2.3-py3-none-any.whl",
				"filename": "team-api-1.2.3-py3-none-any.whl",
				"size": 1234,
				"upload_time_iso_8601": "2026-05-11T09:00:00Z",
				"yanked": false,
				"digests": {"sha256": "abc123"}
			}]
		}
	}`)
	after := []byte(`{
		"info": {"name": "team-api", "version": "1.2.3"},
		"releases": {
			"1.2.3": [{
				"packagetype": "bdist_wheel",
				"url": "https://files.pythonhosted.org/packages/team-api-1.2.3-py3-none-any.whl",
				"filename": "team-api-1.2.3-py3-none-any.whl",
				"size": 1234,
				"upload_time_iso_8601": "2026-05-11T09:00:00Z",
				"yanked": true,
				"digests": {"sha256": "abc123"}
			}]
		}
	}`)
	first := collectPyPIPackageTick(t, before, "package_registry:pypi-generation-1")
	second := collectPyPIPackageTick(t, after, "package_registry:pypi-generation-2")

	if first.Generation.FreshnessHint == second.Generation.FreshnessHint {
		t.Fatalf("FreshnessHint did not change after yank flag flip: %q", first.Generation.FreshnessHint)
	}
	candidate := backfill.UnchangedGenerationCandidate{
		GenerationID:  "package_registry:pypi-generation-1",
		FreshnessHint: first.Generation.FreshnessHint,
		Status:        scope.GenerationStatusActive,
	}
	action, _ := backfill.DecideUnchangedGeneration(second.Generation, candidate, true)
	if action != backfill.UnchangedActionStore {
		t.Fatalf("DecideUnchangedGeneration() = %d, want store (%d)", action, backfill.UnchangedActionStore)
	}
}

// TestChangedDocumentStoresNewGeneration proves a metadata change mints a new
// generation instead of skipping over real drift (#8003).
func TestChangedDocumentStoresNewGeneration(t *testing.T) {
	t.Parallel()

	before := []byte(`{"name":"team-api","version":"1.2.3"}`)
	after := []byte(`{"name":"team-api","version":"1.2.4"}`)
	first := collectPackageTick(t, before, "package_registry:generation-1")
	second := collectPackageTick(t, after, "package_registry:generation-2")

	if first.Generation.FreshnessHint == second.Generation.FreshnessHint {
		t.Fatalf("FreshnessHint did not change after metadata update: %q", first.Generation.FreshnessHint)
	}
	candidate := backfill.UnchangedGenerationCandidate{
		GenerationID:  "package_registry:generation-1",
		FreshnessHint: first.Generation.FreshnessHint,
		Status:        scope.GenerationStatusActive,
	}
	action, _ := backfill.DecideUnchangedGeneration(second.Generation, candidate, true)
	if action != backfill.UnchangedActionStore {
		t.Fatalf("DecideUnchangedGeneration() = %d, want store (%d)", action, backfill.UnchangedActionStore)
	}
}

func collectPackageTick(t *testing.T, document []byte, generationID string) collectedPackageTick {
	t.Helper()

	source := newTestClaimedSource(t, TargetConfig{
		Base: packages.TargetConfig{
			Provider:     "jfrog",
			Ecosystem:    packages.EcosystemGeneric,
			Registry:     "https://artifactory.example.com",
			ScopeID:      "package-registry://jfrog/generic/team-api",
			Packages:     []string{"team-api"},
			PackageLimit: 1,
			VersionLimit: 2,
		},
	}, staticMetadataProvider{document: document})

	item := testPackageRegistryWorkItem()
	item.GenerationID = generationID
	item.SourceRunID = generationID
	item.WorkItemID = "package-registry-work-item-" + generationID
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	for range collected.Facts {
	}
	return collectedPackageTick{Generation: collected.Generation}
}

func collectPyPIPackageTick(t *testing.T, document []byte, generationID string) collectedPackageTick {
	t.Helper()

	const pypiScopeID = "package-registry://pypi/simple/team-api"
	source := newTestClaimedSource(t, TargetConfig{
		Base: packages.TargetConfig{
			Provider:     "pypi",
			Ecosystem:    packages.EcosystemPyPI,
			Registry:     "https://pypi.org/pypi",
			ScopeID:      pypiScopeID,
			Packages:     []string{"team-api"},
			PackageLimit: 1,
			VersionLimit: 2,
		},
	}, staticMetadataProvider{document: document})

	item := testPackageRegistryWorkItemForScope(pypiScopeID)
	item.GenerationID = generationID
	item.SourceRunID = generationID
	item.WorkItemID = "package-registry-work-item-" + generationID
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	for range collected.Facts {
	}
	return collectedPackageTick{Generation: collected.Generation}
}

type collectedPackageTick struct {
	Generation scope.ScopeGeneration
}
