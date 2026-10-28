// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDockerfileInstallGraphProjectionKeepsRepeatedDeclarations(t *testing.T) {
	envelope := func(id, ordinal, version string) facts.Envelope {
		metadata := map[string]any{"install_ordinal": ordinal, "manager": "apt-get", "stage": "builder", "category": "package"}
		if version != "" {
			metadata["version"] = version
		}
		return facts.Envelope{FactID: "install-" + ordinal, FactKind: "content_entity", Payload: map[string]any{
			"repo_id": "repo-a", "relative_path": "Dockerfile", "entity_type": "DockerfileInstall",
			"entity_name": "curl", "entity_id": id, "start_line": 7, "end_line": 7,
			"entity_metadata": metadata,
		}}
	}
	rows := ExtractEntityRows([]facts.Envelope{envelope("", "1", ""), envelope("", "2", "8.2"), envelope("supplied-id", "3", "")}, "repo-a", "/repos/a")
	if len(rows) != 3 {
		t.Fatalf("graph install rows = %d, want 3", len(rows))
	}
	for index, row := range rows {
		if row.Label != "DockerfileInstall" || row.FilePath != "/repos/a/Dockerfile" || row.Metadata["manager"] != "apt-get" {
			t.Fatalf("row %d = %#v", index, row)
		}
	}
	if rows[0].EntityID == rows[1].EntityID || rows[0].EntityID == "" || rows[1].EntityID == "" {
		t.Fatalf("same-line install identities collapsed: %q, %q", rows[0].EntityID, rows[1].EntityID)
	}
	for index, ordinal := range []string{"1", "2"} {
		want := content.CanonicalEntityIDWithMetadata("repo-a", "Dockerfile", "DockerfileInstall", "curl", 7, map[string]any{"install_ordinal": ordinal})
		if rows[index].EntityID != want {
			t.Fatalf("row %d uid = %q, want %q", index, rows[index].EntityID, want)
		}
	}
	if rows[2].EntityID != "supplied-id" {
		t.Fatalf("supplied uid = %q", rows[2].EntityID)
	}
}
