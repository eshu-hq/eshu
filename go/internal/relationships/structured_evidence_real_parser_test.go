// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// These tests build parsed_file_data by running the REAL HCL/YAML parsers
// (go/internal/parser/hcl, go/internal/parser/yaml, via the parser package's
// DefaultEngine) against a small representative fixture file, then feed that
// real payload into DiscoverEvidence exactly as the ingestion pipeline does.
// This is the equivalence proof issue #5445 slice 1 requires for the eight
// parsed_file_data inner keys this change routes through typed
// factschema.DecodeParsedFileData* accessors instead of a raw map lookup
// (terraform_evidence.go, terragrunt_helper_evidence.go,
// structured_family_evidence.go, argocd_generator_config.go,
// flux_evidence.go): unlike a hand-built map[string]any fixture, this proves
// the accessor decodes the SHAPE THE PARSER ACTUALLY EMITS, not a shape
// someone imagined. Each test only sets parsed_file_data (no "content"), so
// the content-regex extractors (discoverTerraformEvidence,
// discoverHelmEvidence, ...) never fire and the structured extractor under
// test is isolated, mirroring evidence_structured_hcl_test.go's convention.

func parseFixtureForTest(t *testing.T, filename, source string) map[string]any {
	t.Helper()
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, filename)
	if err := os.WriteFile(filePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", filePath, err)
	}
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v", err)
	}
	payload, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v", filePath, err)
	}
	return payload
}

// TestRealParserStructuredTerraformModuleEvidence proves
// discoverStructuredTerraformEvidence, migrated to
// factschema.DecodeParsedFileDataTerraformModules, resolves a module source
// from a REAL-PARSER-emitted terraform_modules row (a "module" HCL block in
// a plain .tf file).
func TestRealParserStructuredTerraformModuleEvidence(t *testing.T) {
	t.Parallel()

	payload := parseFixtureForTest(t, "main.tf", `module "service" {
  source = "git::https://github.com/myorg/payments-service.git//modules/service?ref=v1.2.3"
}
`)

	envelope := facts.Envelope{
		ScopeID: "repo-infra",
		Payload: map[string]any{
			"relative_path":    "main.tf",
			"parsed_file_data": payload,
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"myorg/payments-service", "payments-service"}},
	}

	evidence := DiscoverEvidence([]facts.Envelope{envelope}, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len(evidence) = %d, want 1: %#v", len(evidence), evidence)
	}
	if evidence[0].EvidenceKind != EvidenceKindTerraformModuleSource {
		t.Fatalf("EvidenceKind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindTerraformModuleSource)
	}
	if evidence[0].TargetRepoID != "repo-payments" {
		t.Fatalf("TargetRepoID = %q, want repo-payments", evidence[0].TargetRepoID)
	}
	if evidence[0].Details["module_name"] != "service" {
		t.Fatalf("Details[module_name] = %#v, want %q", evidence[0].Details["module_name"], "service")
	}
}

