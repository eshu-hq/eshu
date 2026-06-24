// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchdocs

import (
	"testing"
	"time"
)

func TestProjectContentEntityBuildsStableCodeDocument(t *testing.T) {
	indexedAt := time.Date(2026, 6, 2, 4, 0, 0, 0, time.UTC)
	doc, decision := ProjectContentEntity(ContentEntity{
		EntityID:     "content-entity:e123",
		RepoID:       "repo-payments",
		RelativePath: "src/payment.go",
		EntityType:   "Function",
		EntityName:   "HandlePayment",
		StartLine:    12,
		EndLine:      30,
		Language:     "Go",
		SourceCache:  "func HandlePayment(ctx context.Context) error { return nil }",
		IndexedAt:    indexedAt,
	})

	if !decision.Include {
		t.Fatalf("ProjectContentEntity decision.Include = false, reason = %q", decision.Reason)
	}
	if got, want := doc.ID, "searchdoc:content_entity:content-entity:e123"; got != want {
		t.Fatalf("doc.ID = %q, want %q", got, want)
	}
	if got, want := doc.SourceKind, SourceKindCodeEntity; got != want {
		t.Fatalf("doc.SourceKind = %q, want %q", got, want)
	}
	if got, want := doc.Title, "Function HandlePayment"; got != want {
		t.Fatalf("doc.Title = %q, want %q", got, want)
	}
	if got, want := doc.Path, "src/payment.go"; got != want {
		t.Fatalf("doc.Path = %q, want %q", got, want)
	}
	if got := doc.ContextText; got == "" {
		t.Fatal("doc.ContextText is empty")
	}
	assertHasGraphHandle(t, doc, "content_entity", "content-entity:e123")
	assertHasGraphHandle(t, doc, "repository", "repo-payments")
	assertHasLabel(t, doc, "language:go")
	assertHasLabel(t, doc, "entity_type:function")
	if got, want := doc.TruthScope.Level, TruthLevelDerived; got != want {
		t.Fatalf("doc.TruthScope.Level = %q, want %q", got, want)
	}
	if got, want := doc.TruthScope.Basis, TruthBasisContentIndex; got != want {
		t.Fatalf("doc.TruthScope.Basis = %q, want %q", got, want)
	}
	if got, want := doc.Freshness.State, FreshnessFresh; got != want {
		t.Fatalf("doc.Freshness.State = %q, want %q", got, want)
	}
	if !doc.UpdatedAt.Equal(indexedAt) {
		t.Fatalf("doc.UpdatedAt = %s, want %s", doc.UpdatedAt, indexedAt)
	}
}

func TestProjectContentFileBuildsRepositoryFileDocument(t *testing.T) {
	doc, decision := ProjectContentFile(ContentFile{
		RepoID:       "repo-platform",
		RelativePath: "deploy/service.yaml",
		Language:     "Kubernetes",
		Content:      "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n",
	})

	if !decision.Include {
		t.Fatalf("ProjectContentFile decision.Include = false, reason = %q", decision.Reason)
	}
	if got, want := doc.ID, "searchdoc:content_file:repo-platform:deploy/service.yaml"; got != want {
		t.Fatalf("doc.ID = %q, want %q", got, want)
	}
	if got, want := doc.SourceKind, SourceKindRepositoryFile; got != want {
		t.Fatalf("doc.SourceKind = %q, want %q", got, want)
	}
	assertHasGraphHandle(t, doc, "repository", "repo-platform")
	assertHasGraphHandle(t, doc, "file", "repo-platform:deploy/service.yaml")
	assertHasLabel(t, doc, "language:kubernetes")
}

func TestProjectContentEntityOmitsFileHandleWithoutPath(t *testing.T) {
	doc, decision := ProjectContentEntity(ContentEntity{
		EntityID:    "content-entity:e-no-path",
		RepoID:      "repo-payments",
		EntityType:  "Package",
		EntityName:  "payments",
		SourceCache: "package payments",
	})

	if !decision.Include {
		t.Fatalf("ProjectContentEntity decision.Include = false, reason = %q", decision.Reason)
	}
	assertHasGraphHandle(t, doc, "content_entity", "content-entity:e-no-path")
	assertHasGraphHandle(t, doc, "repository", "repo-payments")
	assertNoGraphHandle(t, doc, "file", "repo-payments:")
}

