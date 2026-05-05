package query

import "testing"

func TestBuildRepositoryConfigArtifactsExtractsLookupBackedTemplateAssetsWithTrimspaceWrapper(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terraform-modules-aws", []FileContent{
		{
			RelativePath: "modules/autoscaling/main.tf",
			Content: `locals {
  userdata_template = lookup(
    var.configuration,
    "userdata_template",
    "${path.module}/templates/user_data.tpl",
  )
  dashboard_template = lookup(
    var.configuration,
    "dashboard_template",
    "${path.module}/templates/cloudwatch-dashboard.tpl",
  )

  userdata = trimspace(templatefile(local.userdata_template, {
    userdata = var.user_data
  }))
  cloudwatch_dashboard = templatefile(local.dashboard_template, {
    name = var.name
  })
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

	wantPaths := []string{"templates/cloudwatch-dashboard.tpl", "templates/user_data.tpl"}
	for index, want := range wantPaths {
		if got, ok := configPaths[index]["path"].(string); !ok || got != want {
			t.Fatalf("config_paths[%d].path = %#v, want %#v", index, configPaths[index]["path"], want)
		}
		if got, wantRepo := configPaths[index]["source_repo"], "terraform-modules-aws"; got != wantRepo {
			t.Fatalf("config_paths[%d].source_repo = %#v, want %#v", index, got, wantRepo)
		}
		if got, wantRelative := configPaths[index]["relative_path"], "modules/autoscaling/main.tf"; got != wantRelative {
			t.Fatalf("config_paths[%d].relative_path = %#v, want %#v", index, got, wantRelative)
		}
		if got, wantKind := configPaths[index]["evidence_kind"], "local_config_asset"; got != wantKind {
			t.Fatalf("config_paths[%d].evidence_kind = %#v, want %#v", index, got, wantKind)
		}
	}
}

func TestBuildRepositoryConfigArtifactsExtractsLegacyInterpolationWrappedLookupAndFile(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terraform-legacy-modules", []FileContent{
		{
			RelativePath: "modules/ecs/main.tf",
			Content: `locals {
  task_template = "${lookup(var.configuration, "template", "${path.module}/templates/ecs/container.tpl")}"
}

data "template_file" "task" {
  template = "${file(local.task_template)}"
}
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 1 {
		t.Fatalf("len(config_paths) = %d, want 1", len(configPaths))
	}
	if got, want := configPaths[0]["path"], "templates/ecs/container.tpl"; got != want {
		t.Fatalf("config_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["evidence_kind"], "local_config_asset"; got != want {
		t.Fatalf("config_paths[0].evidence_kind = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryConfigArtifactsExtractsLegacyInterpolationWrappedLookupAndTemplatefile(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terraform-legacy-modules", []FileContent{
		{
			RelativePath: "modules/ec2/main.tf",
			Content: `locals {
  template = "${lookup(var.configuration, "template_file", "${path.module}/templates/user_data.tpl")}"
}

resource "example_instance" "this" {
  user_data = templatefile(local.template, {
    userdata = var.user_data
  })
}
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 1 {
		t.Fatalf("len(config_paths) = %d, want 1", len(configPaths))
	}
	if got, want := configPaths[0]["path"], "templates/user_data.tpl"; got != want {
		t.Fatalf("config_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["evidence_kind"], "local_config_asset"; got != want {
		t.Fatalf("config_paths[0].evidence_kind = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryConfigArtifactsExtractsDirectLookupWrappedByFile(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terraform-legacy-modules", []FileContent{
		{
			RelativePath: "modules/batch/main.tf",
			Content: `data "template_file" "job" {
  template = "${file(lookup(var.configuration, "template", "${path.module}/batch/container.tpl"))}"
}
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 1 {
		t.Fatalf("len(config_paths) = %d, want 1", len(configPaths))
	}
	if got, want := configPaths[0]["path"], "batch/container.tpl"; got != want {
		t.Fatalf("config_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["evidence_kind"], "local_config_asset"; got != want {
		t.Fatalf("config_paths[0].evidence_kind = %#v, want %#v", got, want)
	}
}
