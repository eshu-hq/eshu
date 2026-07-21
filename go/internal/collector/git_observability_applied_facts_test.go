// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestStreamFactsEmitsAppliedObservabilityFacts(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	relativePath := filepath.Join("clusters", "prod", "argocd", "observability.yaml")
	filePath := filepath.Join(repoPath, relativePath)
	writeCollectorTestFile(t, filePath, "kind: Application\n")
	observedAt := time.Date(2026, time.June, 1, 16, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		RepoPath:  repoPath,
		FileData: []map[string]any{{
			"path":        filePath,
			"commit_sha":  "abcdef0123456789abcdef0123456789abcdef01",
			"source_kind": "yaml",
			"observability_applied_sync_states": []map[string]any{{
				"source_class":      "applied",
				"source_kind":       "argocd",
				"app_name":          "observability",
				"app_namespace":     "argocd",
				"cluster_name":      "prod",
				"sync_status":       "Synced",
				"health_status":     "Healthy",
				"source_revision":   "abcdef0123456789abcdef0123456789abcdef01",
				"resource_count":    1,
				"outcome":           "exact",
				"cluster_server":    "https://kubernetes.default.svc",
				"dashboard_json":    `{"title":"private"}`,
				"resourceVersion":   "12345",
				"private_label_key": "private",
			}},
			"observability_applied_resources": []map[string]any{{
				"source_class":                 "applied",
				"source_kind":                  "argocd",
				"app_name":                     "observability",
				"resource_kind":                "ServiceMonitor",
				"resource_namespace":           "payments",
				"resource_name":                "checkout-api",
				"observability_resource_class": "scrape_config",
				"sync_status":                  "Synced",
				"outcome":                      "exact",
				"labels":                       map[string]any{"team": "payments"},
			}},
		}},
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: filepath.ToSlash(relativePath),
			Digest:       "digest-applied-observability",
			Language:     "yaml",
			CommitSHA:    "abcdef0123456789abcdef0123456789abcdef01",
		}},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false, "")
	allFacts := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount(), len(allFacts); got != want {
		t.Fatalf("FactCount = %d, want len(allFacts) %d", got, want)
	}
	source := factByKindForTest(t, allFacts, facts.ObservabilitySourceInstanceFactKind)
	if got := source.Payload["source_class"]; got != "applied" {
		t.Fatalf("source_instance source_class = %#v, want applied", got)
	}
	syncState := factByKindForTest(t, allFacts, facts.ObservabilityAppliedSyncStateFactKind)
	if got := syncState.Payload["source_class"]; got != "applied" {
		t.Fatalf("applied sync source_class = %#v, want applied", got)
	}
	if got := syncState.Payload["cluster_server"]; got != nil {
		t.Fatalf("cluster_server = %#v, want redacted", got)
	}
	if got := syncState.Payload["dashboard_json"]; got != nil {
		t.Fatalf("dashboard_json = %#v, want omitted", got)
	}
	if got := syncState.Payload["private_label_key"]; got != nil {
		t.Fatalf("private_label_key = %#v, want omitted", got)
	}
	if got := syncState.Payload["cluster_server_fingerprint"]; got == "" {
		t.Fatal("cluster_server_fingerprint is blank")
	}
	resource := factByKindForTest(t, allFacts, facts.ObservabilityAppliedResourceFactKind)
	if got := resource.Payload["observability_resource_class"]; got != "scrape_config" {
		t.Fatalf("observability_resource_class = %#v, want scrape_config", got)
	}
	if got := resource.Payload["labels"]; got != nil {
		t.Fatalf("labels = %#v, want omitted", got)
	}
}
