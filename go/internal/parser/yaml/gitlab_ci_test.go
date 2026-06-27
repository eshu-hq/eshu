// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"os"
	"path/filepath"
	"testing"
)

// writeGitlabCI writes source to a .gitlab-ci.yml in a temp dir and returns the
// parsed payload, failing the test on any parse error.
func writeGitlabCI(t *testing.T, source string) map[string]any {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitlab-ci.yml")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write .gitlab-ci.yml: %v", err)
	}
	payload, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	return payload
}

// gitlabJobByName returns the job row with the given name, or nil.
func gitlabJobByName(rows []map[string]any, name string) map[string]any {
	for _, row := range rows {
		if row["name"] == name {
			return row
		}
	}
	return nil
}

// TestParseGitlabCIEmitsPipelineAndJobRows proves a .gitlab-ci.yml is detected by
// filename and produces one GitlabPipeline row (ordered stages + variable count)
// plus one GitlabJob row per top-level job, with each job's stage, when, image,
// needs, and script line count captured.
func TestParseGitlabCIEmitsPipelineAndJobRows(t *testing.T) {
	t.Parallel()

	source := `stages:
  - build
  - test
variables:
  CI_DEBUG: "false"
  REGION: us-east-1
build-app:
  stage: build
  image: golang:1.23
  script:
    - go build ./...
    - go vet ./...
run-tests:
  stage: test
  when: on_success
  image:
    name: golang:1.23
    entrypoint: [""]
  needs:
    - build-app
  script:
    - go test ./...
`
	payload := writeGitlabCI(t, source)

	pipelines, ok := payload["gitlab_pipelines"].([]map[string]any)
	if !ok || len(pipelines) != 1 {
		t.Fatalf("gitlab_pipelines = %T len=%d, want 1; keys=%v", payload["gitlab_pipelines"], len(pipelines), keysOf(payload))
	}
	pipeline := pipelines[0]
	if got := pipeline["name"]; got != gitlabPipelineName {
		t.Fatalf("pipeline name = %v, want %v", got, gitlabPipelineName)
	}
	if got := pipeline["stages"]; got != "build,test" {
		t.Fatalf("pipeline stages = %v, want build,test (ordered)", got)
	}
	if got := pipeline["variable_count"]; got != 2 {
		t.Fatalf("pipeline variable_count = %v, want 2", got)
	}

	jobs, ok := payload["gitlab_jobs"].([]map[string]any)
	if !ok || len(jobs) != 2 {
		t.Fatalf("gitlab_jobs len = %d, want 2; rows=%+v", len(jobs), jobs)
	}

	build := gitlabJobByName(jobs, "build-app")
	if build == nil {
		t.Fatalf("build-app job missing; rows=%+v", jobs)
	}
	if got := build["job_stage"]; got != "build" {
		t.Fatalf("build-app job_stage = %v, want build", got)
	}
	if got := build["image"]; got != "golang:1.23" {
		t.Fatalf("build-app image = %v, want golang:1.23", got)
	}
	if got := build["script_line_count"]; got != 2 {
		t.Fatalf("build-app script_line_count = %v, want 2", got)
	}
	if _, present := build["needs"]; present {
		t.Fatalf("build-app should have no needs; got %v", build["needs"])
	}

	test := gitlabJobByName(jobs, "run-tests")
	if test == nil {
		t.Fatalf("run-tests job missing; rows=%+v", jobs)
	}
	if got := test["job_stage"]; got != "test" {
		t.Fatalf("run-tests job_stage = %v, want test", got)
	}
	if got := test["job_when"]; got != "on_success" {
		t.Fatalf("run-tests job_when = %v, want on_success", got)
	}
	if got := test["image"]; got != "golang:1.23" {
		t.Fatalf("run-tests image (mapping form) = %v, want golang:1.23", got)
	}
	if got := test["needs"]; got != "build-app" {
		t.Fatalf("run-tests needs = %v, want build-app", got)
	}
}

// TestParseGitlabCIRowsSortedByLine proves the job rows are emitted in line order
// so the bucket is deterministic across runs.
func TestParseGitlabCIRowsSortedByLine(t *testing.T) {
	t.Parallel()

	source := `stages: [build]
zebra:
  stage: build
  script: ["echo z"]
alpha:
  stage: build
  script: ["echo a"]
`
	payload := writeGitlabCI(t, source)
	jobs := payload["gitlab_jobs"].([]map[string]any)
	if len(jobs) != 2 {
		t.Fatalf("gitlab_jobs len = %d, want 2", len(jobs))
	}
	// "zebra" is declared first (lower line) so it sorts before "alpha".
	if jobs[0]["name"] != "zebra" || jobs[1]["name"] != "alpha" {
		t.Fatalf("jobs not in line order: %v, %v", jobs[0]["name"], jobs[1]["name"])
	}
}

