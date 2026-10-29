// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

func TestDockerfileInstallWriterClearsOptionalVersionAndRetractsStaleNodes(t *testing.T) {
	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := canonical.CanonicalMaterialization{
		RepoID: "repo-a", ScopeID: "scope-a", GenerationID: "gen-2",
		Entities: []canonical.EntityRow{
			{EntityID: "install-1", Label: "DockerfileInstall", EntityName: "curl", FilePath: "/repos/a/Dockerfile", RelativePath: "Dockerfile", StartLine: 7, RepoID: "repo-a", Metadata: map[string]any{"manager": "apt-get", "stage": "builder", "category": "package", "install_ordinal": "1"}},
			{EntityID: "install-2", Label: "DockerfileInstall", EntityName: "curl", FilePath: "/repos/a/Dockerfile", RelativePath: "Dockerfile", StartLine: 7, RepoID: "repo-a", Metadata: map[string]any{"manager": "apt-get", "stage": "builder", "category": "package", "install_ordinal": "2", "version": "8.2"}},
		},
	}
	statements := writer.buildEntityStatements(mat)
	var rows []map[string]any
	for _, stmt := range statements {
		if strings.Contains(stmt.Cypher, "MERGE (n:DockerfileInstall") {
			if !strings.Contains(stmt.Cypher, "SET n += row.props") {
				t.Fatalf("install writer lost map update: %s", stmt.Cypher)
			}
			rows = append(rows, stmt.Parameters["rows"].([]map[string]any)...)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("install upsert rows = %d, want 2", len(rows))
	}
	versions := map[string]any{}
	for _, row := range rows {
		props := row["props"].(map[string]any)
		if props["manager"] != "apt-get" || props["stage"] != "builder" || props["category"] != "package" {
			t.Fatalf("install props = %#v", props)
		}
		versions[row["entity_id"].(string)] = props["version"]
	}
	if versions["install-1"] != nil || versions["install-2"] != "8.2" {
		t.Fatalf("version clear/pin props = %#v", versions)
	}
	foundRetract := false
	for _, stmt := range writer.buildEntityRetractStatements(mat) {
		if strings.Contains(stmt.Cypher, "DockerfileInstall") && strings.Contains(stmt.Cypher, "DETACH DELETE") {
			foundRetract = stmt.Drain && stmt.Parameters["repo_id"] == "repo-a" && stmt.Parameters["generation_id"] == "gen-2"
		}
	}
	if !foundRetract {
		t.Fatal("no generation-scoped DockerfileInstall retraction")
	}
}
