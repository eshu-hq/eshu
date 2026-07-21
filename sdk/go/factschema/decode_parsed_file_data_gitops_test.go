// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import "testing"

// The fixtures below mirror the exact row shapes
// go/internal/parser/yaml's producers emit (helm.go parseHelmChart/
// parseHelmValues, argocd.go parseArgoCDApplication/
// parseArgoCDApplicationSet, flux_source.go parseFluxSourceRepository),
// cited field-by-field against that source rather than an imagined shape.
// go/internal/relationships' real-parser-driven tests
// (structured_family_evidence_test.go, flux_evidence_test.go) are the
// authoritative equivalence proof that runs the actual YAML parser; these
// package-level tests exercise the decode function itself, including the
// malformed-input paths a real parser can never produce by construction.

// TestDecodeParsedFileDataHelmCharts_TypedRows proves the helm_charts inner
// key decodes into typed []HelmChart rows exposing Name, Dependencies, and
// DependencyRepositories, matching go/internal/parser/yaml/helm.go's
// parseHelmChart row shape (comma-joined CSV strings, not JSON arrays).
func TestDecodeParsedFileDataHelmCharts_TypedRows(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"helm_charts": []any{
			map[string]any{
				"name":                    "checkout-service",
				"line_number":             float64(1),
				"version":                 "1.2.3",
				"app_version":             "2.0.0",
				"chart_type":              "application",
				"description":             "",
				"dependencies":            "redis",
				"dependency_repositories": "https://charts.example.test/redis",
				"path":                    "Chart.yaml",
				"lang":                    "yaml",
			},
		},
	}

	charts, err := DecodeParsedFileDataHelmCharts(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataHelmCharts() error = %v, want nil", err)
	}
	if len(charts) != 1 {
		t.Fatalf("len(charts) = %d, want 1", len(charts))
	}
	chart := charts[0]
	if chart.Name != "checkout-service" {
		t.Fatalf("Name = %q", chart.Name)
	}
	if chart.Dependencies != "redis" {
		t.Fatalf("Dependencies = %q", chart.Dependencies)
	}
	if chart.DependencyRepositories != "https://charts.example.test/redis" {
		t.Fatalf("DependencyRepositories = %q", chart.DependencyRepositories)
	}
	if chart.Attributes == nil {
		t.Fatal("Attributes = nil, want the non-read producer fields captured")
	}
	if got, ok := chart.Attributes["version"].(string); !ok || got != "1.2.3" {
		t.Fatalf("Attributes[version] = %#v, want string \"1.2.3\"", chart.Attributes["version"])
	}
}

// TestDecodeParsedFileDataHelmCharts_Absent proves an absent helm_charts key
// decodes to a nil slice with no error.
func TestDecodeParsedFileDataHelmCharts_Absent(t *testing.T) {
	t.Parallel()

	charts, err := DecodeParsedFileDataHelmCharts(map[string]any{"lang": "yaml"})
	if err != nil {
		t.Fatalf("DecodeParsedFileDataHelmCharts() error = %v, want nil", err)
	}
	if charts != nil {
		t.Fatalf("charts = %#v, want nil for an absent helm_charts key", charts)
	}
}

// TestDecodeParsedFileDataHelmValues_TypedRows proves the helm_values inner
// key decodes into typed []HelmValues rows exposing Name and
// ImageRepositories, matching go/internal/parser/yaml/helm.go's
// parseHelmValues row shape.
func TestDecodeParsedFileDataHelmValues_TypedRows(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"helm_values": []any{
			map[string]any{
				"name":               "values",
				"line_number":        float64(1),
				"top_level_keys":     "image,replicaCount",
				"image_repositories": "ghcr.io/example/checkout-service",
				"path":               "values.yaml",
				"lang":               "yaml",
			},
		},
	}

	values, err := DecodeParsedFileDataHelmValues(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataHelmValues() error = %v, want nil", err)
	}
	if len(values) != 1 {
		t.Fatalf("len(values) = %d, want 1", len(values))
	}
	value := values[0]
	if value.Name != "values" || value.ImageRepositories != "ghcr.io/example/checkout-service" {
		t.Fatalf("Name/ImageRepositories = %q/%q", value.Name, value.ImageRepositories)
	}
}

