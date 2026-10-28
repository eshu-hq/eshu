// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestPHPSourceAcceptanceThroughNativeSnapshot proves each PHP source shape
// survives native parse, private handoff, and the streamed fact emitter.
func TestPHPSourceAcceptanceThroughNativeSnapshot(t *testing.T) {
	const secret = "0123456789abcdef-secret"
	sources := []struct {
		path, kind, prefix, suffix string
	}{
		{"composer.json", "composer_json_require_php", "{\n  \"require\": {\n    \"php\": \"", "\"\n  }\n}"},
		{"composer.json", "composer_json_platform_php", "{\n  \"config\": {\"platform\": {\n    \"php\": \"", "\"\n  }}\n}"},
		{"composer.lock", "composer_lock_platform_php", "{\n  \"platform\": {\n    \"php\": \"", "\"\n  }\n}"},
		{"composer.lock", "composer_lock_platform_overrides_php", "{\n  \"platform-overrides\": {\n    \"php\": \"", "\"\n  }\n}"},
		{".php-version", "php_version", "", "\n"},
	}
	specs := []struct{ raw, kind string }{
		{"8.3.12", "exact"}, {"^8.2", "range"}, {"latest", "alias"}, {"${PHP_VERSION}", "unresolved"},
	}
	for _, source := range sources {
		t.Run(source.kind, func(t *testing.T) {
			collect := func(body string) []string {
				t.Helper()
				repoRoot := t.TempDir()
				writeCollectorTestFile(t, filepath.Join(repoRoot, source.path), body)
				observed := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
				snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).
					SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
				if err != nil {
					t.Fatal(err)
				}
				collected := buildStreamingGeneration(repoRoot, testCollectorRepositoryMetadata(repoRoot), "php-acceptance", observed, snapshot, false, "")
				rows := decodeDeclarationList(t, drainFactChannel(collected.Facts))
				got := make([]string, 0, len(rows))
				for _, row := range rows {
					got = append(got, row.Runtime+"|"+row.SourceKind+"|"+row.RawSpec+"|"+row.SpecKind)
				}
				return got
			}
			for _, spec := range specs {
				t.Run(spec.kind, func(t *testing.T) {
					got := collect(source.prefix + spec.raw + source.suffix)
					want := "php|" + source.kind + "|" + spec.raw + "|" + spec.kind
					if len(got) != 1 || got[0] != want {
						t.Fatalf("declarations = %v, want [%s]", got, want)
					}
				})
			}
			for _, tc := range []struct{ name, body string }{
				{"credential", source.prefix + secret + source.suffix},
				{"malformed", source.prefix + "8.2" + source.suffix + " trailing"},
			} {
				if source.kind == "php_version" && tc.name == "malformed" {
					tc.body = "\xff\n"
				}
				t.Run(tc.name, func(t *testing.T) {
					if got := collect(tc.body); len(got) != 0 {
						t.Fatalf("rejected PHP source emitted %v", got)
					}
				})
			}
		})
	}
}
