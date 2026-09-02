// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package groovy_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathGroovyJenkinsfile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Jenkinsfile")
	writeGroovyTestFile(
		t,
		filePath,
		`@Library('pipelines') _

pipelinePM2(
  use_configd: true,
  entry_point: 'dist/svc-notify.js',
  pre_deploy: { pipe, params ->
    sh 'echo migrate'
  }
)
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, true, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["lang"] != "groovy" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "groovy")
	}
	if got["is_dependency"] != true {
		t.Fatalf("is_dependency = %#v, want %#v", got["is_dependency"], true)
	}
	if got["path"] != filePath {
		t.Fatalf("path = %#v, want %#v", got["path"], filePath)
	}
	if got["source"] == "" {
		t.Fatal("expected source to be populated when IndexSource is enabled")
	}

	assertEmptyNamedBucket(t, got, "functions")
	assertEmptyNamedBucket(t, got, "classes")
	assertEmptyNamedBucket(t, got, "imports")
	parsertest.AssertBucketContainsFieldValue(t, got, "function_calls", "name", "pipelinePM2")
	assertEmptyNamedBucket(t, got, "variables")
	assertEmptyNamedBucket(t, got, "modules")
	assertEmptyNamedBucket(t, got, "module_inclusions")
	parsertest.AssertStringSliceContains(t, got, "shared_libraries", "pipelines")
	parsertest.AssertStringSliceContains(t, got, "pipeline_calls", "pipelinePM2")
	parsertest.AssertStringSliceContains(t, got, "entry_points", "dist/svc-notify.js")
	if got["use_configd"] != true {
		t.Fatalf("use_configd = %#v, want %#v", got["use_configd"], true)
	}
	if got["has_pre_deploy"] != true {
		t.Fatalf("has_pre_deploy = %#v, want %#v", got["has_pre_deploy"], true)
	}
	parsertest.AssertStringSliceContains(t, got, "shell_commands", "echo migrate")
	assertEmptyNamedBucket(t, got, "ansible_playbook_hints")
}

func TestDefaultEngineParsePathGroovyJenkinsfileAnsibleHints(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Jenkinsfile")
	writeGroovyTestFile(
		t,
		filePath,
		`@Library('pipelines') _
pipelineDeploy(entry_point: 'deploy.sh')
sh 'ansible-playbook deploy.yml -i inventory/dynamic_hosts.py --limit prod'
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, got, "pipeline_calls", "pipelineDeploy")
	parsertest.AssertStringSliceContains(t, got, "shell_commands", "ansible-playbook deploy.yml -i inventory/dynamic_hosts.py --limit prod")
	parsertest.AssertBucketContainsFieldValue(t, got, "ansible_playbook_hints", "playbook", "deploy.yml")
}

func TestDefaultEngineParsePathGroovyJenkinsfileLibraryStep(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Jenkinsfile")
	writeGroovyTestFile(
		t,
		filePath,
		`def libs = []
library identifier: 'pipelines@v2'
library('shared-controllers@main')
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, got, "shared_libraries", "pipelines")
	parsertest.AssertStringSliceContains(t, got, "shared_libraries", "shared-controllers")

	prescanned, err := engine.PreScanPaths([]string{filePath})
	if err != nil {
		t.Fatalf("PreScanPaths() error = %v, want nil", err)
	}
	parsertest.AssertPrescanContains(t, prescanned, "pipelines", filePath)
	parsertest.AssertPrescanContains(t, prescanned, "shared-controllers", filePath)
}

func TestDefaultEnginePreScanPathsGroovyJenkinsfile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Jenkinsfile")
	writeGroovyTestFile(
		t,
		filePath,
		`@Library('pipelines') _
pipelineDeploy(entry_point: 'deploy.sh')
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanPaths([]string{filePath})
	if err != nil {
		t.Fatalf("PreScanPaths() error = %v, want nil", err)
	}
	parsertest.AssertPrescanContains(t, got, "pipelines", filePath)
	parsertest.AssertPrescanContains(t, got, "pipelineDeploy", filePath)
	parsertest.AssertPrescanContains(t, got, "deploy.sh", filePath)
}

func TestDefaultEngineParsePathGroovySuppressesIgnoredCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Jenkinsfile")
	writeGroovyTestFile(
		t,
		filePath,
		`@Library('pipelines') _
pipelineDeploy(entry_point: 'deploy.sh')

pipeline {
  agent any
  parameters {}
  environment {}
  options {}
  stages {
    stage('Build') {
      when { branch 'main' }
      steps {
        script {
          if (true) { return 0 }
          for (x in []) { continue }
          while (false) { break }
          try {} catch (e) {}
          node('worker') {}
          new Object()
        }
      }
    }
  }
  post {}
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	ignoredCalls := []string{
		"pipeline", "agent", "parameters", "environment", "options",
		"stages", "stage", "when", "steps", "script", "post",
		"if", "for", "while", "catch", "return", "new",
	}

	calls, _ := got["function_calls"].([]map[string]any)
	for _, call := range calls {
		name, _ := call["name"].(string)
		for _, ignored := range ignoredCalls {
			if name == ignored {
				t.Fatalf("function_calls contains ignored keyword %q", ignored)
			}
		}
	}

	parsertest.AssertBucketContainsFieldValue(t, got, "function_calls", "name", "pipelineDeploy")
}

func TestDefaultEngineParsePathGroovyCyclomaticComplexity(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "BranchHelper.groovy")
	writeGroovyTestFile(
		t,
		filePath,
		`class BranchHelper {
  int complexMethod(int x) {
    if (x > 0 && x < 10) {
      for (int i = 0; i < x; i++) {
        while (x > 0) {
          switch (x) {
            case 1: break
            default: break
          }
          try {
            return x > 0 ? 1 : 0
          } catch (Exception e) {
            throw e
          }
        }
      }
    }
    return 0
  }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	item := parsertest.AssertBucketItemByName(t, got, "functions", "complexMethod")
	complexity, _ := item["cyclomatic_complexity"].(int)
	// base 1 + if 1 + && 1 + for 1 + while 1 + switch case 1 +
	// ternary 1 + catch 1 = 8. The switch default is excluded.
	if complexity != 8 {
		t.Fatalf("cyclomatic_complexity = %d, want 8", complexity)
	}
}