// TestRealParserStructuredTerragruntDependencyAndModuleSourceEvidence proves
// discoverStructuredTerraformEvidence resolves BOTH a Terragrunt terraform
// -source and a dependency config_path from a REAL-PARSER-emitted
// terragrunt.hcl payload (parseTerragruntModuleSources +
// parseTerragruntDependencies).
func TestRealParserStructuredTerragruntDependencyAndModuleSourceEvidence(t *testing.T) {
	t.Parallel()

	payload := parseFixtureForTest(t, "terragrunt.hcl", `terraform {
  source = "../payments-service/modules/app"
}

dependency "payments" {
  config_path = "../payments-service"
}
`)

	envelope := facts.Envelope{
		ScopeID: "repo-gitops",
		Payload: map[string]any{
			"relative_path":    "terragrunt.hcl",
			"parsed_file_data": payload,
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence([]facts.Envelope{envelope}, catalog)
	var sawModuleSource, sawDependencyConfigPath bool
	for _, item := range evidence {
		if item.TargetRepoID != "repo-payments" {
			t.Fatalf("TargetRepoID = %q, want repo-payments (evidence %#v)", item.TargetRepoID, item)
		}
		switch item.EvidenceKind {
		case EvidenceKindTerraformModuleSource:
			sawModuleSource = true
			if item.RelationshipType != RelUsesModule {
				t.Fatalf("RelationshipType = %q, want %q", item.RelationshipType, RelUsesModule)
			}
		case EvidenceKindTerragruntDependencyConfigPath:
			sawDependencyConfigPath = true
			if item.RelationshipType != RelDiscoversConfigIn {
				t.Fatalf("RelationshipType = %q, want %q", item.RelationshipType, RelDiscoversConfigIn)
			}
		}
	}
	if !sawModuleSource || !sawDependencyConfigPath {
		t.Fatalf("evidence = %#v, want one EvidenceKindTerraformModuleSource and one EvidenceKindTerragruntDependencyConfigPath", evidence)
	}
}

// TestRealParserStructuredTerragruntConfigHelperEvidence proves
// discoverStructuredTerragruntConfigEvidence, migrated to
// factschema.DecodeParsedFileDataTerragruntConfigs, resolves three
// helper-path kinds from a REAL-PARSER-emitted terragrunt_configs row
// (parseTerragruntConfig + parseTerragruntHelperPaths).
//
// The fixture deliberately omits an `include` block: the terragruntIncludePathPattern
// regex (go/internal/parser/hcl/helpers.go) only matches `path =
// find_in_parent_folders("...")`, so ANY real include block populated this
// way also always matches the separate, broader
// terragruntFindInParentFoldersPattern -- the two buckets share the SAME
// value for that path. matchCatalog's seen-map dedupes by (kind, source,
// target, path, matched_value) without a helper_kind component, so the
// include_path and find_in_parent_folders_path evidence for that shared
// value collapse into one fact. That collision is a pre-existing
// matchCatalog dedup characteristic, unrelated to this migration (it
// reproduces identically against the raw pre-typing map read) -- this test
// exercises read_config_path, find_in_parent_folders_path, and
// local_config_asset_path with non-overlapping real values instead of
// re-litigating that dedup design. include_path itself keeps its existing
// coverage in TestDiscoverStructuredTerragruntHelperConfigEvidence
// (evidence_structured_hcl_test.go, hand-built map).
func TestRealParserStructuredTerragruntConfigHelperEvidence(t *testing.T) {
	t.Parallel()

	payload := parseFixtureForTest(t, "terragrunt.hcl", `terraform {
  source = "../modules/app"
}

locals {
  vpc     = read_terragrunt_config("../iac-eks-terragrunt-core/vpc.hcl")
  parent  = find_in_parent_folders("../iac-eks-terragrunt-core/global.yaml")
  runtime = yamldecode(file("../terraform-modules-aws/config/runtime.yaml"))
}
`)

	envelope := facts.Envelope{
		ScopeID: "repo-live",
		Payload: map[string]any{
			"relative_path":    "env/prod/terragrunt.hcl",
			"parsed_file_data": payload,
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-terragrunt-core", Aliases: []string{"iac-eks-terragrunt-core"}},
		{RepoID: "repo-terraform-modules-aws", Aliases: []string{"terraform-modules-aws"}},
	}

	evidence := DiscoverEvidence([]facts.Envelope{envelope}, catalog)
	want := map[string]string{
		"read_config_path":            "repo-terragrunt-core",
		"find_in_parent_folders_path": "repo-terragrunt-core",
		"local_config_asset_path":     "repo-terraform-modules-aws",
	}
	got := map[string]string{}
	for _, item := range evidence {
		if item.EvidenceKind != EvidenceKindTerragruntConfigAssetPath {
			continue
		}
		if item.RelationshipType != RelDiscoversConfigIn {
			t.Fatalf("RelationshipType = %q, want %q", item.RelationshipType, RelDiscoversConfigIn)
		}
		helperKind, _ := item.Details["helper_kind"].(string)
		got[helperKind] = item.TargetRepoID
	}
	for helperKind, wantRepo := range want {
		if got[helperKind] != wantRepo {
			t.Fatalf("helper_kind %q resolved to %q, want %q (all evidence: %#v)", helperKind, got[helperKind], wantRepo, evidence)
		}
	}
}

// TestRealParserStructuredHelmEvidence proves discoverStructuredHelmEvidence,
// migrated to factschema.DecodeParsedFileDataHelmCharts /
// DecodeParsedFileDataHelmValues, resolves both a chart dependency and a
// values image repository from REAL-PARSER-emitted Chart.yaml/values.yaml
// payloads.
func TestRealParserStructuredHelmEvidence(t *testing.T) {
	t.Parallel()

	chartPayload := parseFixtureForTest(t, "Chart.yaml", `name: checkout-service
version: 1.2.3
appVersion: "2.0.0"
dependencies:
  - name: redis
    repository: https://charts.example.test/redis
`)
	valuesPayload := parseFixtureForTest(t, "values.yaml", `image:
  repository: ghcr.io/example/checkout-service
  tag: v1.0.0
`)

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-checkout",
			Payload: map[string]any{"relative_path": "Chart.yaml", "parsed_file_data": chartPayload},
		},
		{
			ScopeID: "repo-checkout",
			Payload: map[string]any{"relative_path": "values.yaml", "parsed_file_data": valuesPayload},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-redis-chart", Aliases: []string{"redis"}},
		{RepoID: "repo-checkout-image", Aliases: []string{"checkout-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	var sawChartDependency, sawImageRepository bool
	for _, item := range evidence {
		switch item.EvidenceKind {
		case EvidenceKindHelmChart:
			sawChartDependency = true
			if item.TargetRepoID != "repo-redis-chart" {
				t.Fatalf("HelmChart TargetRepoID = %q, want repo-redis-chart", item.TargetRepoID)
			}
		case EvidenceKindHelmValues:
			sawImageRepository = true
			if item.TargetRepoID != "repo-checkout-image" {
				t.Fatalf("HelmValues TargetRepoID = %q, want repo-checkout-image", item.TargetRepoID)
			}
		}
	}
	if !sawChartDependency || !sawImageRepository {
		t.Fatalf("evidence = %#v, want one EvidenceKindHelmChart and one EvidenceKindHelmValues", evidence)
	}
}

// TestRealParserStructuredArgoCDEvidence proves discoverStructuredArgoCDEvidence
// and ResolveArgoCDGeneratorConfigRepos, both migrated to
// factschema.DecodeParsedFileDataArgoCDApplications /
// DecodeParsedFileDataArgoCDApplicationSets, resolve an Application source
// and an ApplicationSet's generator + template sources from
// REAL-PARSER-emitted argocd_applications/argocd_applicationsets payloads.
// This exercises the TWO independent argocd_applicationsets read sites
// together (structured_family_evidence.go and argocd_generator_config.go).
func TestRealParserStructuredArgoCDEvidence(t *testing.T) {
	t.Parallel()

	applicationPayload := parseFixtureForTest(t, "application.yaml", `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: checkout
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/example/checkout-deploy
    path: overlays/prod
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: checkout
`)
	appSetPayload := parseFixtureForTest(t, "applicationset.yaml", `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: checkout-envs
  namespace: argocd
spec:
  generators:
  - git:
      repoURL: https://github.com/example/checkout-config
      directories:
      - path: envs/*
  template:
    spec:
      project: default
      source:
        repoURL: https://github.com/example/checkout-deploy
        path: overlays/prod
      destination:
        server: https://kubernetes.default.svc
        namespace: checkout
`)

	catalog := []CatalogEntry{
		{RepoID: "repo-checkout-deploy", Aliases: []string{"checkout-deploy"}},
		{RepoID: "repo-checkout-config", Aliases: []string{"checkout-config"}},
	}

	applicationEnvelope := facts.Envelope{
		ScopeID: "repo-checkout-appdef",
		Payload: map[string]any{"relative_path": "application.yaml", "parsed_file_data": applicationPayload},
	}
	appSetEnvelope := facts.Envelope{
		ScopeID: "repo-checkout-appdef",
		Payload: map[string]any{"relative_path": "applicationset.yaml", "parsed_file_data": appSetPayload},
	}

	evidence := DiscoverEvidence([]facts.Envelope{applicationEnvelope, appSetEnvelope}, catalog)
	var sawAppSource, sawAppSetDeploySource bool
	for _, item := range evidence {
		switch item.EvidenceKind {
		case EvidenceKindArgoCDAppSource:
			sawAppSource = true
			if item.TargetRepoID != "repo-checkout-deploy" {
				t.Fatalf("ArgoCDAppSource TargetRepoID = %q, want repo-checkout-deploy", item.TargetRepoID)
			}
		case EvidenceKindArgoCDApplicationSetDeploySource:
			sawAppSetDeploySource = true
			// This EvidenceKind's SourceRepoID/TargetRepoID encode "the
			// deployed repository sources from the config repository":
			// SourceRepoID is the DEPLOYED repo, TargetRepoID is the CONFIG
			// repo (see appendDeploySourceEvidence) -- the inverse of the
			// naming intuition "target = where evidence points."
			if item.SourceRepoID != "repo-checkout-deploy" || item.TargetRepoID != "repo-checkout-config" {
				t.Fatalf("ArgoCDApplicationSetDeploySource Source/TargetRepoID = %q/%q, want repo-checkout-deploy/repo-checkout-config", item.SourceRepoID, item.TargetRepoID)
			}
		}
	}
	if !sawAppSource || !sawAppSetDeploySource {
		t.Fatalf("evidence = %#v, want one EvidenceKindArgoCDAppSource and one EvidenceKindArgoCDApplicationSetDeploySource", evidence)
	}

	// structuredApplicationSetGeneratorRepos (argocd_generator_config.go) is
	// a SECOND, independent read of argocd_applicationsets -- prove it
	// resolves the same generator config repo from the same real payload.
	refs := ResolveArgoCDGeneratorConfigRepos([]facts.Envelope{appSetEnvelope}, catalog)
	if len(refs) != 1 || refs[0].ConfigRepoID != "repo-checkout-config" {
		t.Fatalf("ResolveArgoCDGeneratorConfigRepos() = %#v, want one ref to repo-checkout-config", refs)
	}
}

// TestRealParserStructuredFluxEvidence proves discoverStructuredFluxEvidence,
// migrated to factschema.DecodeParsedFileDataFluxGitRepositories, resolves a
// cross-repo GitRepository source from a REAL-PARSER-emitted
// flux_git_repositories payload.
func TestRealParserStructuredFluxEvidence(t *testing.T) {
	t.Parallel()

	payload := parseFixtureForTest(t, "gitrepository.yaml", `apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: checkout-config
  namespace: flux-system
spec:
  url: https://github.com/example/checkout-config
  ref:
    branch: main
`)

	envelope := facts.Envelope{
		ScopeID: "repo-flux-control-plane",
		Payload: map[string]any{"relative_path": "gitrepository.yaml", "parsed_file_data": payload},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-checkout-config", RemoteURL: "https://github.com/example/checkout-config"},
	}

	evidence := DiscoverEvidence([]facts.Envelope{envelope}, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len(evidence) = %d, want 1: %#v", len(evidence), evidence)
	}
	if evidence[0].EvidenceKind != EvidenceKindFluxGitRepositorySource {
		t.Fatalf("EvidenceKind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindFluxGitRepositorySource)
	}
	if evidence[0].TargetRepoID != "repo-checkout-config" {
		t.Fatalf("TargetRepoID = %q, want repo-checkout-config", evidence[0].TargetRepoID)
	}
	if evidence[0].Details["flux_git_repository_name"] != "checkout-config" {
		t.Fatalf("Details[flux_git_repository_name] = %#v, want %q", evidence[0].Details["flux_git_repository_name"], "checkout-config")
	}
	if evidence[0].Details["flux_git_repository_namespace"] != "flux-system" {
		t.Fatalf("Details[flux_git_repository_namespace] = %#v, want %q", evidence[0].Details["flux_git_repository_namespace"], "flux-system")
	}
}
