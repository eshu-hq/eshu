// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"log/slog"
	"testing"
)

// recordingSlogHandler is a minimal slog.Handler that captures every record
// passed to it, so a test can assert a specific log fired without depending
// on log output formatting. It ignores grouping/WithAttrs (unused by the
// single flat Debug call under test here).
type recordingSlogHandler struct {
	records *[]slog.Record
}

func newRecordingLogger() (*slog.Logger, *[]slog.Record) {
	records := &[]slog.Record{}
	handler := recordingSlogHandler{records: records}
	return slog.New(handler), records
}

func (h recordingSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h recordingSlogHandler) Handle(_ context.Context, record slog.Record) error {
	*h.records = append(*h.records, record)
	return nil
}

func (h recordingSlogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h recordingSlogHandler) WithGroup(string) slog.Handler { return h }

func recordAttr(record slog.Record, key string) (string, bool) {
	var (
		value string
		found bool
	)
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == key {
			value = attr.Value.String()
			found = true
			return false
		}
		return true
	})
	return value, found
}

// TestBuildOutgoingK8sSelectRelationshipsLogsMixedVintageDropAtDebug proves
// the k8sSelectMatch mixed-vintage drop (Service selector known and
// non-empty, candidate Deployment row predates pod_template_labels capture)
// fires a Debug-level structured log carrying both entity IDs, so an
// operator can diagnose the transient missing SELECTS edge without reading
// source. This is the review-thread addition to #5343 Fix 2's telemetry.
func TestBuildOutgoingK8sSelectRelationshipsLogsMixedVintageDropAtDebug(t *testing.T) {
	t.Parallel()

	logger, records := newRecordingLogger()
	reader := boundedK8sFakeContentStore{rows: []EntityContent{
		{
			EntityID:     "deployment-vintage",
			RepoID:       "repo-1",
			RelativePath: "deploy/frontend.yaml",
			EntityType:   "K8sResource",
			EntityName:   "frontend-deploy",
			Metadata: map[string]any{
				"kind":           "Deployment",
				"namespace":      "prod",
				"qualified_name": "prod/Deployment/frontend-deploy",
				// pod_template_labels intentionally absent: pre-upgrade row.
			},
		},
	}}
	service := EntityContent{
		EntityID:     "service-1",
		RepoID:       "repo-1",
		RelativePath: "deploy/service.yaml",
		EntityType:   "K8sResource",
		EntityName:   "web",
		Metadata: map[string]any{
			"kind":           "Service",
			"namespace":      "prod",
			"qualified_name": "prod/Service/web",
			"selector":       "app=frontend",
		},
	}

	relationships, ok, truncated, err := buildOutgoingK8sSelectRelationships(context.Background(), reader, service, logger)
	if err != nil {
		t.Fatalf("buildOutgoingK8sSelectRelationships() error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("buildOutgoingK8sSelectRelationships() ok = false, want true")
	}
	if truncated {
		t.Fatalf("buildOutgoingK8sSelectRelationships() truncated = true, want false")
	}
	if len(relationships) != 0 {
		t.Fatalf("len(relationships) = %d, want 0 (mixed vintage must not match or fall back): %#v", len(relationships), relationships)
	}

	if len(*records) != 1 {
		t.Fatalf("log records = %d, want 1: %#v", len(*records), *records)
	}
	record := (*records)[0]
	if record.Level != slog.LevelDebug {
		t.Fatalf("log level = %v, want Debug (self-heals on re-ingest, not an alert)", record.Level)
	}
	if gotService, ok := recordAttr(record, "service_entity_id"); !ok || gotService != "service-1" {
		t.Fatalf("log service_entity_id = %q (present=%v), want %q", gotService, ok, "service-1")
	}
	if gotWorkload, ok := recordAttr(record, "workload_entity_id"); !ok || gotWorkload != "deployment-vintage" {
		t.Fatalf("log workload_entity_id = %q (present=%v), want %q", gotWorkload, ok, "deployment-vintage")
	}
}

// TestBuildOutgoingK8sSelectRelationshipsNoLogOnRealMatch proves the Debug
// log is precise: a genuine selector match (workload carries
// pod_template_labels) must NOT emit the mixed-vintage diagnostic.
func TestBuildOutgoingK8sSelectRelationshipsNoLogOnRealMatch(t *testing.T) {
	t.Parallel()

	logger, records := newRecordingLogger()
	reader := boundedK8sFakeContentStore{rows: []EntityContent{
		{
			EntityID:     "deployment-1",
			RepoID:       "repo-1",
			RelativePath: "deploy/frontend.yaml",
			EntityType:   "K8sResource",
			EntityName:   "frontend-deploy",
			Metadata: map[string]any{
				"kind":                "Deployment",
				"namespace":           "prod",
				"qualified_name":      "prod/Deployment/frontend-deploy",
				"pod_template_labels": "app=frontend",
			},
		},
	}}
	service := EntityContent{
		EntityID:     "service-1",
		RepoID:       "repo-1",
		RelativePath: "deploy/service.yaml",
		EntityType:   "K8sResource",
		EntityName:   "web",
		Metadata: map[string]any{
			"kind":           "Service",
			"namespace":      "prod",
			"qualified_name": "prod/Service/web",
			"selector":       "app=frontend",
		},
	}

	relationships, _, _, err := buildOutgoingK8sSelectRelationships(context.Background(), reader, service, logger)
	if err != nil {
		t.Fatalf("buildOutgoingK8sSelectRelationships() error = %v, want nil", err)
	}
	if len(relationships) != 1 {
		t.Fatalf("len(relationships) = %d, want 1", len(relationships))
	}
	if len(*records) != 0 {
		t.Fatalf("log records = %d, want 0 (a real match must not log the mixed-vintage diagnostic): %#v", len(*records), *records)
	}
}

// TestBuildOutgoingK8sSelectRelationshipsMixedVintageNilLoggerSafe proves a
// nil logger (e.g. CodeHandler's relationshipsFromEntity call path, which has
// no Logger field) is a safe no-op, not a panic, on the mixed-vintage path.
func TestBuildOutgoingK8sSelectRelationshipsMixedVintageNilLoggerSafe(t *testing.T) {
	t.Parallel()

	reader := boundedK8sFakeContentStore{rows: []EntityContent{
		{
			EntityID:     "deployment-vintage",
			RepoID:       "repo-1",
			RelativePath: "deploy/frontend.yaml",
			EntityType:   "K8sResource",
			EntityName:   "frontend-deploy",
			Metadata: map[string]any{
				"kind":           "Deployment",
				"namespace":      "prod",
				"qualified_name": "prod/Deployment/frontend-deploy",
			},
		},
	}}
	service := EntityContent{
		EntityID:     "service-1",
		RepoID:       "repo-1",
		RelativePath: "deploy/service.yaml",
		EntityType:   "K8sResource",
		EntityName:   "web",
		Metadata: map[string]any{
			"kind":           "Service",
			"namespace":      "prod",
			"qualified_name": "prod/Service/web",
			"selector":       "app=frontend",
		},
	}

	if _, _, _, err := buildOutgoingK8sSelectRelationships(context.Background(), reader, service, nil); err != nil {
		t.Fatalf("buildOutgoingK8sSelectRelationships() error = %v, want nil", err)
	}
}
