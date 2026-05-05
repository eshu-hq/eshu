package query

import (
	"context"
	"database/sql/driver"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadRepositoryControllerArtifactsFallsBackToGetFileContentForJenkinsfile(t *testing.T) {
	t.Parallel()

	fixtureContent := readAnsibleJenkinsAutomationFixture(t, "Jenkinsfile")
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-1", "Jenkinsfile", "abc123", fixtureContent,
					"hash-jenkins", int64(12), "groovy", "groovy",
				},
			},
		},
	})

	reader := NewContentReader(db)
	got, err := loadRepositoryControllerArtifacts(
		context.Background(),
		reader,
		"repo-1",
		"controller-service",
		[]FileContent{
			{RelativePath: "Jenkinsfile"},
			{RelativePath: "inventory/dynamic_hosts.py"},
			{RelativePath: "group_vars/all.yml"},
			{RelativePath: "host_vars/web-prod.yml"},
			{RelativePath: "roles/website_import/tasks/main.yml"},
		},
	)
	if err != nil {
		t.Fatalf("loadRepositoryControllerArtifacts() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("loadRepositoryControllerArtifacts() = nil, want controller_artifacts")
	}

	artifacts := mapSliceValue(got, "controller_artifacts")
	if len(artifacts) != 1 {
		t.Fatalf("len(controller_artifacts) = %d, want 1", len(artifacts))
	}

	row := artifacts[0]
	if got, want := row["path"], "Jenkinsfile"; got != want {
		t.Fatalf("controller_artifacts[0].path = %#v, want %#v", got, want)
	}
	if got, want := row["controller_kind"], "jenkins_pipeline"; got != want {
		t.Fatalf("controller_artifacts[0].controller_kind = %#v, want %#v", got, want)
	}

	pipelineCalls := StringSliceVal(row, "pipeline_calls")
	if len(pipelineCalls) != 1 || pipelineCalls[0] != "pipelineDeploy" {
		t.Fatalf("controller_artifacts[0].pipeline_calls = %#v, want [pipelineDeploy]", row["pipeline_calls"])
	}
	sharedLibraries := StringSliceVal(row, "shared_libraries")
	if len(sharedLibraries) != 1 || sharedLibraries[0] != "controller-pipelines" {
		t.Fatalf("controller_artifacts[0].shared_libraries = %#v, want [controller-pipelines]", row["shared_libraries"])
	}
	shellCommands := StringSliceVal(row, "shell_commands")
	if len(shellCommands) != 1 || shellCommands[0] != "./scripts/deploy.sh" {
		t.Fatalf("controller_artifacts[0].shell_commands = %#v, want [./scripts/deploy.sh]", row["shell_commands"])
	}
	inventories := StringSliceVal(row, "ansible_inventories")
	if len(inventories) != 1 || inventories[0] != "inventory/dynamic_hosts.py" {
		t.Fatalf("controller_artifacts[0].ansible_inventories = %#v, want [inventory/dynamic_hosts.py]", row["ansible_inventories"])
	}
	varFiles := StringSliceVal(row, "ansible_var_files")
	if len(varFiles) != 2 || varFiles[0] != "group_vars/all.yml" || varFiles[1] != "host_vars/web-prod.yml" {
		t.Fatalf("controller_artifacts[0].ansible_var_files = %#v, want [group_vars/all.yml host_vars/web-prod.yml]", row["ansible_var_files"])
	}
	taskEntrypoints := StringSliceVal(row, "ansible_task_entrypoints")
	if len(taskEntrypoints) != 1 || taskEntrypoints[0] != "roles/website_import/tasks/main.yml" {
		t.Fatalf("controller_artifacts[0].ansible_task_entrypoints = %#v, want [roles/website_import/tasks/main.yml]", row["ansible_task_entrypoints"])
	}
}

func readAnsibleJenkinsAutomationFixture(t *testing.T, parts ...string) string {
	t.Helper()

	path := ansibleJenkinsAutomationFixturePath(parts...)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", path, err)
	}
	return string(body)
}

func ansibleJenkinsAutomationFixturePath(parts ...string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "tests", "fixtures", "ecosystems", "ansible_jenkins_automation"))
	elems := append([]string{root}, parts...)
	return filepath.Join(elems...)
}
