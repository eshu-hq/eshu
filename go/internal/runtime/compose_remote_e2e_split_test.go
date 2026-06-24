// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRemoteE2EComposeRootIncludesFragments(t *testing.T) {
	t.Parallel()

	raw := readRepositoryFile(t, "../../..", "docker-compose.remote-e2e.yaml")
	var doc struct {
		Include  []string                  `yaml:"include"`
		Services map[string]composeService `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("parse remote E2E Compose root: %v", err)
	}

	want := []string{
		"docker-compose.remote-e2e.foundation.yaml",
		"docker-compose.remote-e2e.runtime.yaml",
		"docker-compose.remote-e2e.seed.yaml",
	}
	if strings.Join(doc.Include, "\n") != strings.Join(want, "\n") {
		t.Fatalf("remote E2E Compose include = %#v, want %#v", doc.Include, want)
	}
	if len(doc.Services) != 0 {
		t.Fatalf("remote E2E Compose root should delegate services to fragments, got %d root services", len(doc.Services))
	}
}

func TestRemoteE2EComposeFilesStayUnderAgentLineCap(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"docker-compose.remote-e2e.yaml",
		"docker-compose.remote-e2e.foundation.yaml",
		"docker-compose.remote-e2e.runtime.yaml",
		"docker-compose.remote-e2e.seed.yaml",
	} {
		lines := countRepositoryFileLines(t, path)
		if lines > 500 {
			t.Fatalf("%s has %d lines, want <= 500", path, lines)
		}
	}
}

func countRepositoryFileLines(t *testing.T, relativePath string) int {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", relativePath))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	if len(raw) == 0 {
		return 0
	}
	lines := strings.Count(string(raw), "\n")
	if !strings.HasSuffix(string(raw), "\n") {
		lines++
	}
	return lines
}
