// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/content/shape"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// ungovernedRegistry returns the default registry with every content-store
// policy reset to store, which is the shape of the registry before #8978 bound
// .npmrc to metadata. It is the baseline for the governed cost below.
func ungovernedRegistry(b *testing.B) parser.Registry {
	b.Helper()
	definitions := parser.DefaultRegistry().Definitions()
	for i := range definitions {
		definitions[i].ContentStore = parser.ContentStore{}
	}
	registry, err := parser.NewRegistry(definitions)
	if err != nil {
		b.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

// BenchmarkContentStoreUngoverned measures what the policy costs a registry
// that binds no non-store policy: one registry flag check per snapshot and one
// nil-map read per emitted file. The default registry is no longer such a
// registry (#8978 binds .npmrc), so the baseline is built from it.
func BenchmarkContentStoreUngoverned(b *testing.B) {
	registry := ungovernedRegistry(b)
	files := make([]shape.File, 5000)
	for i := range files {
		files[i] = shape.File{Path: "src/pkg/file.go", Body: strings.Repeat("x", 64)}
	}
	body := []byte(strings.Repeat("x", 4096))
	b.Run("snapshot_stage_5000_files", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if stores := applyContentStoreToShapeFiles(registry, "/repo", files, nil); stores != nil {
				b.Fatal("ungoverned registry returned a policy map")
			}
		}
	})
	b.Run("emit_stage_per_file", func(b *testing.B) {
		var tally contentStoreTally
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = tally.storedBytes(nil, "src/pkg/file.go", body)
		}
	})
}

// BenchmarkContentStoreGovernedDefault measures what the default registry now
// costs every repository: the registry is governed by the .npmrc metadata
// binding (#8978) and the tfvars and yarnrc bindings (#8833), so the snapshot
// stage and the re-read metas look up each file's policy even when no file is
// governed. The emit stage reads an empty (non-nil) map for such a repo.
func BenchmarkContentStoreGovernedDefault(b *testing.B) {
	registry := parser.DefaultRegistry()
	files := make([]shape.File, 5000)
	metas := make([]model.ContentFileMeta, 5000)
	for i := range files {
		files[i] = shape.File{Path: "src/pkg/file.go", Body: strings.Repeat("x", 64)}
		metas[i] = model.ContentFileMeta{RelativePath: "src/pkg/file.go"}
	}
	body := []byte(strings.Repeat("x", 4096))
	b.Run("snapshot_stage_5000_files_no_npmrc", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if stores := applyContentStoreToShapeFiles(registry, "/repo", files, nil); len(stores) != 0 {
				b.Fatal("a repository without an .npmrc must govern no file")
			}
		}
	})
	b.Run("metas_5000_files", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			snapshot := RepositorySnapshot{ContentFileMetas: metas}
			addContentStoresForMetas(registry, &snapshot)
			if snapshot.ContentStores != nil {
				b.Fatal("an ungoverned meta got a policy")
			}
		}
	})
	b.Run("emit_stage_per_file_empty_map", func(b *testing.B) {
		stores := map[string]parser.ContentStore{}
		var tally contentStoreTally
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = tally.storedBytes(stores, "src/pkg/file.go", body)
		}
	})
}
