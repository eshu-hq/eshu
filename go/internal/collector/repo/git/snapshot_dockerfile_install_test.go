// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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

func TestSnapshotDockerfileInstallEvidenceNeverPromotesCredentialOptionOrPath(t *testing.T) {
	const marker = "exampleSecretToken123"
	root := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(root, "Dockerfile"),
		"FROM alpine\n"+
			"RUN pip install --index-url https://user:"+marker+"@private.example/simple requests\n"+
			"ADD https://example.test/%74oken%3D"+marker+"/file.tar.gz /tmp/\n"+
			"ARG API_TOKEN="+marker+"\n"+
			"RUN pip install $API_TOKEN\n"+
			"ENV DOWNLOAD_ALIAS=$API_TOKEN\n"+
			"ADD https://example.test/$DOWNLOAD_ALIAS/file.tar.gz /tmp/\n")
	snapshot, err := (NativeRepositorySnapshotter{}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	generation := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-9303-security", time.Now(), snapshot, false, "")
	installCount := 0
	for _, fact := range drainFactChannel(generation.Facts) {
		switch fact.FactKind {
		case "content_entity":
			if fact.Payload["entity_type"] != "DockerfileInstall" {
				continue
			}
			installCount++
			if fact.Payload["entity_name"] != "requests" {
				t.Fatal("credential-bearing option or URL path became a persisted install entity")
			}
			metadata, _ := fact.Payload["entity_metadata"].(map[string]any)
			for _, value := range metadata {
				if strings.Contains(fmt.Sprint(value), marker) {
					t.Fatal("credential marker reached persisted install entity metadata")
				}
			}
		case "content":
			if fact.Payload["content_path"] != "Dockerfile" {
				continue
			}
			names, _ := fact.Payload["dockerfile_install_names"].([]string)
			if len(names) != 1 || names[0] != "requests" {
				t.Fatal("credential-bearing option or URL path reached content reference names")
			}
		}
	}
	if installCount != 1 {
		t.Fatalf("install entities = %d, want one ordinary package", installCount)
	}
}
