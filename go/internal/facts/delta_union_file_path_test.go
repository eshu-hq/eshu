// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

// TestDeltaScopeFilePathCells pins the path the #8750 destructive-successor
// guard matches against the touched set. It must be the path OverlayDeltaBase
// uses, so every cell states the qualified path or "" for a row the overlay
// never attributes to a file.
func TestDeltaScopeFilePathCells(t *testing.T) {
	t.Parallel()

	scope := DeltaScope{IsDelta: true, RepoPath: "/repos/org"}
	cells := []struct {
		name  string
		scope DeltaScope
		env   Envelope
		want  string
	}{
		{"file kind qualifies the relative path", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "a/b.go"}}, "/repos/org/a/b.go"},
		{"legacy fileFact kind is a file", scope, Envelope{FactKind: "fileFact", Payload: map[string]any{"relative_path": "a/b.go"}}, "/repos/org/a/b.go"},
		{"content_entity attributes to its file", scope, Envelope{FactKind: "content_entity", Payload: map[string]any{"relative_path": "a/b.go"}}, "/repos/org/a/b.go"},
		{"parsed_file_data attributes to its file", scope, Envelope{FactKind: "parsed_file_data", Payload: map[string]any{"relative_path": "a/b.go"}}, "/repos/org/a/b.go"},
		{"content reads content_path", scope, Envelope{FactKind: "content", Payload: map[string]any{"content_path": "a/b.go"}}, "/repos/org/a/b.go"},
		{"content ignores relative_path", scope, Envelope{FactKind: "content", Payload: map[string]any{"relative_path": "a/b.go"}}, ""},
		{"file ignores content_path", scope, Envelope{FactKind: "file", Payload: map[string]any{"content_path": "a/b.go"}}, ""},
		{"repository kind is not file-attributable", scope, Envelope{FactKind: "repository", Payload: map[string]any{"relative_path": "a/b.go"}}, ""},
		{"unknown kind is not file-attributable", scope, Envelope{FactKind: "reducer_eshu_search_document", Payload: map[string]any{"relative_path": "a/b.go"}}, ""},
		{"empty kind is not file-attributable", scope, Envelope{Payload: map[string]any{"relative_path": "a/b.go"}}, ""},
		{"empty path", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": ""}}, ""},
		{"whitespace path", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "  \t"}}, ""},
		{"missing path", scope, Envelope{FactKind: "file", Payload: map[string]any{}}, ""},
		{"nil payload", scope, Envelope{FactKind: "file"}, ""},
		{"non-string path", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": 7}}, ""},
		{"dot-slash prefix is cleaned", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "./a/b.go"}}, "/repos/org/a/b.go"},
		{"duplicate slashes are cleaned", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "a//b.go"}}, "/repos/org/a/b.go"},
		{"trailing slash is cleaned", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "a/b/"}}, "/repos/org/a/b"},
		{"surrounding spaces are trimmed", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "  a/b.go "}}, "/repos/org/a/b.go"},
		{"interior spaces and unicode survive", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "docs/naïve plan.md"}}, "/repos/org/docs/naïve plan.md"},
		{"parent escape is out of repo", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "../escape.go"}}, ""},
		{"bare parent is out of repo", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": ".."}}, ""},
		{"interior escape that leaves the repo", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "a/../../escape.go"}}, ""},
		{"interior dot-dot that stays inside is cleaned", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "a/../b.go"}}, "/repos/org/b.go"},
		{"absolute path is out of repo", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "/etc/passwd"}}, ""},
		{"bare dot is the repo root, not a file", scope, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "."}}, ""},
		{"empty repo path leaves the cleaned relative path", DeltaScope{IsDelta: true}, Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "./a/b.go"}}, "a/b.go"},
		{"a tombstone still resolves, as the overlay drops tombstoned base paths", scope, Envelope{FactKind: "file", IsTombstone: true, Payload: map[string]any{"relative_path": "a/b.go"}}, "/repos/org/a/b.go"},
	}
	for _, cell := range cells {
		t.Run(cell.name, func(t *testing.T) {
			t.Parallel()
			if got := cell.scope.FilePath(cell.env); got != cell.want {
				t.Errorf("FilePath() = %q, want %q", got, cell.want)
			}
		})
	}
}

// TestLiveFileRelativePathCells pins which rows the guard counts as a delta's
// live files: a live file fact with a non-blank string path, returned verbatim
// for DeltaScope.FilePath to qualify.
func TestLiveFileRelativePathCells(t *testing.T) {
	t.Parallel()

	cells := []struct {
		name   string
		env    Envelope
		want   string
		wantOK bool
	}{
		{"live file", Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "a/b.go"}}, "a/b.go", true},
		{"legacy fileFact kind", Envelope{FactKind: "fileFact", Payload: map[string]any{"relative_path": "a/b.go"}}, "a/b.go", true},
		{"path is returned verbatim, not cleaned", Envelope{FactKind: "file", Payload: map[string]any{"relative_path": " ./a/b.go "}}, " ./a/b.go ", true},
		{"out-of-repo path is still a live file row (FilePath drops it later)", Envelope{FactKind: "file", Payload: map[string]any{"relative_path": "../x.go"}}, "../x.go", true},
		{"tombstone", Envelope{FactKind: "file", IsTombstone: true, Payload: map[string]any{"relative_path": "a/b.go"}}, "", false},
		{"content_entity is not a file row", Envelope{FactKind: "content_entity", Payload: map[string]any{"relative_path": "a/b.go"}}, "", false},
		{"content is not a file row", Envelope{FactKind: "content", Payload: map[string]any{"content_path": "a/b.go"}}, "", false},
		{"repository is not a file row", Envelope{FactKind: "repository", Payload: map[string]any{"relative_path": "a/b.go"}}, "", false},
		{"empty kind", Envelope{Payload: map[string]any{"relative_path": "a/b.go"}}, "", false},
		{"empty path", Envelope{FactKind: "file", Payload: map[string]any{"relative_path": ""}}, "", false},
		{"whitespace path", Envelope{FactKind: "file", Payload: map[string]any{"relative_path": " \t"}}, "", false},
		{"missing path", Envelope{FactKind: "file", Payload: map[string]any{}}, "", false},
		{"nil payload", Envelope{FactKind: "file"}, "", false},
		{"non-string path", Envelope{FactKind: "file", Payload: map[string]any{"relative_path": []string{"a"}}}, "", false},
	}
	for _, cell := range cells {
		t.Run(cell.name, func(t *testing.T) {
			t.Parallel()
			got, ok := LiveFileRelativePath(cell.env)
			if ok != cell.wantOK || got != cell.want {
				t.Errorf("LiveFileRelativePath() = (%q, %v), want (%q, %v)", got, ok, cell.want, cell.wantOK)
			}
		})
	}
}
