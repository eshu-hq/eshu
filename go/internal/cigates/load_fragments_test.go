// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestLoadGateFragments(t *testing.T) {
	for _, tc := range []struct {
		name, root, shard string
		wantErr           bool
	}{
		{"valid", "version: v1\ngate_fragments: [ci-gates.d/first.yaml]\n", "version: v1\ngates:\n" + strings.TrimPrefix(minimalValidYAML, "version: v1\ngates:\n"), false},
		{"missing", "version: v1\ngate_fragments: [ci-gates.d/missing.yaml]\n", "", true},
		{"duplicate ref", "version: v1\ngate_fragments: [ci-gates.d/first.yaml, ci-gates.d/first.yaml]\n", "version: v1\ngates:\n" + strings.TrimPrefix(minimalValidYAML, "version: v1\ngates:\n"), true},
		{"mixed", minimalValidYAML + "gate_fragments: [ci-gates.d/first.yaml]\n", "version: v1\ngates:\n" + strings.TrimPrefix(minimalValidYAML, "version: v1\ngates:\n"), true},
		{"escape", "version: v1\ngate_fragments: [../outside.yaml]\n", "", true},
		{"unknown shard key", "version: v1\ngate_fragments: [ci-gates.d/first.yaml]\n", "version: v1\nwrong: true\ngates:\n" + strings.TrimPrefix(minimalValidYAML, "version: v1\ngates:\n"), true},
		{"omitted declaration", "version: v1\n", "", true},
		{"empty refs", "version: v1\ngate_fragments: []\n", "", true},
		{"absolute", "version: v1\ngate_fragments: [/tmp/outside.yaml]\n", "", true},
		{"duplicate root key", "version: v1\nversion: v1\ngate_fragments: [ci-gates.d/first.yaml]\n", "", true},
		{"malformed shard", "version: v1\ngate_fragments: [ci-gates.d/first.yaml]\n", "version: [", true},
		{"unknown record key", "version: v1\ngate_fragments: [ci-gates.d/first.yaml]\n", "version: v1\ngates:\n" + strings.Replace(strings.TrimPrefix(minimalValidYAML, "version: v1\ngates:\n"), "    name:", "    unknown: true\n    name:", 1), true},
		{"duplicate id", "version: v1\ngate_fragments: [ci-gates.d/first.yaml]\n", minimalValidYAML + strings.TrimPrefix(minimalValidYAML, "version: v1\ngates:\n"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "ci-gates.d"), 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.shard != "" {
				if err := os.WriteFile(filepath.Join(dir, "ci-gates.d", "first.yaml"), []byte(tc.shard), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(dir, "ci-gates.v1.yaml")
			if err := os.WriteFile(path, []byte(tc.root), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := cigates.Load(path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Load accepted invalid fragments: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Gates) != 1 || got.Gates[0].ID != "openapi-surface" {
				t.Fatalf("unexpected gates: %+v", got.Gates)
			}
		})
	}
}

func TestLoadGateFragmentSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	fragments := filepath.Join(dir, "ci-gates.d")
	if err := os.Mkdir(fragments, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside.yaml")
	if err := os.WriteFile(outside, []byte(minimalValidYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(fragments, "first.yaml")); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "ci-gates.v1.yaml")
	if err := os.WriteFile(root, []byte("version: v1\ngate_fragments: [ci-gates.d/first.yaml]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := cigates.Load(root); err == nil {
		t.Fatal("Load accepted fragment symlink outside ci-gates.d")
	}
}
