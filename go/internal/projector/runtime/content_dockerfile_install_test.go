// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDockerfileInstallReplayPreservesRowsAndDistinctSameLineIdentities(t *testing.T) {
	contentFact := facts.Envelope{Payload: map[string]any{
		"content_path": "Dockerfile", "content_body": "FROM alpine\nRUN apk add curl curl=8.2\n",
		"dockerfile_install_names": []any{"curl", "curl"},
	}}
	record, ok := BuildContentRecord(contentFact)
	if !ok || len(record.InstallNames) != 2 || record.InstallNames[0] != "curl" || record.InstallNames[1] != "curl" {
		t.Fatalf("replayed content record = %#v, found=%t", record, ok)
	}
	entityIDs := map[string]struct{}{}
	for _, ordinal := range []string{"1", "2"} {
		fact := facts.Envelope{FactKind: "content_entity", Payload: map[string]any{
			"relative_path": "Dockerfile", "entity_type": "DockerfileInstall",
			"entity_name": "curl", "start_line": 2,
			"entity_metadata": map[string]any{"manager": "apk", "category": "package", "stage": "runtime", "install_ordinal": ordinal},
		}}
		entity, found := BuildContentEntityRecord("repo-a", fact)
		if !found || entity.Metadata["manager"] != "apk" || entity.EntityID == "" {
			t.Fatalf("replayed install entity = %#v, found=%t", entity, found)
		}
		entityIDs[entity.EntityID] = struct{}{}
	}
	if len(entityIDs) != 2 {
		t.Fatalf("same-line declarations collapsed to %d entity IDs", len(entityIDs))
	}
}
