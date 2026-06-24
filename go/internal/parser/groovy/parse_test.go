// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package groovy

import (
	"os"
	"path/filepath"
	"testing"

	tree_sitter_groovy "github.com/dekobon/tree-sitter-groovy/bindings/go"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestParseBuildsGroovyPayload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Jenkinsfile")
	source := `@Library('pipelines') _
pipelineDeploy(entry_point: 'deploy.sh')
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	got, err := Parse(path, true, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	if got["path"] != path || got["lang"] != "groovy" || got["is_dependency"] != true {
		t.Fatalf("payload identity = %#v, want path/lang/dependency", got)
	}
	assertEmptyNamedBucket(t, got, "functions")
	assertEmptyNamedBucket(t, got, "classes")
	assertEmptyNamedBucket(t, got, "imports")
	assertBucketItemByName(t, got, "function_calls", "pipelineDeploy")
	assertEmptyNamedBucket(t, got, "variables")
	assertEmptyNamedBucket(t, got, "modules")
	assertEmptyNamedBucket(t, got, "module_inclusions")
	assertStringSliceContains(t, got["shared_libraries"].([]string), "pipelines")
	assertStringSliceContains(t, got["pipeline_calls"].([]string), "pipelineDeploy")
	assertStringSliceContains(t, got["entry_points"].([]string), "deploy.sh")
	if got["source"] != source {
		t.Fatalf("source = %#v, want original source", got["source"])
	}
}

func TestParseExtractsGroovyClassesFunctionsAndCalls(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "src", "DeployHelper.groovy")
	source := `package org.example

class DeployHelper {
  def deployApp(String target) {
    pipelineDeploy(target)
    renderTarget(target)
  }

  private String renderTarget(String target) {
    return "deploy-${target}"
  }
}

def topLevelHelper() {
  new DeployHelper().deployApp('prod')
}
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	got, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	class := assertBucketItemByName(t, got, "classes", "DeployHelper")
	if class["line_number"] != 3 {
		t.Fatalf("DeployHelper line_number = %#v, want 3", class["line_number"])
	}
	deploy := assertBucketItemByName(t, got, "functions", "deployApp")
	if deploy["class_context"] != "DeployHelper" {
		t.Fatalf("deployApp class_context = %#v, want DeployHelper", deploy["class_context"])
	}
	assertBucketItemByName(t, got, "functions", "renderTarget")
	assertBucketItemByName(t, got, "functions", "topLevelHelper")
	assertBucketItemByName(t, got, "function_calls", "pipelineDeploy")
	assertBucketItemByName(t, got, "function_calls", "renderTarget")
	assertBucketItemByName(t, got, "function_calls", "deployApp")
}

func TestParseWithParserExtractsGroovyClassesFunctionsAndCalls(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "src", "DeployHelper.groovy")
	source := `package org.example

class DeployHelper {
  def deployApp(String target) {
    pipelineDeploy(target)
    renderTarget(target)
  }

  private String renderTarget(String target) {
    return "deploy-${target}"
  }
}

def topLevelHelper() {
  new DeployHelper().deployApp('prod')
}
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_groovy.Language())); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage(groovy) error = %v, want nil", err)
	}
	defer parser.Close()

	got, err := ParseWithParser(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("ParseWithParser() error = %v, want nil", err)
	}

	assertBucketItemByName(t, got, "classes", "DeployHelper")
	deploy := assertBucketItemByName(t, got, "functions", "deployApp")
	if deploy["class_context"] != "DeployHelper" {
		t.Fatalf("deployApp class_context = %#v, want DeployHelper", deploy["class_context"])
	}
	assertBucketItemByName(t, got, "functions", "renderTarget")
	assertBucketItemByName(t, got, "functions", "topLevelHelper")
	assertBucketItemByName(t, got, "function_calls", "pipelineDeploy")
	assertBucketItemByName(t, got, "function_calls", "renderTarget")
	assertBucketItemByName(t, got, "function_calls", "deployApp")
}

func TestParseWithParserKeepsGroovyMethodInvocationCalls(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "DeployHelper.groovy")
	source := `class DeployHelper {
  def deployApp(String target) {
    renderTarget(target)
  }

  private String renderTarget(String target) {
    return "deploy-${target}"
  }
}

new DeployHelper().deployApp('prod')
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_groovy.Language())); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage(groovy) error = %v, want nil", err)
	}
	defer parser.Close()

	got, err := ParseWithParser(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("ParseWithParser() error = %v, want nil", err)
	}

	assertBucketItemByName(t, got, "function_calls", "renderTarget")
	assertBucketItemByName(t, got, "function_calls", "deployApp")
}