func TestProjectRuntimeSummaryBuildsRuntimeDocument(t *testing.T) {
	doc, decision := ProjectRuntimeSummary(RuntimeSummary{
		ID:          "service:payments-api",
		RepoID:      "repo-payments",
		Title:       "payments-api",
		Summary:     "Kubernetes workload payments-api uses image ghcr.io/acme/payments@sha256:abc",
		ServiceID:   "service:payments-api",
		WorkloadID:  "workload:payments-api",
		ImageDigest: "sha256:abc",
	})

	if !decision.Include {
		t.Fatalf("ProjectRuntimeSummary decision.Include = false, reason = %q", decision.Reason)
	}
	if got, want := doc.SourceKind, SourceKindRuntimeSummary; got != want {
		t.Fatalf("doc.SourceKind = %q, want %q", got, want)
	}
	assertHasGraphHandle(t, doc, "service", "service:payments-api")
	assertHasGraphHandle(t, doc, "workload", "workload:payments-api")
	assertHasGraphHandle(t, doc, "container_image", "sha256:abc")
	assertHasGraphHandle(t, doc, "runtime_summary", "service:payments-api")
	assertHasLabel(t, doc, "runtime")
}

func TestProjectSemanticContextBuildsDerivedLabelDocument(t *testing.T) {
	updatedAt := time.Date(2026, 6, 6, 14, 30, 0, 0, time.UTC)
	doc, decision := ProjectSemanticContext(SemanticContext{
		ID:          "semantic-context:checkout-alert-routing",
		RepoID:      "repo-checkout",
		Title:       "Checkout alert routing context",
		ContextText: "checkout-api alerts route through the pagerduty primary policy during deploys",
		ServiceID:   "service:checkout-api",
		WorkloadID:  "workload:checkout-api",
		Environment: "prod",
		Labels:      []string{"alert-routing", "deployment"},
		SourceIDs:   []string{"service:checkout-api", "workload:checkout-api"},
		UpdatedAt:   updatedAt,
	})

	if !decision.Include {
		t.Fatalf("ProjectSemanticContext decision.Include = false, reason = %q", decision.Reason)
	}
	if got, want := doc.ID, "searchdoc:semantic_context:semantic-context:checkout-alert-routing"; got != want {
		t.Fatalf("doc.ID = %q, want %q", got, want)
	}
	if got, want := doc.SourceKind, SourceKindSemanticContext; got != want {
		t.Fatalf("doc.SourceKind = %q, want %q", got, want)
	}
	if got, want := doc.TruthScope.Level, TruthLevelDerived; got != want {
		t.Fatalf("doc.TruthScope.Level = %q, want %q", got, want)
	}
	if got, want := doc.TruthScope.Basis, TruthBasisReadModel; got != want {
		t.Fatalf("doc.TruthScope.Basis = %q, want %q", got, want)
	}
	if !doc.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("doc.UpdatedAt = %s, want %s", doc.UpdatedAt, updatedAt)
	}
	assertHasGraphHandle(t, doc, "semantic_context", "semantic-context:checkout-alert-routing")
	assertHasGraphHandle(t, doc, "repository", "repo-checkout")
	assertHasGraphHandle(t, doc, "service", "service:checkout-api")
	assertHasGraphHandle(t, doc, "workload", "workload:checkout-api")
	assertHasGraphHandle(t, doc, "environment", "prod")
	assertHasLabel(t, doc, "semantic_context")
	assertHasLabel(t, doc, "alert-routing")
	assertHasLabel(t, doc, "deployment")
}

func TestProjectSemanticContextRejectsUnboundedOrSensitiveRows(t *testing.T) {
	tests := []struct {
		name   string
		input  SemanticContext
		reason ExclusionReason
	}{
		{
			name: "missing stable id",
			input: SemanticContext{
				RepoID:      "repo-checkout",
				ContextText: "checkout ownership notes",
				ServiceID:   "service:checkout-api",
			},
			reason: ReasonMissingStableHandle,
		},
		{
			name: "missing bounded graph handle",
			input: SemanticContext{
				ID:          "semantic-context:unscoped",
				ContextText: "unscoped semantic context should not enter retrieval",
			},
			reason: ReasonMissingStableHandle,
		},
		{
			name: "secret context",
			input: SemanticContext{
				ID:          "semantic-context:secret",
				RepoID:      "repo-checkout",
				ContextText: `pagerduty_token = "super-secret"`,
			},
			reason: ReasonSensitiveContext,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, decision := ProjectSemanticContext(tt.input)
			if decision.Include {
				t.Fatalf("decision.Include = true, doc = %#v", doc)
			}
			if got := decision.Reason; got != tt.reason {
				t.Fatalf("decision.Reason = %q, want %q", got, tt.reason)
			}
		})
	}
}