// TestDecodeParsedFileDataArgoCDApplications_TypedRows proves the
// argocd_applications inner key decodes into typed []ArgoCDApplication rows
// exposing every field argoApplicationSourceRefs reads, matching
// go/internal/parser/yaml/argocd.go's parseArgoCDApplication row shape.
func TestDecodeParsedFileDataArgoCDApplications_TypedRows(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"argocd_applications": []any{
			map[string]any{
				"name":            "checkout",
				"line_number":     float64(1),
				"namespace":       "argocd",
				"project":         "default",
				"dest_name":       "",
				"dest_server":     "https://kubernetes.default.svc",
				"dest_namespace":  "checkout",
				"path":            "application.yaml",
				"lang":            "yaml",
				"source_repo":     "https://github.com/example/checkout-deploy",
				"source_path":     "overlays/prod",
				"source_revision": "main",
				"source_root":     "overlays/",
			},
		},
	}

	applications, err := DecodeParsedFileDataArgoCDApplications(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataArgoCDApplications() error = %v, want nil", err)
	}
	if len(applications) != 1 {
		t.Fatalf("len(applications) = %d, want 1", len(applications))
	}
	application := applications[0]
	if application.Name != "checkout" {
		t.Fatalf("Name = %q", application.Name)
	}
	if application.SourceRepo != "https://github.com/example/checkout-deploy" {
		t.Fatalf("SourceRepo = %q", application.SourceRepo)
	}
	if application.SourcePath != "overlays/prod" || application.SourceRevision != "main" || application.SourceRoot != "overlays/" {
		t.Fatalf("SourcePath/SourceRevision/SourceRoot = %q/%q/%q", application.SourcePath, application.SourceRevision, application.SourceRoot)
	}
	if application.DestServer != "https://kubernetes.default.svc" || application.DestNamespace != "checkout" {
		t.Fatalf("DestServer/DestNamespace = %q/%q", application.DestServer, application.DestNamespace)
	}
}

// TestDecodeParsedFileDataArgoCDApplications_WholeRowDroppedOnFieldTypeMismatch
// locks in the deliberate, documented (Finding 4, issue #5445 review)
// row-atomic decode contract: a single wrong-typed field (here source_repos
// as a []any instead of the codegraphv1.ArgoCDApplication.SourceRepos string
// the wire always carries -- go/internal/parser/yaml/argocd.go's
// joinArgoSourceTupleValues never emits anything else) makes decodeMapInto
// error for that ENTIRE row, and decodeParsedFileDataTolerantSlice skips the
// whole Application, not just the one bad field. This is stricter than the
// pre-#5445 raw-map read, which passed the raw value straight to
// tupleCSVValues (tolerant of string/[]string/[]any per field). See
// go/internal/relationships/structured_family_evidence.go's
// argoApplicationSourceRefs doc comment for the full reasoning. A second,
// well-formed Application row in the same slice must still decode.
func TestDecodeParsedFileDataArgoCDApplications_WholeRowDroppedOnFieldTypeMismatch(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"argocd_applications": []any{
			map[string]any{
				"name":         "malformed-source-repos",
				"source_repos": []any{"https://github.com/example/a", "https://github.com/example/b"},
			},
			map[string]any{
				"name":         "well-formed",
				"source_repos": "https://github.com/example/checkout-deploy",
			},
		},
	}

	applications, err := DecodeParsedFileDataArgoCDApplications(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataArgoCDApplications() error = %v, want nil (malformed row skipped, not surfaced)", err)
	}
	if len(applications) != 1 {
		t.Fatalf("applications = %#v, want exactly the well-formed row (malformed row's whole entry dropped)", applications)
	}
	if applications[0].Name != "well-formed" {
		t.Fatalf("applications[0].Name = %q, want %q", applications[0].Name, "well-formed")
	}
}

// TestDecodeParsedFileDataArgoCDApplicationSets_TypedRows proves the
// argocd_applicationsets inner key decodes into typed
// []ArgoCDApplicationSet rows exposing every field
// discoverStructuredArgoCDEvidence and structuredApplicationSetGeneratorRepos
// read, matching go/internal/parser/yaml/argocd.go's
// parseArgoCDApplicationSet row shape.
func TestDecodeParsedFileDataArgoCDApplicationSets_TypedRows(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"argocd_applicationsets": []any{
			map[string]any{
				"name":                   "checkout-envs",
				"line_number":            float64(1),
				"namespace":              "argocd",
				"generators":             "git",
				"project":                "default",
				"dest_name":              "",
				"dest_server":            "https://kubernetes.default.svc",
				"dest_namespace":         "checkout",
				"source_repos":           "https://github.com/example/checkout-config",
				"source_paths":           "envs/*",
				"generator_source_repos": "https://github.com/example/checkout-config",
				"generator_source_paths": "envs/*",
				"generator_source_roots": "envs/",
				"template_source_repos":  "https://github.com/example/checkout-deploy",
				"template_source_paths":  "overlays/{{path.basename}}",
				"template_source_roots":  "overlays/",
				"source_roots":           "envs/",
				"path":                   "applicationset.yaml",
				"lang":                   "yaml",
			},
		},
	}

	appSets, err := DecodeParsedFileDataArgoCDApplicationSets(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataArgoCDApplicationSets() error = %v, want nil", err)
	}
	if len(appSets) != 1 {
		t.Fatalf("len(appSets) = %d, want 1", len(appSets))
	}
	appSet := appSets[0]
	if appSet.Name != "checkout-envs" {
		t.Fatalf("Name = %q", appSet.Name)
	}
	if appSet.GeneratorSourceRepos != "https://github.com/example/checkout-config" {
		t.Fatalf("GeneratorSourceRepos = %q", appSet.GeneratorSourceRepos)
	}
	if appSet.TemplateSourceRepos != "https://github.com/example/checkout-deploy" {
		t.Fatalf("TemplateSourceRepos = %q", appSet.TemplateSourceRepos)
	}
	if appSet.GeneratorSourceRoots != "envs/" || appSet.TemplateSourceRoots != "overlays/" || appSet.SourceRoots != "envs/" {
		t.Fatalf("GeneratorSourceRoots/TemplateSourceRoots/SourceRoots = %q/%q/%q", appSet.GeneratorSourceRoots, appSet.TemplateSourceRoots, appSet.SourceRoots)
	}
	if appSet.DestServer != "https://kubernetes.default.svc" || appSet.DestNamespace != "checkout" {
		t.Fatalf("DestServer/DestNamespace = %q/%q", appSet.DestServer, appSet.DestNamespace)
	}
}