func TestParseWithParserAddsGroovyClassQualifiedCallMetadata(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "DeployPipeline.groovy")
	source := `class DeployPipeline {
  def call() {
    DeployHelper.deployApp('prod')
  }
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_groovy.Language())); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage(groovy) error = %v, want nil", err)
	}
	defer parser.Close()

	got, err := ParseWithParser(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("ParseWithParser() error = %v, want nil", err)
	}

	call := assertBucketItemByName(t, got, "function_calls", "deployApp")
	if call["full_name"] != "DeployHelper.deployApp" {
		t.Fatalf("full_name = %#v, want DeployHelper.deployApp", call["full_name"])
	}
	if call["inferred_obj_type"] != "DeployHelper" {
		t.Fatalf("inferred_obj_type = %#v, want DeployHelper", call["inferred_obj_type"])
	}
	if call["lang"] != "groovy" {
		t.Fatalf("lang = %#v, want groovy", call["lang"])
	}
}

func TestParseWithParserKeepsGroovyLowercaseReceiverCallsUnqualified(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "DeployPipeline.groovy")
	source := `class DeployPipeline {
  def call(service) {
    service.deployApp('prod')
  }
}
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_groovy.Language())); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage(groovy) error = %v, want nil", err)
	}
	defer parser.Close()

	got, err := ParseWithParser(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("ParseWithParser() error = %v, want nil", err)
	}

	call := assertBucketItemByName(t, got, "function_calls", "deployApp")
	if _, ok := call["full_name"]; ok {
		t.Fatalf("full_name = %#v, want absent for lowercase receiver", call["full_name"])
	}
	if _, ok := call["inferred_obj_type"]; ok {
		t.Fatalf("inferred_obj_type = %#v, want absent for lowercase receiver", call["inferred_obj_type"])
	}
	if call["lang"] != "groovy" {
		t.Fatalf("lang = %#v, want groovy", call["lang"])
	}
}

func TestParseMarksJenkinsfileAndSharedLibraryCallAsRoots(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	jenkinsfile := filepath.Join(root, "Jenkinsfile")
	if err := os.WriteFile(jenkinsfile, []byte(`pipeline {
  agent any
  stages {
    stage('Deploy') {
      steps {
        sh 'make deploy'
      }
    }
  }
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	got, err := Parse(jenkinsfile, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse(Jenkinsfile) error = %v, want nil", err)
	}
	entrypoint := assertBucketItemByName(t, got, "functions", "Jenkinsfile")
	assertStringSliceContains(t, entrypoint["dead_code_root_kinds"].([]string), "groovy.jenkins_pipeline_entrypoint")
	if entrypoint["framework"] != "jenkins" {
		t.Fatalf("Jenkinsfile framework = %#v, want jenkins", entrypoint["framework"])
	}

	sharedLibrary := filepath.Join(root, "vars", "deployService.groovy")
	if err := os.MkdirAll(filepath.Dir(sharedLibrary), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.WriteFile(sharedLibrary, []byte(`def call(Map config = [:]) {
  node {
    stage('Deploy') {
      pipelineDeploy(config)
    }
  }
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	got, err = Parse(sharedLibrary, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse(vars/deployService.groovy) error = %v, want nil", err)
	}
	call := assertBucketItemByName(t, got, "functions", "call")
	assertStringSliceContains(t, call["dead_code_root_kinds"].([]string), "groovy.shared_library_call")
	if call["framework"] != "jenkins" {
		t.Fatalf("call framework = %#v, want jenkins", call["framework"])
	}

	if !isSharedLibraryVarsFile("vars/deployService.groovy") {
		t.Fatalf("isSharedLibraryVarsFile(relative vars path) = false, want true")
	}
}

func TestPreScanReturnsSortedUniqueMetadataNames(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Jenkinsfile")
	source := `pipelineDeploy(entry_point: 'deploy.sh')
@Library('pipelines') _
pipelineDeploy(entry_point: 'deploy.sh')
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	got, err := PreScan(path)
	if err != nil {
		t.Fatalf("PreScan() error = %v, want nil", err)
	}

	want := []string{"deploy.sh", "pipelineDeploy", "pipelines"}
	if len(got) != len(want) {
		t.Fatalf("PreScan() len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i, item := range want {
		if got[i] != item {
			t.Fatalf("PreScan()[%d] = %q, want %q in %#v", i, got[i], item, got)
		}
	}
}

func assertBucketItemByName(t *testing.T, payload map[string]any, bucket string, name string) map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", bucket, payload[bucket])
	}
	return assertNamedItem(t, items, name)
}

func assertNamedItem(t *testing.T, items []map[string]any, name string) map[string]any {
	t.Helper()

	for _, item := range items {
		if item["name"] == name {
			return item
		}
	}
	t.Fatalf("items missing name %q in %#v", name, items)
	return nil
}

func assertEmptyNamedBucket(t *testing.T, payload map[string]any, key string) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	if len(items) != 0 {
		t.Fatalf("%s len = %d, want 0: %#v", key, len(items), items)
	}
}
