package query

import "testing"

func TestBuildRepositoryConfigArtifactsExtractsTerragruntParentDirJoinedHelperPaths(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("iac-terragrunt-core", []FileContent{
		{
			RelativePath: "env/prod/terragrunt.hcl",
			Content: `locals {
  parent_runtime = templatefile(join("/", [get_parent_terragrunt_dir(), "templates/runtime.json"]), {})
  parent_global  = file(join("/", [get_parent_terragrunt_dir(), "global.yaml"]))
}
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 2 {
		t.Fatalf("len(config_paths) = %d, want 2", len(configPaths))
	}

	wantPaths := map[string]struct{}{
		"global.yaml":            {},
		"templates/runtime.json": {},
	}
	for _, row := range configPaths {
		path, _ := row["path"].(string)
		if _, ok := wantPaths[path]; !ok {
			t.Fatalf("unexpected config_paths row = %#v", row)
		}
		if got, want := row["source_repo"], "iac-terragrunt-core"; got != want {
			t.Fatalf("config_paths[%q].source_repo = %#v, want %#v", path, got, want)
		}
		if got, want := row["relative_path"], "env/prod/terragrunt.hcl"; got != want {
			t.Fatalf("config_paths[%q].relative_path = %#v, want %#v", path, got, want)
		}
		if got, want := row["evidence_kind"], "local_config_asset"; got != want {
			t.Fatalf("config_paths[%q].evidence_kind = %#v, want %#v", path, got, want)
		}
		delete(wantPaths, path)
	}
	if len(wantPaths) != 0 {
		t.Fatalf("missing config_paths rows for %#v", wantPaths)
	}
}
