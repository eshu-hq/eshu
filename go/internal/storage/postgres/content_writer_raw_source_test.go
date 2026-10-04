// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

func TestContentWriterPassesRawBodyAndDigestToSQL(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"  line\n", " \t\n ", ""} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			fake := &fakeExecQueryer{}
			writer := NewContentWriter(withTransactions(fake))
			_, err := writer.Write(context.Background(), content.Materialization{
				RepoID:  "repository:r_test",
				Records: []content.Record{{Path: "app.py", Body: body, Digest: "supplied-digest"}},
			})
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			for _, call := range fake.execs {
				if !strings.Contains(call.query, "INSERT INTO content_files") {
					continue
				}
				if got := call.args[3]; got != body {
					t.Fatalf("content SQL arg=%q, want %q", got, body)
				}
				if got := call.args[4]; got != "supplied-digest" {
					t.Fatalf("content_hash SQL arg=%q, want supplied digest", got)
				}
				return
			}
			t.Fatal("content_files insert not issued")
		})
	}
}

func TestContentWriterPreservesRawSourceCacheParameter(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"  function f() {}\n", "\n", " \t\n ", ""} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			fake := &fakeExecQueryer{}
			writer := NewContentWriter(withTransactions(fake))
			_, err := writer.Write(context.Background(), content.Materialization{
				RepoID: "repository:r_test",
				Entities: []content.EntityRecord{{
					EntityID: "entity-1", Path: "app.py", EntityType: "Function",
					EntityName: "f", StartLine: 1, EndLine: 1, SourceCache: source,
				}},
			})
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			for _, call := range fake.execs {
				if !strings.Contains(call.query, "INSERT INTO content_entities") {
					continue
				}
				if got := call.args[13]; got != source {
					t.Fatalf("source_cache SQL arg=%q, want %q", got, source)
				}
				return
			}
			t.Fatal("content_entities insert not issued")
		})
	}
}
