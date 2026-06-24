// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import "testing"

func TestMaterializeCarriesFunctionPackageImportPathMetadata(t *testing.T) {
	t.Parallel()

	got, err := Materialize(Input{
		RepoID: "repository:r_12345678",
		Files: []File{
			{
				Path: "handlers.go",
				Body: "package handlers\n\nfunc handle() {}\n",
				EntityBuckets: map[string][]Entity{
					"functions": {
						{
							Name:       "handle",
							LineNumber: 3,
							Metadata: map[string]any{
								"package_import_path": "example.com/repo/handlers",
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}
	if len(got.Entities) != 1 {
		t.Fatalf("len(Materialize().Entities) = %d, want 1", len(got.Entities))
	}
	if got, want := got.Entities[0].Metadata["package_import_path"], "example.com/repo/handlers"; got != want {
		t.Fatalf("package_import_path = %#v, want %#v", got, want)
	}
}
