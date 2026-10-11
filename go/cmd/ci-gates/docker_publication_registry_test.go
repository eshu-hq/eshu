// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// TestDockerPublicationScriptsSelectBlockingGates protects the PR aggregate:
// every publication helper can change lane behavior, so each helper edit must
// require all four hosted checks that exercise or depend on those lanes.
func TestDockerPublicationScriptsSelectBlockingGates(t *testing.T) {
	t.Parallel()

	reg, err := cigates.Load(filepath.Join(repoRoot(t), "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("load committed registry: %v", err)
	}
	want := map[string]string{
		"docker-image-build":           "build-and-push-image",
		"docker-image-reproducibility": "verify-reproducibility",
		"apk-floors":                   "verify-apk-floors",
		"helm-package":                 "package-and-push-chart",
	}
	paths := []string{
		"scripts/select-docker-publication-lanes.sh",
		"scripts/test-docker-release-image-dispatch.sh",
		"scripts/assert-docker-release-image-tags.sh",
		"scripts/assert-ghcr-image-tag-absent.sh",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			required, err := reg.RequiredGates([]string{path})
			if err != nil {
				t.Fatalf("select required gates: %v", err)
			}
			selected := make(map[string]string)
			for _, gate := range required {
				for _, id := range gate.GateIDs {
					if gate.Workflow != "docker-publish.yml" {
						continue
					}
					selected[id] = gate.Job
				}
			}
			for id, job := range want {
				if selected[id] != job {
					t.Errorf("%s selects %q job %q, want %q; selected: %v", path, id, selected[id], job, selected)
				}
			}
		})
	}
}
