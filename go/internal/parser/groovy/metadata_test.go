// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package groovy

import (
	"fmt"
	"strings"
	"testing"
)

// TestPipelineMetadataAnsibleGateAmongManyNonAnsibleCommands is a
// characterization test for the issue #4845 Ansible precondition gate in
// PipelineMetadata: a shell command containing "ansible-playbook" must still
// be found and produce the same AnsiblePlaybookHint regardless of how many
// other, non-matching shell commands surround it. This is exactly the shape
// the gate optimizes (many short non-Ansible sh calls in a shared-library
// Jenkins pipeline) and the shape a too-narrow gate would silently break.
func TestPipelineMetadataAnsibleGateAmongManyNonAnsibleCommands(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("def call() {\n")
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "  sh 'kubectl apply -f deploy/service-%d.yaml --namespace prod'\n", i)
	}
	b.WriteString("  sh 'ansible-playbook site.yml -i inventory/prod --limit web'\n")
	for i := 50; i < 100; i++ {
		fmt.Fprintf(&b, "  sh 'echo step-%d'\n", i)
	}
	b.WriteString("}\n")

	got := PipelineMetadata(b.String())

	if len(got.ShellCommands) != 101 {
		t.Fatalf("ShellCommands len = %d, want 101", len(got.ShellCommands))
	}
	if len(got.AnsiblePlaybookHints) != 1 {
		t.Fatalf("AnsiblePlaybookHints len = %d, want 1: %#v", len(got.AnsiblePlaybookHints), got.AnsiblePlaybookHints)
	}
	hint := got.AnsiblePlaybookHints[0]
	if hint.Playbook != "site.yml" {
		t.Fatalf("Playbook = %q, want site.yml", hint.Playbook)
	}
	if hint.Inventory != "inventory/prod" {
		t.Fatalf("Inventory = %#v, want inventory/prod", hint.Inventory)
	}
	if hint.Command != "ansible-playbook site.yml -i inventory/prod --limit web" {
		t.Fatalf("Command = %q, want the full ansible-playbook shell command", hint.Command)
	}
}

func TestPipelineMetadataExtractsJenkinsSignals(t *testing.T) {
	t.Parallel()

	source := `@Library('pipelines@v2') _
library identifier: 'shared-controllers@main'
pipelineDeploy(
  entry_point: 'dist/api.js',
  use_configd: true,
  pre_deploy: { sh 'ansible-playbook deploy.yml -i inventory/hosts --limit prod' }
)
`

	got := PipelineMetadata(source)
	assertStringSliceContains(t, got.SharedLibraries, "pipelines")
	assertStringSliceContains(t, got.SharedLibraries, "shared-controllers")
	assertStringSliceContains(t, got.PipelineCalls, "pipelineDeploy")
	assertStringSliceContains(t, got.EntryPoints, "dist/api.js")
	assertStringSliceContains(t, got.ShellCommands, "ansible-playbook deploy.yml -i inventory/hosts --limit prod")
	if got.UseConfigd == nil || *got.UseConfigd != true {
		t.Fatalf("UseConfigd = %#v, want true pointer", got.UseConfigd)
	}
	if !got.HasPreDeploy {
		t.Fatalf("HasPreDeploy = false, want true")
	}
	if len(got.AnsiblePlaybookHints) != 1 {
		t.Fatalf("AnsiblePlaybookHints len = %d, want 1: %#v", len(got.AnsiblePlaybookHints), got.AnsiblePlaybookHints)
	}
	if got.AnsiblePlaybookHints[0].Playbook != "deploy.yml" {
		t.Fatalf("Playbook = %q, want deploy.yml", got.AnsiblePlaybookHints[0].Playbook)
	}
	if got.AnsiblePlaybookHints[0].Inventory != "inventory/hosts" {
		t.Fatalf("Inventory = %#v, want inventory/hosts", got.AnsiblePlaybookHints[0].Inventory)
	}
}

func TestPipelineMetadataMapPreservesExistingPayloadShape(t *testing.T) {
	t.Parallel()

	got := PipelineMetadata(`pipelineDeploy(entry_point: 'deploy.sh')`).Map()
	if _, ok := got["use_configd"]; !ok {
		t.Fatalf("Map() missing use_configd key")
	}
	if _, ok := got["ansible_playbook_hints"].([]map[string]any); !ok {
		t.Fatalf("ansible_playbook_hints = %T, want []map[string]any", got["ansible_playbook_hints"])
	}
}

