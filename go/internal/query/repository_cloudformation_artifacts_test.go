package query

import "testing"

func TestBuildRepositoryCloudFormationRuntimeArtifactsSurfacesServerlessFunction(t *testing.T) {
	t.Parallel()

	got := buildRepositoryCloudFormationRuntimeArtifacts([]map[string]any{
		{
			"type":          "CloudFormationResource",
			"name":          "ProcessRecords",
			"file_path":     "infra/template.yml",
			"resource_type": "AWS::Serverless::Function",
		},
		{
			"type":          "CloudFormationResource",
			"name":          "LogBucket",
			"file_path":     "infra/template.yml",
			"resource_type": "AWS::S3::Bucket",
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryCloudFormationRuntimeArtifacts() = nil, want deployment artifacts")
	}

	artifacts, ok := got["deployment_artifacts"].([]map[string]any)
	if !ok {
		t.Fatalf("deployment_artifacts type = %T, want []map[string]any", got["deployment_artifacts"])
	}
	if len(artifacts) != 1 {
		t.Fatalf("len(deployment_artifacts) = %d, want 1", len(artifacts))
	}

	row := artifacts[0]
	if got, want := row["relative_path"], "infra/template.yml"; got != want {
		t.Fatalf("relative_path = %#v, want %#v", got, want)
	}
	if got, want := row["artifact_type"], "cloudformation_serverless"; got != want {
		t.Fatalf("artifact_type = %#v, want %#v", got, want)
	}
	if got, want := row["artifact_name"], "ProcessRecords"; got != want {
		t.Fatalf("artifact_name = %#v, want %#v", got, want)
	}
	if got, want := row["resource_type"], "AWS::Serverless::Function"; got != want {
		t.Fatalf("resource_type = %#v, want %#v", got, want)
	}
	if got, want := row["signals"], []string{"template_file", "serverless_transform"}; !stringSliceEqual(got, want) {
		t.Fatalf("signals = %#v, want %#v", got, want)
	}
}

func TestLoadDeploymentArtifactOverviewAddsCloudFormationDeliveryPath(t *testing.T) {
	t.Parallel()

	overview, err := loadDeploymentArtifactOverview(
		t.Context(),
		nil,
		nil,
		"repo-serverless",
		"serverless-job",
		nil,
		[]map[string]any{
			{
				"type":          "CloudFormationResource",
				"name":          "ProcessRecords",
				"file_path":     "infra/template.yml",
				"resource_type": "AWS::Serverless::Function",
			},
		},
		map[string]any{"families": []string{"cloudformation"}},
	)
	if err != nil {
		t.Fatalf("loadDeploymentArtifactOverview() error = %v, want nil", err)
	}

	deployment := BuildRepositoryDeploymentOverview(
		[]string{"serverless-job"},
		nil,
		[]string{"cloudformation"},
		overview,
	)
	families, ok := deployment["delivery_family_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_family_paths type = %T, want []map[string]any", deployment["delivery_family_paths"])
	}
	row := requireRepositoryStoryDeliveryFamily(families, "cloudformation")
	if row == nil {
		t.Fatalf("delivery_family_paths = %#v, want cloudformation family", families)
	}
	if got, want := row["mode"], "serverless_delivery"; got != want {
		t.Fatalf("mode = %#v, want %#v", got, want)
	}
	if got, want := row["path"], "infra/template.yml"; got != want {
		t.Fatalf("path = %#v, want %#v", got, want)
	}
}
