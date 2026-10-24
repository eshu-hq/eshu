// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

type binaryPermissionJob struct {
	Permissions map[string]string `yaml:"permissions"`
}

type binaryPermissionWorkflow struct {
	Permissions map[string]string              `yaml:"permissions"`
	Jobs        map[string]binaryPermissionJob `yaml:"jobs"`
}

// TestBinaryBuildReusablePermissions checks GitHub's caller permission ceiling
// without giving the ordinary binary build the release job's write token.
func TestBinaryBuildReusablePermissions(t *testing.T) {
	t.Parallel()
	read := func(name string) binaryPermissionWorkflow {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		var workflow binaryPermissionWorkflow
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatal(err)
		}
		return workflow
	}
	caller, callee := read("test.yml"), read("build.yml")
	if got, want := caller.Jobs["call-build"].Permissions["contents"], callee.Permissions["contents"]; got != want {
		t.Errorf("call-build contents permission %q cannot admit reusable workflow permission %q", got, want)
	}
	if got := callee.Permissions["contents"]; got != "write" {
		t.Errorf("release workflow contents permission = %q, want existing write permission", got)
	}
	if got := callee.Jobs["build"].Permissions["contents"]; got != "read" {
		t.Errorf("binary build contents permission = %q, want explicit read permission", got)
	}
}
