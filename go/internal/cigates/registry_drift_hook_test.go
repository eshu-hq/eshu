// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// registryDriftHookFiles returns the files: pattern of the real
// .pre-commit-config.yaml hook that the ci-gate-registry gate names as its
// hook_id, so the test follows the registry instead of a copied hook id.
func registryDriftHookFiles(t *testing.T, root string) (string, *Gate) {
	t.Helper()
	reg, err := Load(filepath.Join(root, "specs/ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	var gate *Gate
	for i := range reg.Gates {
		if reg.Gates[i].ID == "ci-gate-registry" {
			gate = &reg.Gates[i]
		}
	}
	if gate == nil || gate.HookID == "" {
		t.Fatal("ci-gate-registry gate or its hook_id is missing")
	}
	raw, err := os.ReadFile(filepath.Join(root, ".pre-commit-config.yaml"))
	if err != nil {
		t.Fatalf("read .pre-commit-config.yaml: %v", err)
	}
	var cfg struct {
		Repos []struct {
			Hooks []struct {
				ID    string `yaml:"id"`
				Files string `yaml:"files"`
			} `yaml:"hooks"`
		} `yaml:"repos"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse .pre-commit-config.yaml: %v", err)
	}
	for _, repo := range cfg.Repos {
		for _, h := range repo.Hooks {
			if h.ID == gate.HookID {
				return h.Files, gate
			}
		}
	}
	t.Fatalf("hook %q not found in .pre-commit-config.yaml", gate.HookID)
	return "", nil
}

// TestRegistryDriftHookCoversFragmentsAndScripts pins that the pre-commit
// registry drift hook runs for every path that can introduce registry drift
// (#9615): each registry fragment under specs/ci-gates.d/ (every gate row has
// lived there since the registry split), each scripts/ trigger of the
// ci-gate-registry gate, and a newly added scripts/lib module, the shape #9604
// missed when a gate command imported a module no trigger named. The hook's
// files: pattern is a Python re.search regex; RE2 accepts the same syntax here.
func TestRegistryDriftHookCoversFragmentsAndScripts(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	files, gate := registryDriftHookFiles(t, root)
	re, err := regexp.Compile(files)
	if err != nil {
		t.Fatalf("hook files: pattern %q does not compile: %v", files, err)
	}

	fragments, err := filepath.Glob(filepath.Join(root, "specs/ci-gates.d/*.yaml"))
	if err != nil || len(fragments) == 0 {
		t.Fatalf("no registry fragments found (err %v)", err)
	}
	want := []string{"scripts/lib/new_gate_imported_module.py"}
	for _, f := range fragments {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, filepath.ToSlash(rel))
	}
	for _, trigger := range gate.Triggers {
		if strings.HasPrefix(trigger, "scripts/") && !strings.ContainsAny(trigger, "*?[") {
			want = append(want, trigger)
		}
	}
	for _, p := range want {
		if !re.MatchString(p) {
			t.Errorf("hook %q files: pattern does not match %q; a commit touching it would skip the registry drift check", gate.HookID, p)
		}
	}
}
