// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import "testing"

// This file is the delta proof and regression lock for issue #5445 finding
// 1: the streaming per-commit ingestion path
// (go/internal/storage/postgres/ingestion.go's upsertStreamingFacts callback
// -> relationships.DiscoverEvidenceWithStats) feeds DiscoverEvidence a
// facts.Envelope built directly from
// go/internal/collector/git_fact_builder.go's fileFactEnvelope, which embeds
// the in-memory parser payload map verbatim -- NO JSON marshal/unmarshal
// round trip. That payload's parsed_file_data inner buckets are exactly what
// go/internal/parser/shared.AppendBucket produces: []map[string]any, never
// []any (the shape a Postgres JSONB decode yields, and the only shape the
// deferred BackfillAllRelationshipEvidence pass ever sees, since it reloads
// facts from Postgres).
//
// Before issue #5445 slice 1, every one of these 8 accessors' predecessor
// call sites did a raw `parsedFileData[key].([]any)` type assertion, which
// fails silently (ok=false, no panic, no log) against a real
// []map[string]any value. So the streaming path produced ZERO evidence for
// terraform_modules, terragrunt_dependencies, terragrunt_configs,
// helm_charts, helm_values, argocd_applications, argocd_applicationsets, and
// flux_git_repositories on every commit, for as long as those extractors
// existed -- confirmed at merge-base with the real HCL/YAML parsers and the
// pre-#5445 evidence code (OLD CODE evidence count, real-parser payload,
// streaming shape = 0).
//
// decode_parsed_file_data_terraform_test.go and
// decode_parsed_file_data_gitops_test.go exercise every accessor only
// against []any fixtures -- the shape the dead streaming path never emits.
// The subtests below exercise the other real shape directly, at the
// accessor level, with no parser dependency: comment out the
// `case []map[string]any:` branch in decodeParsedFileDataTolerantSlice
// (decode_parsed_file_data_tolerant.go) and every one of them fails, proving
// this test suite -- not just the real-parser integration test -- would have
// caught the years-old bug.
func TestDecodeParsedFileDataAccessors_AcceptNativeMapSliceShape(t *testing.T) {
	t.Parallel()

	t.Run("TerraformModules", func(t *testing.T) {
		t.Parallel()
		modules, err := DecodeParsedFileDataTerraformModules(map[string]any{
			"terraform_modules": []map[string]any{
				{"name": "vpc", "source": "terraform-aws-modules/vpc/aws"},
			},
		})
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(modules) != 1 || modules[0].Name != "vpc" || modules[0].Source != "terraform-aws-modules/vpc/aws" {
			t.Fatalf("modules = %#v, want one vpc/terraform-aws-modules/vpc/aws row", modules)
		}
	})

	t.Run("TerragruntDependencies", func(t *testing.T) {
		t.Parallel()
		deps, err := DecodeParsedFileDataTerragruntDependencies(map[string]any{
			"terragrunt_dependencies": []map[string]any{
				{"name": "vpc", "config_path": "../vpc"},
			},
		})
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(deps) != 1 || deps[0].Name != "vpc" || deps[0].ConfigPath != "../vpc" {
			t.Fatalf("deps = %#v, want one vpc/../vpc row", deps)
		}
	})

	t.Run("TerragruntConfigs", func(t *testing.T) {
		t.Parallel()
		configs, err := DecodeParsedFileDataTerragruntConfigs(map[string]any{
			"terragrunt_configs": []map[string]any{
				{"include_paths": "../../_envcommon/root.hcl"},
			},
		})
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(configs) != 1 || configs[0].IncludePaths != "../../_envcommon/root.hcl" {
			t.Fatalf("configs = %#v, want one include_paths row", configs)
		}
	})

	t.Run("HelmCharts", func(t *testing.T) {
		t.Parallel()
		charts, err := DecodeParsedFileDataHelmCharts(map[string]any{
			"helm_charts": []map[string]any{
				{"name": "myapp", "dependency_repositories": "https://charts.example.com"},
			},
		})
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(charts) != 1 || charts[0].Name != "myapp" || charts[0].DependencyRepositories != "https://charts.example.com" {
			t.Fatalf("charts = %#v, want one myapp row", charts)
		}
	})

	t.Run("HelmValues", func(t *testing.T) {
		t.Parallel()
		values, err := DecodeParsedFileDataHelmValues(map[string]any{
			"helm_values": []map[string]any{
				{"name": "values", "image_repositories": "example.com/myapp"},
			},
		})
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(values) != 1 || values[0].Name != "values" || values[0].ImageRepositories != "example.com/myapp" {
			t.Fatalf("values = %#v, want one values row", values)
		}
	})

	t.Run("ArgoCDApplications", func(t *testing.T) {
		t.Parallel()
		applications, err := DecodeParsedFileDataArgoCDApplications(map[string]any{
			"argocd_applications": []map[string]any{
				{"name": "myapp", "source_repos": "https://github.com/myorg/config-repo.git"},
			},
		})
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(applications) != 1 || applications[0].Name != "myapp" ||
			applications[0].SourceRepos != "https://github.com/myorg/config-repo.git" {
			t.Fatalf("applications = %#v, want one myapp row", applications)
		}
	})

	t.Run("ArgoCDApplicationSets", func(t *testing.T) {
		t.Parallel()
		appSets, err := DecodeParsedFileDataArgoCDApplicationSets(map[string]any{
			"argocd_applicationsets": []map[string]any{
				{"name": "myappset", "generator_source_repos": "https://github.com/myorg/platform-config.git"},
			},
		})
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(appSets) != 1 || appSets[0].Name != "myappset" ||
			appSets[0].GeneratorSourceRepos != "https://github.com/myorg/platform-config.git" {
			t.Fatalf("appSets = %#v, want one myappset row", appSets)
		}
	})

	t.Run("FluxGitRepositories", func(t *testing.T) {
		t.Parallel()
		gitRepositories, err := DecodeParsedFileDataFluxGitRepositories(map[string]any{
			"flux_git_repositories": []map[string]any{
				{"name": "flux-system", "url": "https://github.com/myorg/deploy-repo.git"},
			},
		})
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(gitRepositories) != 1 || gitRepositories[0].Name != "flux-system" ||
			gitRepositories[0].URL != "https://github.com/myorg/deploy-repo.git" {
			t.Fatalf("gitRepositories = %#v, want one flux-system row", gitRepositories)
		}
	})
}
