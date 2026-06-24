// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestEngineDispatchesRegisteredLanguageProvider(t *testing.T) {
	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "main.toy")
	writeTestFile(t, sourcePath, "call ToySymbol\n")

	provider := &fixtureLanguageProvider{
		parsePayload: map[string]any{
			"path":          sourcePath,
			"lang":          "toy",
			"is_dependency": false,
			"functions": []map[string]any{
				{"name": "ToySymbol"},
			},
		},
		preScanNames: []string{"ToySymbol"},
		capabilities: LanguageCapabilities{
			PreScan: true,
		},
	}
	registry, err := NewRegistry([]Definition{
		{
			ParserKey:  "toy",
			Language:   "toy",
			Extensions: []string{".toy"},
			Provider:   provider,
		},
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v, want nil", err)
	}
	engine, err := NewEngine(registry, NewRuntime())
	if err != nil {
		t.Fatalf("NewEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, sourcePath, false, Options{VariableScope: "all"})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	if payload["lang"] != "toy" {
		t.Fatalf("lang = %#v, want toy", payload["lang"])
	}
	assertNamedBucketContains(t, payload, "functions", "ToySymbol")
	if provider.parseRequest.Path != sourcePath {
		t.Fatalf("provider parse path = %q, want %q", provider.parseRequest.Path, sourcePath)
	}
	if provider.parseRequest.RepoRoot != repoRoot {
		t.Fatalf("provider parse repo root = %q, want %q", provider.parseRequest.RepoRoot, repoRoot)
	}
	if provider.parseRequest.Options.VariableScope != "all" {
		t.Fatalf("provider parse variable scope = %q, want all", provider.parseRequest.Options.VariableScope)
	}

	preScan, err := engine.PreScanRepositoryPaths(repoRoot, []string{sourcePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	assertPrescanContains(t, preScan, "ToySymbol", sourcePath)
	if provider.preScanRequest.Path != sourcePath {
		t.Fatalf("provider prescan path = %q, want %q", provider.preScanRequest.Path, sourcePath)
	}

	definition, ok := registry.LookupByExtension(".toy")
	if !ok {
		t.Fatal("LookupByExtension(.toy) returned false")
	}
	capabilities := definition.Provider.Capabilities()
	if !capabilities.PreScan {
		t.Fatalf("provider capabilities = %#v, want prescan", capabilities)
	}
}

func TestEngineSkipsProviderPreScanWithoutCapability(t *testing.T) {
	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "main.toy")
	writeTestFile(t, sourcePath, "call ToySymbol\n")

	provider := &fixtureLanguageProvider{
		parsePayload: map[string]any{
			"path": sourcePath,
			"lang": "toy",
		},
		preScanNames: []string{"ToySymbol"},
	}
	registry, err := NewRegistry([]Definition{
		{
			ParserKey:  "toy",
			Language:   "toy",
			Extensions: []string{".toy"},
			Provider:   provider,
		},
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v, want nil", err)
	}
	engine, err := NewEngine(registry, NewRuntime())
	if err != nil {
		t.Fatalf("NewEngine() error = %v, want nil", err)
	}

	preScan, err := engine.PreScanRepositoryPaths(repoRoot, []string{sourcePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	if len(preScan) != 0 {
		t.Fatalf("PreScanRepositoryPaths() = %#v, want empty map", preScan)
	}
	if provider.preScanRequest.Path != "" {
		t.Fatalf("provider prescan path = %q, want no provider prescan call", provider.preScanRequest.Path)
	}
}

type fixtureLanguageProvider struct {
	parsePayload   map[string]any
	parseRequest   ParseRequest
	preScanNames   []string
	preScanRequest PreScanRequest
	capabilities   LanguageCapabilities
}

func (p *fixtureLanguageProvider) Parse(request ParseRequest) (map[string]any, error) {
	p.parseRequest = request
	return p.parsePayload, nil
}

func (p *fixtureLanguageProvider) PreScan(request PreScanRequest) ([]string, error) {
	p.preScanRequest = request
	return p.preScanNames, nil
}

func (p *fixtureLanguageProvider) Capabilities() LanguageCapabilities {
	return p.capabilities
}
