// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Queue-path jobs carry no Docker Hub login step: every queue-path image
// resolves to a GHCR digest in specs/ci-images.v1.json, so a login only
// spends shared-runner pulls against the anonymous limit (#7886). This test
// pins the invariant for all ten workflows that carried one; the readiness
// split test additionally pins the live job's shape.
func TestQueueWorkflowsCarryNoDockerHubLogin(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	for _, file := range []string{
		"docker-publish.yml",
		"e2e-tests.yml",
		"frontend.yml",
		"golden-corpus-gate.yml",
		"ifa-determinism-gate.yml",
		"live-backend-tests.yml",
		"live-postgres-readiness.yml",
		"read-api-latency-gate.yml",
		"static-contract-gates.yml",
		"test.yml",
	} {
		raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", file))
		if err != nil {
			t.Fatal(err)
		}
		var parsed loginSweepWorkflow
		if err := yaml.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for name, job := range parsed.Jobs {
			if _, ok := job.Env["DOCKERHUB_LOGIN_ENABLED"]; ok {
				t.Errorf("%s job %s carries a Docker Hub login flag", file, name)
			}
			for _, step := range job.Steps {
				if isDockerHubLoginStep(step.Uses, step.With) {
					t.Errorf("%s job %s logs in to Docker Hub", file, name)
				}
			}
		}
	}
}

func TestIsDockerHubLoginStep(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		uses  string
		with  map[string]string
		login bool
	}{
		{"not a login", "actions/checkout@v5", nil, false},
		{"ghcr login passes", "docker/login-action@v3", map[string]string{"registry": "ghcr.io"}, false},
		{"registry expression passes", "docker/login-action@v3", map[string]string{"registry": "${{ env.REGISTRY }}"}, false},
		{"bare login fails", "docker/login-action@v3", nil, true},
		{"empty registry fails", "docker/login-action@v3", map[string]string{"registry": ""}, true},
		{"docker.io registry fails", "docker/login-action@v3", map[string]string{"registry": "docker.io"}, true},
	} {
		if got := isDockerHubLoginStep(tc.uses, tc.with); got != tc.login {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.login)
		}
	}
}

type loginSweepWorkflow struct {
	Jobs map[string]loginSweepJob `yaml:"jobs"`
}

type loginSweepJob struct {
	Env   map[string]string `yaml:"env"`
	Steps []loginSweepStep  `yaml:"steps"`
}

type loginSweepStep struct {
	Uses string            `yaml:"uses"`
	With map[string]string `yaml:"with"`
}

// isDockerHubLoginStep reports whether a docker/login-action step
// authenticates to Docker Hub: no registry input means Docker Hub, as does
// an explicit docker.io registry. A GHCR registry (literal or expression)
// is not a Docker Hub login.
func isDockerHubLoginStep(uses string, with map[string]string) bool {
	if !strings.HasPrefix(uses, "docker/login-action@") {
		return false
	}
	registry, ok := with["registry"]
	return !ok || registry == "" || strings.Contains(registry, "docker.io")
}