// TestPipelineMetadataCharacterization pins the exact output of PipelineMetadata
// for a representative Jenkinsfile fixture that exercises all nine Jenkins-DSL
// evidence patterns.  This test must stay green when any of those patterns are
// touched; a regression here means a consumer-visible contract break.
//
// The nine patterns and the fields they populate:
//
//	groovyLibraryPattern        -> SharedLibraries (annotation form)
//	groovyLibraryStepPattern    -> SharedLibraries (step form)
//	groovyPipelineCallPattern   -> PipelineCalls
//	groovyShellCommandPattern   -> ShellCommands
//	groovyAnsiblePattern        -> AnsiblePlaybookHints (applied to ShellCommands)
//	groovyEntryPointPattern     -> EntryPoints
//	groovyUseConfigdPattern     -> UseConfigd
//	groovyPreDeployPattern      -> HasPreDeploy
//	groovyJenkinsEntrypointPattern (entities.go) -> firstGroovyJenkinsEntrypointLine
func TestPipelineMetadataCharacterization(t *testing.T) {
	t.Parallel()

	// Fixture covers every pattern exactly once.
	source := `@Library('my-pipeline-lib@v3') _
library identifier: 'deploy-helpers@main'
pipeline {
  agent any
  stages {
    stage('Deploy') {
      steps {
        script {
          pipelineRelease(
            entry_point: 'cmd/server/main.go',
            use_configd: false,
            pre_deploy: {}
          )
          sh 'ansible-playbook site.yml -i envs/prod'
        }
      }
    }
  }
}
`

	got := PipelineMetadata(source)

	// SharedLibraries: both annotation and step forms, version suffixes stripped.
	wantLibs := []string{"my-pipeline-lib", "deploy-helpers"}
	if len(got.SharedLibraries) != len(wantLibs) {
		t.Fatalf("SharedLibraries = %#v, want %#v", got.SharedLibraries, wantLibs)
	}
	for i, want := range wantLibs {
		if got.SharedLibraries[i] != want {
			t.Fatalf("SharedLibraries[%d] = %q, want %q", i, got.SharedLibraries[i], want)
		}
	}

	// PipelineCalls: the pipeline-step call name captured.
	assertStringSliceContains(t, got.PipelineCalls, "pipelineRelease")

	// EntryPoints: the entry_point value captured.
	assertStringSliceContains(t, got.EntryPoints, "cmd/server/main.go")

	// UseConfigd: the boolean value captured correctly.
	if got.UseConfigd == nil || *got.UseConfigd != false {
		t.Fatalf("UseConfigd = %#v, want false pointer", got.UseConfigd)
	}

	// HasPreDeploy: pre_deploy key presence detected.
	if !got.HasPreDeploy {
		t.Fatalf("HasPreDeploy = false, want true")
	}

	// ShellCommands: the sh argument captured.
	assertStringSliceContains(t, got.ShellCommands, "ansible-playbook site.yml -i envs/prod")

	// AnsiblePlaybookHints: derived from ShellCommands.
	if len(got.AnsiblePlaybookHints) != 1 {
		t.Fatalf("AnsiblePlaybookHints len = %d, want 1: %#v", len(got.AnsiblePlaybookHints), got.AnsiblePlaybookHints)
	}
	hint := got.AnsiblePlaybookHints[0]
	if hint.Playbook != "site.yml" {
		t.Fatalf("Playbook = %q, want site.yml", hint.Playbook)
	}
	if hint.Inventory != "envs/prod" {
		t.Fatalf("Inventory = %#v, want envs/prod", hint.Inventory)
	}

	// groovyJenkinsEntrypointPattern: pipeline{ opener found on line 3.
	line := firstGroovyJenkinsEntrypointLine(source)
	if line != 3 {
		t.Fatalf("firstGroovyJenkinsEntrypointLine = %d, want 3", line)
	}
}

func assertStringSliceContains(t *testing.T, values []string, want string) {
	t.Helper()

	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("%#v does not contain %q", values, want)
}
