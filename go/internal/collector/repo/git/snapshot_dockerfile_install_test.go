// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotDockerfileInstallEvidenceReachesContentFact(t *testing.T) {
	root, err := filepath.Abs("testdata/content_version")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := (NativeRepositorySnapshotter{}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	generation := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-9303", time.Now(), snapshot, false, "")
	foundContent := false
	entityIDs := map[string]struct{}{}
	for _, fact := range drainFactChannel(generation.Facts) {
		if fact.FactKind == "content_entity" && fact.Payload["entity_type"] == "DockerfileInstall" && fact.Payload["entity_name"] == "curl" {
			metadata, _ := fact.Payload["entity_metadata"].(map[string]any)
			if metadata["manager"] != "apt-get" || metadata["stage"] == "" {
				t.Fatalf("install entity metadata = %#v", metadata)
			}
			id, _ := fact.Payload["entity_id"].(string)
			entityIDs[id] = struct{}{}
		}

		if fact.FactKind != "content" || fact.Payload["content_path"] != "Dockerfile" {
			continue
		}
		if names, ok := fact.Payload["dockerfile_install_names"].([]string); !ok || len(names) != 2 || names[0] != "curl" || names[1] != "curl" {
			t.Fatalf("Dockerfile content install names = %#v, want two curl declarations", fact.Payload["dockerfile_install_names"])
		}
		foundContent = true
	}
	if !foundContent || len(entityIDs) != 2 {
		t.Fatalf("Dockerfile content=%t distinct entity IDs=%d, want two", foundContent, len(entityIDs))
	}
}
