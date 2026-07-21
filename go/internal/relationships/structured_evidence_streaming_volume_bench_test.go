// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// parseFixtureForBench is parseFixtureForTest
// (structured_evidence_real_parser_test.go), duplicated against the
// testing.TB interface instead of *testing.T so
// buildRepresentativeStreamingRepoBatch can build the SAME real-parser
// fixture for both the *testing.T volume proof and the *testing.B cost
// benchmark below.
func parseFixtureForBench(t testing.TB, filename, source string) map[string]any {
	t.Helper()
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, filename)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir for fixture %s: %v", filePath, err)
	}
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

// This file is the runtime-affecting-change proof issue #5445 finding 1
// requires: the migration to factschema.DecodeParsedFileData* accessors is
// not output-preserving, it makes the streaming per-commit ingestion path
// (go/internal/storage/postgres/ingestion.go's upsertStreamingFacts callback
// -> relationships.DiscoverEvidenceWithStats -> RelationshipStore.
// UpsertEvidenceFacts) do real catalog matching and evidence-fact writes for
// terraform_modules, terragrunt_dependencies, terragrunt_configs,
// helm_charts, helm_values, argocd_applications, argocd_applicationsets, and
// flux_git_repositories that were previously always skipped (OLD CODE
// evidence count on this shape = 0, proven directly below and previously at
// merge-base with the pre-#5445 raw `.([]any)` read). Both halves of the
// evidence table this file measures:
//
//  1. streaming-time evidence VOLUME: buildRepresentativeStreamingRepoBatch
//     assembles a batch of REAL-PARSER-emitted file facts (go/internal/parser's
//     DefaultEngine, via parseFixtureForTest) for a representative mid-size
//     platform/gitops repository -- 24 files spanning all 8 migrated buckets,
//     matched against a populated catalog so real evidence resolves, not just
//     discovery attempts.
//  2. streaming-time COST delta: BenchmarkStreamingEvidenceDiscovery_
//     RepresentativeRepo measures relationships.DiscoverEvidenceWithStats'
//     reducer-side (in-memory, no Postgres) compute cost on that same batch.
//     RelationshipStore.UpsertEvidenceFacts (relationship_store.go) persists
//     the result as bounded multi-row `INSERT ... ON CONFLICT DO NOTHING`
//     batches of up to evidenceInsertBatchRows=500 rows each (issue #3704 --
//     pre-existing infrastructure, not new to this change), so the Postgres
//     write-side marginal cost of this fix is bounded by
//     ceil(len(evidence)/500) additional batch statements per commit; this
//     package cannot exercise that Postgres path (relationships has no
//     storage/postgres dependency, by the ownership boundary in
//     docs/internal/agent-guide.md), so a live-Postgres wall-clock number for
//     UpsertEvidenceFacts itself is intentionally out of scope for this
//     in-memory proof and is called out as such in the PR evidence.
func buildRepresentativeStreamingRepoBatch(t testing.TB) ([]facts.Envelope, []CatalogEntry) {
	t.Helper()

	catalog := []CatalogEntry{
		{RepoID: "repo-vpc-module", Aliases: []string{"terraform-aws-modules/vpc/aws"}},
		{RepoID: "repo-eks-module", Aliases: []string{"terraform-aws-modules/eks/aws"}},
		{RepoID: "repo-terragrunt-core", Aliases: []string{"iac-eks-terragrunt-core"}},
		{RepoID: "repo-payments-service", Aliases: []string{"payments-service"}},
		{RepoID: "repo-redis-chart", Aliases: []string{"redis"}},
		{RepoID: "repo-postgres-chart", Aliases: []string{"postgresql"}},
		{RepoID: "repo-checkout-image", Aliases: []string{"checkout-service"}},
		{RepoID: "repo-billing-image", Aliases: []string{"billing-service"}},
		{RepoID: "repo-checkout-deploy", Aliases: []string{"checkout-deploy"}},
		{RepoID: "repo-billing-deploy", Aliases: []string{"billing-deploy"}},
		{RepoID: "repo-platform-config", Aliases: []string{"platform-config"}},
		{RepoID: "repo-platform-runtime", Aliases: []string{"platform-runtime"}},
		{RepoID: "repo-flux-checkout-config", RemoteURL: "https://github.com/example/checkout-config"},
		{RepoID: "repo-flux-billing-config", RemoteURL: "https://github.com/example/billing-config"},
		{RepoID: "repo-flux-platform-config", RemoteURL: "https://github.com/example/platform-config"},
	}

	var envelopes []facts.Envelope
	addFile := func(name, source string) {
		payload := parseFixtureForBench(t, name, source)
		envelopes = append(envelopes, facts.Envelope{
			ScopeID: "repo-platform",
			Payload: map[string]any{"relative_path": name, "parsed_file_data": payload},
		})
	}

	// 6 Terraform module files (terraform_modules).
	for i := 0; i < 3; i++ {
		addFile(fmt.Sprintf("modules/vpc-%d/main.tf", i), `module "vpc" {
  source = "terraform-aws-modules/vpc/aws"
}
`)
	}
	for i := 0; i < 3; i++ {
		addFile(fmt.Sprintf("modules/eks-%d/main.tf", i), `module "eks" {
  source = "terraform-aws-modules/eks/aws"
}
`)
	}

	// 3 Terragrunt files (terragrunt_dependencies + terraform_modules source
	// row) and 1 Terragrunt-config-only file (terragrunt_configs helper
	// paths).
	for i := 0; i < 3; i++ {
		addFile(fmt.Sprintf("live/env-%d/terragrunt.hcl", i), `terraform {
  source = "../payments-service/modules/app"
}

dependency "payments" {
  config_path = "../payments-service"
}
`)
	}
	addFile("live/global/terragrunt.hcl", `terraform {
  source = "../modules/app"
}

locals {
  core = read_terragrunt_config("../iac-eks-terragrunt-core/global.hcl")
}
`)

	// 3 Helm charts (helm_charts) + 3 Helm values files (helm_values).
	addFile("charts/checkout/Chart.yaml", `name: checkout-service
version: 1.2.3
dependencies:
  - name: redis
    repository: https://charts.example.test/redis
`)
	addFile("charts/billing/Chart.yaml", `name: billing-service
version: 1.0.0
dependencies:
  - name: postgresql
    repository: https://charts.example.test/postgresql
`)
	addFile("charts/platform/Chart.yaml", `name: platform-service
version: 2.0.0
`)
	addFile("charts/checkout/values.yaml", `image:
  repository: ghcr.io/example/checkout-service
`)
	addFile("charts/billing/values.yaml", `image:
  repository: ghcr.io/example/billing-service
`)
	addFile("charts/platform/values.yaml", `replicaCount: 3
`)

	// 3 ArgoCD Applications (argocd_applications) + 2 ApplicationSets
	// (argocd_applicationsets).
	addFile("apps/checkout/application.yaml", `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: checkout
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/example/checkout-deploy
    path: overlays/prod
  destination:
    server: https://kubernetes.default.svc
    namespace: checkout
`)
	addFile("apps/billing/application.yaml", `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: billing
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/example/billing-deploy
    path: overlays/prod
  destination:
    server: https://kubernetes.default.svc
    namespace: billing
`)
	addFile("apps/platform/application.yaml", `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: platform
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/example/platform-config
    path: base
  destination:
    server: https://kubernetes.default.svc
    namespace: platform
`)
	addFile("appsets/checkout-envs/applicationset.yaml", `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: checkout-envs
  namespace: argocd
spec:
  generators:
  - git:
      repoURL: https://github.com/example/platform-config
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
	addFile("appsets/billing-envs/applicationset.yaml", `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: billing-envs
  namespace: argocd
spec:
  generators:
  - git:
      repoURL: https://github.com/example/platform-runtime
      directories:
      - path: envs/*
  template:
    spec:
      project: default
      source:
        repoURL: https://github.com/example/billing-deploy
        path: overlays/prod
      destination:
        server: https://kubernetes.default.svc
        namespace: billing
`)

	// 3 Flux GitRepositories (flux_git_repositories).
	addFile("flux/checkout/gitrepository.yaml", `apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: checkout-config
  namespace: flux-system
spec:
  url: https://github.com/example/checkout-config
`)
	addFile("flux/billing/gitrepository.yaml", `apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: billing-config
  namespace: flux-system
spec:
  url: https://github.com/example/billing-config
`)
	addFile("flux/platform/gitrepository.yaml", `apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: platform-config
  namespace: flux-system
spec:
  url: https://github.com/example/platform-config
`)

	return envelopes, catalog
}

// TestStreamingEvidenceVolume_RepresentativeRepo is the before/after evidence
// table for issue #5445 finding 1. It directly reproduces the OLD-code
// zero-evidence result on THIS representative repo's real-parser streaming
// payload (not just citing the prior merge-base proof), then measures the
// NEW code's actual evidence volume on the identical batch.
func TestStreamingEvidenceVolume_RepresentativeRepo(t *testing.T) {
	t.Parallel()

	envelopes, catalog := buildRepresentativeStreamingRepoBatch(t)

	// OLD CODE reproduction: the pre-#5445 read sites all did
	// parsedFileData[key].([]any) directly against the streaming payload.
	// AppendBucket (go/internal/parser/shared/shared.go) always builds
	// []map[string]any, so that assertion fails (ok=false) for every one of
	// the 8 migrated buckets on a real-parser streaming payload -- this is
	// the "OLD CODE evidence count (real-parser payload, streaming shape) =
	// 0" claim, reproduced fresh here rather than only cited from the
	// original finding.
	oldShapeBucketsSeen, oldShapeAnyAsserted := 0, 0
	for _, envelope := range envelopes {
		parsedFileData, _ := envelope.Payload["parsed_file_data"].(map[string]any)
		for _, key := range []string{
			"terraform_modules", "terragrunt_dependencies", "terragrunt_configs",
			"helm_charts", "helm_values", "argocd_applications",
			"argocd_applicationsets", "flux_git_repositories",
		} {
			raw, present := parsedFileData[key]
			if !present {
				continue
			}
			oldShapeBucketsSeen++
			if _, ok := raw.([]any); ok {
				oldShapeAnyAsserted++
			}
		}
	}
	if oldShapeBucketsSeen == 0 {
		t.Fatal("fixture produced no populated buckets across the 8 migrated keys; fixture is not representative")
	}
	if oldShapeAnyAsserted != 0 {
		t.Fatalf(
			"OLD-shape `.([]any)` assertion matched %d/%d real-parser-populated buckets, want 0 "+
				"(this would mean the streaming payload shape changed and the finding-1 accuracy delta no longer applies)",
			oldShapeAnyAsserted, oldShapeBucketsSeen,
		)
	}

	// NEW CODE: the actual streaming call, same batch shape ingestion.go
	// passes to DiscoverEvidenceWithStats.
	evidence, _ := DiscoverEvidenceWithStats(envelopes, catalog)
	if len(evidence) == 0 {
		t.Fatal("NEW code discovered 0 evidence facts on a fixture designed to match the catalog; fixture or catalog is broken")
	}

	t.Logf(
		"streaming evidence volume on a %d-file representative repo: OLD (pre-#5445, real-parser streaming shape) = 0, NEW = %d evidence facts across %d populated parsed_file_data buckets",
		len(envelopes), len(evidence), oldShapeBucketsSeen,
	)
}

// BenchmarkStreamingEvidenceDiscovery_RepresentativeRepo measures the
// reducer-side (in-memory) cost of relationships.DiscoverEvidenceWithStats on
// the same representative-repo batch TestStreamingEvidenceVolume_
// RepresentativeRepo proves now produces real evidence. This is the
// streaming-commit compute delta the fix adds; the corresponding Postgres
// UpsertEvidenceFacts write cost is out of scope for this package (see the
// file doc comment) and is bounded by the pre-existing
// evidenceInsertBatchRows=500-row batched-insert design
// (go/internal/storage/postgres/relationship_store.go).
func BenchmarkStreamingEvidenceDiscovery_RepresentativeRepo(b *testing.B) {
	envelopes, catalog := buildRepresentativeStreamingRepoBatch(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evidence, _ := DiscoverEvidenceWithStats(envelopes, catalog)
		if len(evidence) == 0 {
			b.Fatal("NEW code discovered 0 evidence facts; benchmark fixture regressed")
		}
	}
}
