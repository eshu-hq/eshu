package query

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCorrelationDSLFixtureComposeRepoSurfacesRuntimeArtifacts(t *testing.T) {
	t.Parallel()

	got := buildRepositoryRuntimeArtifacts([]FileContent{
		{
			RelativePath: "docker-compose.yaml",
			ArtifactType: "docker_compose",
			Content:      readCorrelationDSLFixture(t, "service-compose", "docker-compose.yaml"),
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryRuntimeArtifacts() = nil, want deployment artifacts")
	}

	artifacts, ok := got["deployment_artifacts"].([]map[string]any)
	if !ok {
		t.Fatalf("deployment_artifacts type = %T, want []map[string]any", got["deployment_artifacts"])
	}
	if len(artifacts) != 2 {
		t.Fatalf("len(deployment_artifacts) = %d, want 2", len(artifacts))
	}

	api := artifacts[0]
	if got, want := api["artifact_type"], "docker_compose"; got != want {
		t.Fatalf("api.artifact_type = %#v, want %#v", got, want)
	}
	if got, want := api["service_name"], "api"; got != want {
		t.Fatalf("api.service_name = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(api, "signals"), []string{"build", "ports", "environment"}; !stringSliceEqual(got, want) {
		t.Fatalf("api.signals = %#v, want %#v", got, want)
	}
	if got, want := api["build_context"], "."; got != want {
		t.Fatalf("api.build_context = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(api, "ports"), []string{"8080:8080"}; !stringSliceEqual(got, want) {
		t.Fatalf("api.ports = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(api, "environment"), []string{"APP_ENV", "PORT"}; !stringSliceEqual(got, want) {
		t.Fatalf("api.environment = %#v, want %#v", got, want)
	}
	database := artifacts[1]
	if got, want := database["service_name"], "database"; got != want {
		t.Fatalf("database.service_name = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(database, "signals"), []string{"ports"}; !stringSliceEqual(got, want) {
		t.Fatalf("database.signals = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(database, "ports"), []string{"5432:5432"}; !stringSliceEqual(got, want) {
		t.Fatalf("database.ports = %#v, want %#v", got, want)
	}
}

func TestCorrelationDSLFixtureJenkinsAnsibleRepoSurfacesControllerAndAnsibleSignals(t *testing.T) {
	t.Parallel()

	files := []FileContent{
		{
			RelativePath: "Jenkinsfile",
			Content:      readCorrelationDSLFixture(t, "service-jenkins-ansible", "Jenkinsfile"),
		},
		{RelativePath: "inventory/prod.ini"},
		{RelativePath: "group_vars/all.yml"},
		{RelativePath: "host_vars/web-prod.yml"},
		{RelativePath: "roles/service_deploy/tasks/main.yml"},
	}

	got := buildRepositoryControllerArtifacts("service-jenkins-ansible", files)
	if got == nil {
		t.Fatal("buildRepositoryControllerArtifacts() = nil, want controller_artifacts")
	}

	artifacts := mapSliceValue(got, "controller_artifacts")
	if len(artifacts) != 1 {
		t.Fatalf("len(controller_artifacts) = %d, want 1", len(artifacts))
	}

	row := artifacts[0]
	if got, want := row["controller_kind"], "jenkins_pipeline"; got != want {
		t.Fatalf("controller_artifacts[0].controller_kind = %#v, want %#v", got, want)
	}
	if got, want := row["path"], "Jenkinsfile"; got != want {
		t.Fatalf("controller_artifacts[0].path = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(row, "pipeline_calls"), []string{"pipelineDeploy"}; !stringSliceEqual(got, want) {
		t.Fatalf("controller_artifacts[0].pipeline_calls = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(row, "ansible_inventories"), []string{"inventory/prod.ini"}; !stringSliceEqual(got, want) {
		t.Fatalf("controller_artifacts[0].ansible_inventories = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(row, "ansible_var_files"), []string{"group_vars/all.yml", "host_vars/web-prod.yml"}; !stringSliceEqual(got, want) {
		t.Fatalf("controller_artifacts[0].ansible_var_files = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(row, "ansible_task_entrypoints"), []string{"roles/service_deploy/tasks/main.yml"}; !stringSliceEqual(got, want) {
		t.Fatalf("controller_artifacts[0].ansible_task_entrypoints = %#v, want %#v", got, want)
	}

	hints := mapSliceValue(row, "ansible_playbook_hints")
	if len(hints) != 1 {
		t.Fatalf("controller_artifacts[0].ansible_playbook_hints = %#v, want one row", row["ansible_playbook_hints"])
	}
	if got, want := hints[0]["playbook"], "playbooks/deploy.yml"; got != want {
		t.Fatalf("ansible_playbook_hints[0].playbook = %#v, want %#v", got, want)
	}

	overview := buildRepositoryInfrastructureOverview(nil, []FileContent{
		{ArtifactType: "ansible_playbook"},
		{ArtifactType: "ansible_inventory"},
		{ArtifactType: "ansible_vars"},
		{ArtifactType: "ansible_vars"},
		{ArtifactType: "ansible_role"},
		{ArtifactType: "ansible_task_entrypoint"},
	})
	if overview == nil {
		t.Fatal("buildRepositoryInfrastructureOverview() = nil, want artifact counts")
	}

	artifactCounts, ok := overview["artifact_family_counts"].(map[string]int)
	if !ok {
		t.Fatalf("artifact_family_counts type = %T, want map[string]int", overview["artifact_family_counts"])
	}
	if got, want := artifactCounts["ansible"], 6; got != want {
		t.Fatalf("artifact_family_counts[ansible] = %d, want %d", got, want)
	}
}

func readCorrelationDSLFixture(t *testing.T, parts ...string) string {
	t.Helper()

	path := correlationDSLFixturePath(parts...)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", path, err)
	}
	return string(body)
}

func correlationDSLFixturePath(parts ...string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	allParts := append(
		[]string{
			filepath.Dir(file),
			"..",
			"..",
			"..",
			"tests",
			"fixtures",
			"correlation_dsl",
		},
		parts...,
	)
	return filepath.Join(allParts...)
}