func TestProjectContentEntityDropsSensitiveOrNoisyRows(t *testing.T) {
	tests := []struct {
		name   string
		input  ContentEntity
		reason ExclusionReason
	}{
		{
			name: "secret context",
			input: ContentEntity{
				EntityID:     "content-entity:e-secret",
				RepoID:       "repo-payments",
				RelativePath: "src/config.go",
				EntityType:   "Variable",
				EntityName:   "DatabasePassword",
				SourceCache:  `password = "super-secret"`,
			},
			reason: ReasonSensitiveContext,
		},
		{
			name: "dashboard payload",
			input: ContentEntity{
				EntityID:     "content-entity:e-dashboard",
				RepoID:       "repo-platform",
				RelativePath: "dashboards/api.json",
				EntityType:   "DashboardAsset",
				EntityName:   "api-dashboard",
				SourceCache:  `{"panels":[{"targets":[{"expr":"rate(http_requests_total[5m])"}]}]}`,
			},
			reason: ReasonExcludedSourceKind,
		},
		{
			name: "high cardinality metadata",
			input: ContentEntity{
				EntityID:     "content-entity:e-noisy",
				RepoID:       "repo-platform",
				RelativePath: "logs/sample.log",
				EntityType:   "DataAsset",
				EntityName:   "raw-log-line",
				Metadata: map[string]string{
					"log_line": "2026-06-02T04:00:00Z user_id=123456789 request_id=abcdef",
				},
			},
			reason: ReasonExcludedSourceKind,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, decision := ProjectContentEntity(tt.input)
			if decision.Include {
				t.Fatalf("decision.Include = true, doc = %#v", doc)
			}
			if got := decision.Reason; got != tt.reason {
				t.Fatalf("decision.Reason = %q, want %q", got, tt.reason)
			}
		})
	}
}

func TestProjectContentFileDropsNoisyArtifacts(t *testing.T) {
	tests := []struct {
		name         string
		artifactType string
		content      string
	}{
		{
			name:         "dashboard json",
			artifactType: "dashboard_json",
			content:      `{"title":"checkout latency","panels":[{"targets":[{"expr":"rate(http_requests_total[5m])"}]}]}`,
		},
		{
			name:         "query body",
			artifactType: "query_body",
			content:      `select package_name, version from package_registry_metadata`,
		},
		{
			name:         "raw provider payload",
			artifactType: "raw_provider_payload",
			content:      `{"provider":"example","resource_count":12}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, decision := ProjectContentFile(ContentFile{
				RepoID:       "repo-platform",
				RelativePath: "observability/artifact.json",
				ArtifactType: tt.artifactType,
				Content:      tt.content,
			})
			if decision.Include {
				t.Fatalf("decision.Include = true, doc = %#v", doc)
			}
			if got, want := decision.Reason, ReasonExcludedSourceKind; got != want {
				t.Fatalf("decision.Reason = %q, want %q", got, want)
			}
		})
	}
}

func TestProjectContentEntityRejectsAmbiguousRowsWithoutStableHandle(t *testing.T) {
	tests := []ContentEntity{
		{RepoID: "repo-payments", RelativePath: "src/payment.go", EntityType: "Function", EntityName: "HandlePayment"},
		{EntityID: "content-entity:e123", RelativePath: "src/payment.go", EntityType: "Function", EntityName: "HandlePayment"},
	}

	for _, input := range tests {
		doc, decision := ProjectContentEntity(input)
		if decision.Include {
			t.Fatalf("decision.Include = true for input %#v, doc = %#v", input, doc)
		}
		if got, want := decision.Reason, ReasonMissingStableHandle; got != want {
			t.Fatalf("decision.Reason = %q, want %q", got, want)
		}
	}
}

func assertHasGraphHandle(t *testing.T, doc Document, kind string, id string) {
	t.Helper()
	for _, handle := range doc.GraphHandles {
		if handle.Kind == kind && handle.ID == id {
			return
		}
	}
	t.Fatalf("doc.GraphHandles missing %s:%s in %#v", kind, id, doc.GraphHandles)
}

func assertNoGraphHandle(t *testing.T, doc Document, kind string, id string) {
	t.Helper()
	for _, handle := range doc.GraphHandles {
		if handle.Kind == kind && handle.ID == id {
			t.Fatalf("doc.GraphHandles contains %s:%s in %#v", kind, id, doc.GraphHandles)
		}
	}
}

func assertHasLabel(t *testing.T, doc Document, label string) {
	t.Helper()
	for _, got := range doc.Labels {
		if got == label {
			return
		}
	}
	t.Fatalf("doc.Labels missing %q in %#v", label, doc.Labels)
}
