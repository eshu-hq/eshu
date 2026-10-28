// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

func TestSearchDockerfileInstallEntitiesUsesExactIndexedNameAndFullCursor(t *testing.T) {
	sqlDB := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns:       []string{"entity_id", "repo_id", "relative_path", "entity_type", "entity_name", "start_line", "end_line", "metadata"},
		rows:          [][]driver.Value{{"id-1", "repo-a", "Dockerfile", "DockerfileInstall", "curl", int64(2), int64(2), []byte(`{"manager":"apk","install_ordinal":"1"}`)}},
		queryContains: []string{"FROM content_entities", "entity_type = 'DockerfileInstall'", "entity_name = $1", "repo_id = ANY($7)", "ORDER BY repo_id, relative_path, start_line, entity_id", "LIMIT $6"},
	}})
	got, err := NewContentReader(sqlDB).SearchDockerfileInstallEntities(context.Background(), "curl", []string{"repo-a"}, "repo-a", "Dockerfile", 1, "id-0", 11)
	if err != nil || len(got) != 1 || got[0].EntityID != "id-1" || got[0].Metadata["manager"] != "apk" {
		t.Fatalf("SearchDockerfileInstallEntities() = (%#v, %v)", got, err)
	}
}
