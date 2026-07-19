// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package groovy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestParsePipelineMetadataGatedToJenkinsArtifacts characterizes issue #5337
// Detector 3: PipelineMetadata(sourceText) was called unconditionally for
// every .groovy file, so an ordinary class with a method merely named
// pipelineDeploy fabricated Jenkins evidence (pipeline_calls etc.) that feeds
// reducer isJenkinsArtifact and query deployment surfaces. Only a Jenkinsfile
// or a shared-library vars/*.groovy file is real Jenkins pipeline evidence;
// an arbitrary .groovy source file under src/ is not, regardless of what its
// methods happen to be named.
func TestParsePipelineMetadataGatedToJenkinsArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// Positive: a Jenkinsfile keeps pipeline_calls populated.
	jenkinsfile := filepath.Join(root, "Jenkinsfile")
	if err := os.WriteFile(jenkinsfile, []byte(`pipelineDeploy(entry_point: 'deploy.sh')
`), 0o644); err != nil {
		t.Fatalf("WriteFile(Jenkinsfile) error = %v, want nil", err)
	}
	got, err := Parse(jenkinsfile, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse(Jenkinsfile) error = %v, want nil", err)
	}
	assertStringSliceContains(t, got["pipeline_calls"].([]string), "pipelineDeploy")

	// Positive: a shared-library vars/*.groovy file keeps pipeline_calls
	// populated.
	varsFile := filepath.Join(root, "vars", "deployHelper.groovy")
	if err := os.MkdirAll(filepath.Dir(varsFile), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.WriteFile(varsFile, []byte(`def call(Map config = [:]) {
  pipelineDeploy(config)
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(vars/deployHelper.groovy) error = %v, want nil", err)
	}
	got, err = Parse(varsFile, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse(vars/deployHelper.groovy) error = %v, want nil", err)
	}
	assertStringSliceContains(t, got["pipeline_calls"].([]string), "pipelineDeploy")

	// Negative: an ordinary .groovy source file under src/ whose method is
	// merely named pipelineDeploy must not fabricate Jenkins metadata.
	srcFile := filepath.Join(root, "src", "DeployHelper.groovy")
	if err := os.MkdirAll(filepath.Dir(srcFile), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.WriteFile(srcFile, []byte(`class DeployHelper {
  def deployApp(String target) {
    pipelineDeploy(target)
  }
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(src/DeployHelper.groovy) error = %v, want nil", err)
	}
	got, err = Parse(srcFile, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse(src/DeployHelper.groovy) error = %v, want nil", err)
	}
	if calls, ok := got["pipeline_calls"].([]string); ok && len(calls) > 0 {
		t.Fatalf("pipeline_calls = %#v, want empty for a non-Jenkins .groovy source file", calls)
	}
	if libs, ok := got["shared_libraries"].([]string); ok && len(libs) > 0 {
		t.Fatalf("shared_libraries = %#v, want empty for a non-Jenkins .groovy source file", libs)
	}
	// The genuine AST-derived call (not Jenkins metadata) is still captured.
	assertBucketItemByName(t, got, "function_calls", "pipelineDeploy")
}
