// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

func TestContentReferencesForDockerfileInstalls(t *testing.T) {
	rows := []preparedFileRow{{repoID: "repo-a", path: "Dockerfile", body: "FROM alpine\nRUN apk add wrong\n", artifactType: "dockerfile", installNames: []string{"Curl", "curl"}}}
	got := contentReferencesForFiles(rows)
	count := 0
	for _, row := range got {
		if row.kind != "dockerfile_install" {
			continue
		}
		count++
		if row.repoID != "repo-a" || row.path != "Dockerfile" || row.value != "curl" {
			t.Fatalf("reference = %#v", row)
		}
	}
	if count != 1 {
		t.Fatalf("install references = %d, want one deduplicated row", count)
	}
}

func TestDockerfileInstallReferencesReplaceAndDeleteWithContent(t *testing.T) {
	db := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(db))
	write := func(generation string, record content.Record) []fakeExecCall {
		t.Helper()
		start := len(db.execs)
		_, err := writer.Write(context.Background(), content.Materialization{
			RepoID: "repo-a", ScopeID: "scope-a", GenerationID: generation,
			Records: []content.Record{record},
		})
		if err != nil {
			t.Fatal(err)
		}
		return db.execs[start:]
	}
	first := write("g1", content.Record{Path: "Dockerfile", Body: "FROM alpine\nRUN apk add curl\n", Metadata: map[string]string{"artifact_type": "dockerfile"}, InstallNames: []string{"curl"}})
	if !execsContainReferenceValue(first, "curl") {
		t.Fatalf("initial reference not written: %#v", first)
	}
	second := write("g2", content.Record{Path: "Dockerfile", Body: "FROM alpine\nRUN apk add wget\n", Metadata: map[string]string{"artifact_type": "dockerfile"}, InstallNames: []string{"wget"}})
	if len(second) < 3 || !strings.Contains(second[0].query, "DELETE FROM content_file_references") || !execsContainReferenceValue(second, "wget") || execsContainReferenceValue(second, "curl") {
		t.Fatalf("replacement did not delete old references before new upsert: %#v", second)
	}
	third := write("g3", content.Record{Path: "Dockerfile", Deleted: true})
	foundDelete := false
	for _, call := range third {
		if strings.Contains(call.query, "DELETE FROM content_file_references") && fakeExecArgsContain(call.args, "Dockerfile") {
			foundDelete = true
		}
	}
	if !foundDelete || execsContainReferenceValue(third, "wget") {
		t.Fatalf("tombstone did not remove references: %#v", third)
	}
}

func execsContainReferenceValue(calls []fakeExecCall, value string) bool {
	for _, call := range calls {
		if strings.Contains(call.query, "INSERT INTO content_file_references") && fakeExecArgsContain(call.args, value) {
			return true
		}
	}
	return false
}