// TestParseGitlabCIExcludesHiddenAndReservedKeys proves hidden/template jobs
// (keys starting with ".") and the reserved global keywords never become job
// rows, while real jobs still do.
func TestParseGitlabCIExcludesHiddenAndReservedKeys(t *testing.T) {
	t.Parallel()

	source := `stages:
  - build
variables:
  X: "1"
default:
  image: alpine
before_script:
  - echo global
cache:
  paths: [vendor/]
.hidden-template:
  stage: build
  script: ["echo template"]
real-job:
  stage: build
  extends: .hidden-template
  script: ["echo real"]
`
	payload := writeGitlabCI(t, source)
	jobs := payload["gitlab_jobs"].([]map[string]any)
	if len(jobs) != 1 {
		t.Fatalf("gitlab_jobs len = %d, want 1 (only real-job); rows=%+v", len(jobs), jobs)
	}
	if jobs[0]["name"] != "real-job" {
		t.Fatalf("job name = %v, want real-job", jobs[0]["name"])
	}
	// No reserved keyword or hidden job leaked in.
	for _, banned := range []string{"default", "before_script", "cache", "variables", "stages", ".hidden-template"} {
		if gitlabJobByName(jobs, banned) != nil {
			t.Fatalf("reserved/hidden key %q leaked into job rows", banned)
		}
	}
}

// TestParseGitlabCIDependenciesFallback proves a job with no needs: but a
// dependencies: list still resolves its dependency job names.
func TestParseGitlabCIDependenciesFallback(t *testing.T) {
	t.Parallel()

	source := `stages: [build, deploy]
compile:
  stage: build
  script: ["make"]
ship:
  stage: deploy
  dependencies:
    - compile
  script: ["deploy"]
`
	payload := writeGitlabCI(t, source)
	jobs := payload["gitlab_jobs"].([]map[string]any)
	ship := gitlabJobByName(jobs, "ship")
	if ship == nil {
		t.Fatalf("ship job missing; rows=%+v", jobs)
	}
	if got := ship["needs"]; got != "compile" {
		t.Fatalf("ship needs (from dependencies) = %v, want compile", got)
	}
}

// TestParseGitlabCINeedsListOfMaps proves the GitLab-allowed object form of
// needs: (a list of {job: <name>, ...} maps, used with optional/artifacts) is
// resolved to the dependency job name, the same as the bare-string list form.
func TestParseGitlabCINeedsListOfMaps(t *testing.T) {
	t.Parallel()

	source := `stages: [build, test]
build-app:
  stage: build
  script: ["make"]
run-tests:
  stage: test
  needs:
    - job: build-app
      optional: true
  script: ["test"]
`
	payload := writeGitlabCI(t, source)
	jobs := payload["gitlab_jobs"].([]map[string]any)
	test := gitlabJobByName(jobs, "run-tests")
	if test == nil {
		t.Fatalf("run-tests job missing; rows=%+v", jobs)
	}
	if got := test["needs"]; got != "build-app" {
		t.Fatalf("run-tests needs (list-of-maps form) = %v, want build-app", got)
	}
}

// TestParseGitlabCIAnchorsResolve proves YAML anchors / merge keys in job bodies
// resolve so the merged job carries the inherited stage and script.
func TestParseGitlabCIAnchorsResolve(t *testing.T) {
	t.Parallel()

	source := `stages: [test]
.base: &base
  stage: test
  script:
    - echo base
unit:
  <<: *base
  image: node:20
`
	payload := writeGitlabCI(t, source)
	jobs := payload["gitlab_jobs"].([]map[string]any)
	unit := gitlabJobByName(jobs, "unit")
	if unit == nil {
		t.Fatalf("unit job missing; rows=%+v", jobs)
	}
	if got := unit["job_stage"]; got != "test" {
		t.Fatalf("unit job_stage = %v, want test (merged from anchor)", got)
	}
	if got := unit["script_line_count"]; got != 1 {
		t.Fatalf("unit script_line_count = %v, want 1 (merged from anchor)", got)
	}
	if got := unit["image"]; got != "node:20" {
		t.Fatalf("unit image = %v, want node:20", got)
	}
}

// TestParseGitlabCINonCIYAMLEmpty proves a YAML file named .gitlab-ci.yml whose
// content is not a mapping (or carries no jobs) yields empty buckets rather than
// fabricating nodes, and that a non-CI YAML file does not populate the buckets.
func TestParseGitlabCINonCIYAMLEmpty(t *testing.T) {
	t.Parallel()

	// A regular k8s manifest must not produce gitlab buckets.
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy.yaml")
	manifest := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n"
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write deploy.yaml: %v", err)
	}
	payload, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if pipelines, _ := payload["gitlab_pipelines"].([]map[string]any); len(pipelines) != 0 {
		t.Fatalf("non-CI YAML produced %d gitlab_pipelines, want 0", len(pipelines))
	}
	if jobs, _ := payload["gitlab_jobs"].([]map[string]any); len(jobs) != 0 {
		t.Fatalf("non-CI YAML produced %d gitlab_jobs, want 0", len(jobs))
	}

	// A .gitlab-ci.yml with only globals (no jobs) yields a pipeline but no jobs.
	payloadGlobals := writeGitlabCI(t, "stages: [build]\nvariables:\n  A: \"1\"\n")
	if pipelines, _ := payloadGlobals["gitlab_pipelines"].([]map[string]any); len(pipelines) != 1 {
		t.Fatalf("globals-only .gitlab-ci.yml gitlab_pipelines = %d, want 1", len(pipelines))
	}
	if jobs, _ := payloadGlobals["gitlab_jobs"].([]map[string]any); len(jobs) != 0 {
		t.Fatalf("globals-only .gitlab-ci.yml gitlab_jobs = %d, want 0", len(jobs))
	}
}
