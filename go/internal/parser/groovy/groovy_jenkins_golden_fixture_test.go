// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package groovy_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// TestJenkinsGroovyGoldenFixtureDiscriminatesPipeline pins the
// jenkins-ci-pipelines golden-corpus fixture's positive-vs-foil discrimination
// for the #5337 Jenkins-artifact gating: the repo-root Jenkinsfile (a real
// Jenkins artifact with a `pipeline { ... }` block) yields the synthetic
// "Jenkinsfile" entrypoint rooted as groovy.jenkins_pipeline_entrypoint, while
// src/DeployHelper.groovy — an ordinary source file that also contains a
// `pipelineDeploy(` call — is NOT a Jenkins artifact, so its deployApp method is
// unrooted and the genuine AST call survives only in function_calls (not as
// gated pipeline_calls metadata). Parser-tier proof for the staged fixture; the
// end-to-end proof is the B-7 gate's dead-code query shape scoped to
// jenkins-ci-pipelines.
func TestJenkinsGroovyGoldenFixtureDiscriminatesPipeline(t *testing.T) {
	t.Parallel()

	repoRoot := groovyFixturePath(t, "ecosystems", "jenkins-ci-pipelines")
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	jenkinsfilePath := filepath.Join(repoRoot, "Jenkinsfile")
	jenkinsfilePayload, err := engine.ParsePath(repoRoot, jenkinsfilePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", jenkinsfilePath, err)
	}
	// POSITIVE: the Jenkins artifact yields the rooted synthetic entrypoint.
	parsertest.AssertStringSliceContains(
		t,
		parsertest.AssertBucketItemByName(t, jenkinsfilePayload, "functions", "Jenkinsfile"),
		"dead_code_root_kinds",
		"groovy.jenkins_pipeline_entrypoint",
	)

	foilPath := filepath.Join(repoRoot, "src", "DeployHelper.groovy")
	foilPayload, err := engine.ParsePath(repoRoot, foilPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", foilPath, err)
	}
	// FOIL: the ordinary source method is not rooted...
	if foil := parsertest.AssertBucketItemByName(t, foilPayload, "functions", "deployApp"); foil["dead_code_root_kinds"] != nil {
		t.Fatalf("deployApp dead_code_root_kinds = %#v, want nil (src/*.groovy is not a Jenkins artifact)", foil["dead_code_root_kinds"])
	}
	// ...and the Jenkins PipelineMetadata evidence is withheld for a non-artifact.
	if _, ok := foilPayload["pipeline_calls"]; ok {
		t.Fatalf("pipeline_calls present for src/*.groovy foil, want absent: %#v", foilPayload["pipeline_calls"])
	}
	// ...while the genuine AST call still survives in function_calls.
	parsertest.AssertNamedBucketContains(t, foilPayload, "function_calls", "pipelineDeploy")
}
