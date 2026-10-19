// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// This benchmark lives in the parent package (not internal/parser/content)
// because BenchmarkParsePathLargeGatedPHP exercises the ParsePath call site
// in engine.go, not the content package in isolation.

package parser

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	contentparser "github.com/eshu-hq/eshu/go/internal/parser/content"
)

// BenchmarkInferContentMetadataUnconditional measures the "before" cost of
// issue #4768: Infer's three regex scans (goLineControlRE,
// tfTemplatefileRE, and inferRootFamily's internal re-scan) running on every
// file regardless of any IaC/template signal.
func BenchmarkInferContentMetadataUnconditional(b *testing.B) {
	path := filepath.Join("src", "app", "LargeController.php")
	content := generateLargeGatedSource(400)

	b.ResetTimer()
	for b.Loop() {
		_ = contentparser.Infer(path, content)
	}
}

// BenchmarkContentMetadataGated measures the "after" cost: the gate itself
// (extension/basename/path-segment/content-marker checks) plus, since the
// file is gated, using the Metadata{} zero value in place of the
// unconditional call. This is the direct before/after pair for the #4768 fix.
func BenchmarkContentMetadataGated(b *testing.B) {
	path := filepath.Join("src", "app", "LargeController.php")
	content := generateLargeGatedSource(400)

	b.ResetTimer()
	for b.Loop() {
		var metadata contentparser.Metadata
		if !contentparser.ShouldSkip(path, content) {
			metadata = contentparser.Infer(path, content)
		}
		_ = metadata
	}
}

// BenchmarkParsePathLargeGatedPHP is the end-to-end call-site benchmark: a
// large PHP file at a path carrying no IaC/template signal, parsed through
// the real ParsePath entrypoint that engine.go:76-85 gates. This is the
// proof that the gate saves real wall time on the actual call-site, not just
// in isolation.
func BenchmarkParsePathLargeGatedPHP(b *testing.B) {
	repoRoot := b.TempDir()
	filePath := filepath.Join(repoRoot, "large_controller.php")
	writeBenchFile(b, filePath, generateLargeGatedSource(400))

	engine, err := DefaultEngine()
	if err != nil {
		b.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	b.ResetTimer()
	for b.Loop() {
		if _, err := engine.ParsePath(repoRoot, filePath, false, Options{}); err != nil {
			b.Fatalf("ParsePath() error = %v, want nil", err)
		}
	}
}

// BenchmarkContentMetadataGatedRawConfigCorrectlyNotSkipped is the
// post-eshu-code-review correction benchmark: a large ".conf" file (nginx
// config shape) previously fell through the buggy gate's extension check
// (".conf"/".cfg"/".cnf" were entirely missing) and was incorrectly skipped,
// silently corrupting its persisted artifact_type/iac_relevant. The fixed
// gate correctly declines to skip this file, so this benchmark measures the
// real cost the fix restores for this class: gate-check overhead plus the
// full Infer call, not the near-zero skip cost. This is the
// "more files correctly run the real inference now" case the coordinator
// asked to be re-measured, isolated from the still-correctly-gated PHP case
// above.
func BenchmarkContentMetadataGatedRawConfigCorrectlyNotSkipped(b *testing.B) {
	path := filepath.Join("etc", "nginx", "sites-available", "large.conf")
	content := generateLargeNginxConfSource(400)

	if contentparser.ShouldSkip(path, content) {
		b.Fatalf("ShouldSkip(%q) = true, want false (a .conf file must never be skipped)", path)
	}

	b.ResetTimer()
	for b.Loop() {
		var metadata contentparser.Metadata
		if !contentparser.ShouldSkip(path, content) {
			metadata = contentparser.Infer(path, content)
		}
		_ = metadata
	}
}

// generateLargeNginxConfSource produces a large synthetic nginx-shaped ".conf"
// file: real IaC content (server{}/location/ blocks) that must persist
// artifact_type=nginx_config, iac_relevant=true, and therefore must never be
// skipped by ShouldSkip.
func generateLargeNginxConfSource(blockCount int) string {
	var b strings.Builder
	b.WriteString("user www-data;\nworker_processes auto;\n\n")
	for i := range blockCount {
		fmt.Fprintf(&b, "server {\n  listen 80%02d;\n  server_name app%d.internal;\n", i%100, i)
		fmt.Fprintf(&b, "  location /svc%d/ {\n    proxy_pass http://upstream%d;\n  }\n}\n\n", i, i)
	}
	return b.String()
}

// generateLargeGatedSource produces a large synthetic PHP file with no
// IaC/template signal in its path or content (no roles/playbooks/dagster/
// chart/templates/argocd/iac path segment, no ansible-playbook content
// marker, and a .php extension outside contentMetadataGatedExtensions), so
// ShouldSkip always skips it. methodCount controls file size;
// 400 methods produces a file large enough to make Infer's
// regex scans measurable against total parse cost.
func generateLargeGatedSource(methodCount int) string {
	var b strings.Builder
	b.WriteString("<?php\n")
	b.WriteString("namespace App\\Http\\Controllers;\n\n")
	b.WriteString("final class LargeController {\n")
	b.WriteString("    private Service $service;\n\n")
	for i := range methodCount {
		fmt.Fprintf(&b, "    public function show%d(int $id): string {\n", i)
		fmt.Fprintf(&b, "        $result = $this->service->render(%d, $id);\n", i)
		b.WriteString("        return $result;\n")
		b.WriteString("    }\n\n")
	}
	b.WriteString("}\n")
	return b.String()
}
