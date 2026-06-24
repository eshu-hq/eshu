// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azureruntime

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestFixturePageProviderFromFilesChainsBySkipToken(t *testing.T) {
	provider, err := NewFixturePageProviderFromFiles(
		azurecloud.ScopeAccess{},
		filepath.Join("testdata", "resources_page1.json"),
		filepath.Join("testdata", "resources_page2.json"),
	)
	if err != nil {
		t.Fatalf("from files: %v", err)
	}
	src := newFixtureSource(t, provider, testTarget())
	collected, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("Next ok=%v err=%v", ok, err)
	}
	envs := drain(t, collected)
	resources := factsOfKind(envs, facts.AzureCloudResourceFactKind)
	if len(resources) != 3 {
		t.Fatalf("file-backed provider emitted %d resources, want 3", len(resources))
	}
	if calls := provider.Calls(); len(calls) != 2 {
		t.Fatalf("file-backed provider visited %d pages, want 2: %v", len(calls), calls)
	}
}

func TestFixturePageProviderFromFilesRequiresPath(t *testing.T) {
	if _, err := NewFixturePageProviderFromFiles(azurecloud.ScopeAccess{}); err == nil {
		t.Fatal("expected error with no fixture paths")
	}
}

func TestFixturePageProviderMissingTokenIsError(t *testing.T) {
	provider := NewFixturePageProvider(map[string]azurecloud.ResourceGraphPage{
		"": {SkipToken: "missing", Rows: nil},
	}, azurecloud.ScopeAccess{})
	src := newFixtureSource(t, provider, testTarget())
	if _, _, err := src.Next(context.Background()); err == nil {
		t.Fatal("expected error for missing fixture page")
	}
}

func TestStaticFixtureFactoryRejectsNilProvider(t *testing.T) {
	_, err := StaticFixtureFactory(nil).PageProvider(context.Background(), azurecloud.Boundary{}, testTarget())
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}
