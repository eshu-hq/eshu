// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestShouldSkipContentMetadataEquivalence is the mandatory 0/0 exact-equivalence
// proof for issue #4768. inferContentMetadata is ~7.5ms on a large PHP/JS file
// (three regex scans: goLineControlRE, tfTemplatefileRE, and inferRootFamily's
// internal re-scan). shouldSkipContentMetadata lets ParsePath skip that call and
// use the contentMetadata{} zero value whenever the answer is provably identical
// to running inferContentMetadata unconditionally.
//
// Every case below asserts inferContentMetadata(path, content) against the
// contentMetadata the gate implies (zero value when skip=true, the real call's
// result when skip=false), so a widened gate that starts skipping a
// path-triggered or content-triggered case fails this test immediately.
func TestShouldSkipContentMetadataEquivalence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		relativePath string
		content      string
		wantSkip     bool
	}{
		// --- gated: plain source files with no IaC signal by path or extension ---
		{
			name:         "plain python at repo root is gated",
			relativePath: "main.py",
			content:      "def handler():\n    return 200\n",
			wantSkip:     true,
		},
		{
			name:         "plain javascript at repo root is gated",
			relativePath: "index.js",
			content:      "module.exports = function() { return 1; };\n",
			wantSkip:     true,
		},
		{
			name:         "plain php at repo root is gated",
			relativePath: filepath.Join("src", "Controller.php"),
			content:      "<?php\nclass Controller {\n    public function index() {}\n}\n",
			wantSkip:     true,
		},
		{
			name:         "plain go at repo root is gated",
			relativePath: filepath.Join("internal", "service", "handler.go"),
			content:      "package service\n\nfunc Handler() int {\n\treturn 0\n}\n",
			wantSkip:     true,
		},

		// --- NOT gated: path-triggered IaC signal on a source-code extension ---
		{
			name:         "python under roles is not gated (ansible_role via path)",
			relativePath: filepath.Join("roles", "web", "library", "custom_module.py"),
			content:      "def main():\n    pass\n",
			wantSkip:     false,
		},
		{
			name:         "python under playbooks is not gated",
			relativePath: filepath.Join("playbooks", "filter_plugins", "custom.py"),
			content:      "def filters():\n    return {}\n",
			wantSkip:     false,
		},
		{
			name:         "javascript under dagster assets is not gated",
			relativePath: filepath.Join("dagster", "assets", "loader.js"),
			content:      "module.exports = {};\n",
			wantSkip:     false,
		},
		{
			name:         "python under argocd is not gated",
			relativePath: filepath.Join("argocd", "hooks", "presync.py"),
			content:      "def presync():\n    pass\n",
			wantSkip:     false,
		},
		{
			name:         "python under a bare iac path segment is not gated",
			relativePath: filepath.Join("iac", "scripts", "bootstrap.py"),
			content:      "def bootstrap():\n    pass\n",
			wantSkip:     false,
		},
		{
			name:         "python under github workflows is not gated",
			relativePath: filepath.Join(".github", "workflows", "generate.py"),
			content:      "def generate():\n    pass\n",
			wantSkip:     false,
		},
		{
			name:         "python under chart/templates is not gated",
			relativePath: filepath.Join("chart", "templates", "hooks.py"),
			content:      "def hook():\n    pass\n",
			wantSkip:     false,
		},

		// --- NOT gated: basename-triggered IaC signal without a gated extension ---
		{
			name:         "bare Dockerfile is not gated",
			relativePath: "Dockerfile",
			content:      "FROM golang:1.24\nRUN go build ./...\n",
			wantSkip:     false,
		},
		{
			name:         "Dockerfile.dev is not gated",
			relativePath: "Dockerfile.dev",
			content:      "FROM golang:1.24\n",
			wantSkip:     false,
		},

		// --- NOT gated: extension-triggered IaC signal ---
		{
			name:         "values.yaml is not gated",
			relativePath: filepath.Join("chart-a", "values.yaml"),
			content:      "replicaCount: 1\n",
			wantSkip:     false,
		},
		{
			name:         "chart.yaml is not gated",
			relativePath: filepath.Join("chart-a", "Chart.yaml"),
			content:      "apiVersion: v2\nname: chart-a\n",
			wantSkip:     false,
		},
		{
			name:         "terraform hcl is not gated",
			relativePath: filepath.Join("infra", "main.tf"),
			content:      "resource \"aws_s3_bucket\" \"b\" {}\n",
			wantSkip:     false,
		},
		{
			name:         "jinja template is not gated",
			relativePath: filepath.Join("templates", "config.cfg.j2"),
			content:      "server_name = {{ name }}\n",
			wantSkip:     false,
		},
		{
			name:         "plain yaml at repo root is not gated",
			relativePath: filepath.Join("k8s", "deployment.yaml"),
			content:      "apiVersion: v1\nkind: ConfigMap\n",
			wantSkip:     false,
		},

		// --- NOT gated: content-triggered ansible playbook signal, no path/ext signal ---
		{
			name:         "python containing ansible playbook markers is not gated",
			relativePath: "generate_playbook.py",
			content:      "TEMPLATE = '''\nhosts: all\nroles:\n  - common\n'''\n",
			wantSkip:     false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.FromSlash(tt.relativePath)
			gotSkip := shouldSkipContentMetadata(path, tt.content)
			if gotSkip != tt.wantSkip {
				t.Fatalf("shouldSkipContentMetadata(%q) = %t, want %t", tt.relativePath, gotSkip, tt.wantSkip)
			}

			var got contentMetadata
			if gotSkip {
				got = contentMetadata{}
			} else {
				got = inferContentMetadata(path, tt.content)
			}
			want := inferContentMetadata(path, tt.content)
			if got != want {
				t.Fatalf("gate result mismatch for %q: got %+v, want %+v (unconditional inferContentMetadata)", tt.relativePath, got, want)
			}
		})
	}
}

// TestShouldSkipContentMetadataTooWideIsCaught proves the equivalence test above
// actually fails when the gate is widened to also swallow a path-triggered case.
// This guards the correctness trap called out in issue #4768: a naive
// "skip for source-code extensions" gate would break Ansible/Dagster/Helm path
// detection for .py/.js/.php files living under those directories.
func TestShouldSkipContentMetadataTooWideIsCaught(t *testing.T) {
	t.Parallel()

	// A too-wide gate that ignores path segments entirely and only looks at
	// extension. This is the exact regression the real gate must not become.
	tooWideSkip := func(path string) bool {
		suffixes := splitSuffixes(path)
		if len(suffixes) == 0 {
			return true
		}
		last := suffixes[len(suffixes)-1]
		switch last {
		case ".yaml", ".yml", ".hcl", ".tf", ".tfvars", ".tpl", ".tftpl", ".jinja", ".jinja2", ".j2":
			return false
		default:
			return true
		}
	}

	path := filepath.FromSlash(filepath.Join("roles", "web", "library", "custom_module.py"))
	content := "def main():\n    pass\n"

	if !tooWideSkip(path) {
		t.Fatalf("test setup invalid: expected the too-wide gate to skip %q", path)
	}

	got := contentMetadata{}
	want := inferContentMetadata(path, content)
	if got == want {
		t.Fatalf("expected too-wide gate mismatch to be detectable, but zero value equaled real inferContentMetadata result %+v -- test fixture no longer proves the trap", want)
	}
}