// TestDecodeParsedFileDataFluxGitRepositories_TypedRows proves the
// flux_git_repositories inner key decodes into typed []FluxGitRepository
// rows exposing Name and URL, matching
// go/internal/parser/yaml/flux_source.go's parseFluxSourceRepository row
// shape.
func TestDecodeParsedFileDataFluxGitRepositories_TypedRows(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"flux_git_repositories": []any{
			map[string]any{
				"name":        "checkout-config",
				"line_number": float64(1),
				"path":        "gitrepository.yaml",
				"lang":        "yaml",
				"url":         "https://github.com/example/checkout-config",
				"ref_branch":  "main",
			},
		},
	}

	gitRepositories, err := DecodeParsedFileDataFluxGitRepositories(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataFluxGitRepositories() error = %v, want nil", err)
	}
	if len(gitRepositories) != 1 {
		t.Fatalf("len(gitRepositories) = %d, want 1", len(gitRepositories))
	}
	gitRepository := gitRepositories[0]
	if gitRepository.Name != "checkout-config" || gitRepository.URL != "https://github.com/example/checkout-config" {
		t.Fatalf("Name/URL = %q/%q", gitRepository.Name, gitRepository.URL)
	}
}

// TestDecodeParsedFileDataFluxGitRepositories_AbsentURL proves a
// flux_git_repositories row with no url key -- which parseFluxSourceRepository
// omits entirely when spec.url is absent or empty -- decodes to an empty URL
// with no error, matching discoverFluxGitRepositoryEvidence's prior
// payloadString(row, "url") read of a missing key (which short-circuits to
// "no evidence" rather than an error).
func TestDecodeParsedFileDataFluxGitRepositories_AbsentURL(t *testing.T) {
	t.Parallel()

	gitRepositories, err := DecodeParsedFileDataFluxGitRepositories(map[string]any{
		"flux_git_repositories": []any{
			map[string]any{"name": "checkout-config", "line_number": float64(1), "path": "gitrepository.yaml", "lang": "yaml"},
		},
	})
	if err != nil {
		t.Fatalf("DecodeParsedFileDataFluxGitRepositories() error = %v, want nil", err)
	}
	if len(gitRepositories) != 1 || gitRepositories[0].URL != "" {
		t.Fatalf("gitRepositories = %#v, want one row with empty URL", gitRepositories)
	}
}

// TestDecodeParsedFileDataArgoCDApplications_WrongTopLevelShape proves a
// present-but-not-any-recognized-slice-shape argocd_applications value
// surfaces a wrapped error, matching asObjectSlice's whole-container shape
// check.
func TestDecodeParsedFileDataArgoCDApplications_WrongTopLevelShape(t *testing.T) {
	t.Parallel()

	_, err := DecodeParsedFileDataArgoCDApplications(map[string]any{
		"argocd_applications": "not-a-slice",
	})
	if err == nil {
		t.Fatal("DecodeParsedFileDataArgoCDApplications() error = nil, want error for a non-slice argocd_applications value")
	}
}

// TestDecodeParsedFileDataArgoCDApplications_MalformedElementSkipped proves a
// non-object element inside an otherwise well-formed argocd_applications
// slice is SKIPPED, not an aborting error, so one malformed Application
// entry never drops every other well-formed Application in the same
// multi-document YAML file -- the same per-element tolerance
// discoverStructuredArgoCDEvidence's pre-typing raw-map read had
// (item, ok := raw.(map[string]any); if !ok { continue }).
func TestDecodeParsedFileDataArgoCDApplications_MalformedElementSkipped(t *testing.T) {
	t.Parallel()

	applications, err := DecodeParsedFileDataArgoCDApplications(map[string]any{
		"argocd_applications": []any{
			"not-an-object",
			map[string]any{"name": "checkout", "dest_server": "https://kubernetes.default.svc"},
		},
	})
	if err != nil {
		t.Fatalf("DecodeParsedFileDataArgoCDApplications() error = %v, want nil (malformed element skipped)", err)
	}
	if len(applications) != 1 || applications[0].Name != "checkout" {
		t.Fatalf("applications = %#v, want one row for the well-formed element", applications)
	}
}
