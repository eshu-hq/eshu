// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package preparse

import (
	"strings"
	"testing"
	"time"
)

func baseConfig() Config {
	return Config{
		IgnoredPathGlobs:   []string{"vendor/**=vendored"},
		PreservedPathGlobs: []string{"docs/**"},
		EmitDataflow:       false,
		SCIPEnabled:        true,
		SCIPLanguages:      []string{"go", "python"},
		SCIPBinaries: []BinaryStat{
			{Language: "go", Path: "/usr/local/bin/scip-go", Size: 100, ModTime: time.Unix(1, 0)},
		},
		DependencyMode:     false,
		DependencyName:     "",
		DependencyLanguage: "",
	}
}

// TestIdentityChangesWithEveryFoldedInput is the per-input cell set ruling
// a9-143 item 1 requires: changing any input that changes emitted facts
// changes the pre-parse identity, so a skip can never hide new output.
func TestIdentityChangesWithEveryFoldedInput(t *testing.T) {
	t.Parallel()
	base := Identity("git-collector.v1;schema=aa", "commit-a", "main", baseConfig())
	if !strings.HasPrefix(base, IdentityPrefix) {
		t.Fatalf("identity %q lacks prefix %q", base, IdentityPrefix)
	}
	cells := []struct {
		name string
		got  string
	}{
		{"content version", Identity("git-collector.v2;schema=aa", "commit-a", "main", baseConfig())},
		{"commit", Identity("git-collector.v1;schema=aa", "commit-b", "main", baseConfig())},
		{"default branch", Identity("git-collector.v1;schema=aa", "commit-a", "trunk", baseConfig())},
	}
	mutate := map[string]func(*Config){
		"ignored path globs":      func(c *Config) { c.IgnoredPathGlobs = append(c.IgnoredPathGlobs, "gen/**=generated") },
		"ignored glob reason":     func(c *Config) { c.IgnoredPathGlobs = []string{"vendor/**=other"} },
		"preserved path globs":    func(c *Config) { c.PreservedPathGlobs = nil },
		"emit dataflow":           func(c *Config) { c.EmitDataflow = true },
		"scip enabled":            func(c *Config) { c.SCIPEnabled = false },
		"scip languages":          func(c *Config) { c.SCIPLanguages = []string{"go"} },
		"scip binary size":        func(c *Config) { c.SCIPBinaries[0].Size = 101 },
		"scip binary mtime":       func(c *Config) { c.SCIPBinaries[0].ModTime = time.Unix(2, 0) },
		"scip binary path":        func(c *Config) { c.SCIPBinaries[0].Path = "/opt/scip-go" },
		"scip binary missing":     func(c *Config) { c.SCIPBinaries[0].Missing = true },
		"dependency mode":         func(c *Config) { c.DependencyMode = true },
		"dependency package name": func(c *Config) { c.DependencyName = "pkg" },
		"dependency language":     func(c *Config) { c.DependencyLanguage = "go" },
	}
	for name, change := range mutate {
		config := baseConfig()
		config.SCIPBinaries = append([]BinaryStat(nil), config.SCIPBinaries...)
		change(&config)
		cells = append(cells, struct {
			name string
			got  string
		}{name, Identity("git-collector.v1;schema=aa", "commit-a", "main", config)})
	}
	for _, cell := range cells {
		if cell.got == base {
			t.Errorf("%s: identity did not change", cell.name)
		}
	}
}

// TestIdentityIgnoresOrder: the same configuration in another order is the
// same identity, so a reordered env list does not force a parse.
func TestIdentityIgnoresOrder(t *testing.T) {
	t.Parallel()
	a := baseConfig()
	b := baseConfig()
	b.SCIPLanguages = []string{"python", "go"}
	b.IgnoredPathGlobs = []string{"vendor/**=vendored"}
	if Identity("v", "c", "main", a) != Identity("v", "c", "main", b) {
		t.Fatal("reordered SCIP languages changed the identity")
	}
	c := baseConfig()
	c.IgnoredPathGlobs = []string{"x/**=r", "vendor/**=vendored"}
	d := baseConfig()
	d.IgnoredPathGlobs = []string{"vendor/**=vendored", "x/**=r"}
	if Identity("v", "c", "main", c) != Identity("v", "c", "main", d) {
		t.Fatal("reordered ignored globs changed the identity")
	}
}

// TestIdentityRefusesBlankInputs: no commit or content version means no
// identity, so the caller can never stamp or match an empty one.
func TestIdentityRefusesBlankInputs(t *testing.T) {
	t.Parallel()
	if got := Identity("", "c", "main", baseConfig()); got != "" {
		t.Fatalf("blank content version identity = %q, want empty", got)
	}
	if got := Identity("v", " ", "main", baseConfig()); got != "" {
		t.Fatalf("blank commit identity = %q, want empty", got)
	}
}
