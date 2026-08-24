// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// buildSyntheticFastifyFixture returns ~3000 lines of JavaScript with a
// Fastify import, many routes, and many handlers, so the 3x-vs-1x base
// computation difference is measurable in the benchmark.
func buildSyntheticFastifyFixture() string {
	var b strings.Builder
	b.WriteString(`import fastify from "fastify";
const app = fastify();
`)
	n := 500
	for i := 0; i < n; i++ {
		suffix := strconv.Itoa(i)
		b.WriteString("app.get(\"/route")
		b.WriteString(suffix)
		b.WriteString("\", handler")
		b.WriteString(suffix)
		b.WriteString(");\n")
	}
	for i := 0; i < n; i++ {
		suffix := strconv.Itoa(i)
		b.WriteString("app.post(\"/route")
		b.WriteString(suffix)
		b.WriteString("\", handler")
		b.WriteString(suffix)
		b.WriteString(");\n")
	}
	for i := 0; i < n; i++ {
		suffix := strconv.Itoa(i)
		b.WriteString("app.put(\"/route")
		b.WriteString(suffix)
		b.WriteString("\", handler")
		b.WriteString(suffix)
		b.WriteString(");\n")
	}
	for i := 0; i < n; i++ {
		suffix := strconv.Itoa(i)
		b.WriteString("function handler")
		b.WriteString(suffix)
		b.WriteString("(req, reply) {\n  reply.send(\"ok\");\n")
		b.WriteString("  const x = req.params.id;\n")
		b.WriteString("  return { status: \"ok\" };\n")
		b.WriteString("}\n\n")
	}
	return b.String()
}

// BenchmarkParseFastifyFixture parses a synthetic Fastify-heavy fixture once.
// It establishes the pre-threading baseline so the before/after delta is measured
// against the exact same input shape.
func BenchmarkParseFastifyFixture(b *testing.B) {
	engine, err := DefaultEngine()
	if err != nil {
		b.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	fixture := buildSyntheticFastifyFixture()
	b.Logf("fixture size: %d bytes, ~%d lines", len(fixture), strings.Count(fixture, "\n"))

	dir := b.TempDir()
	filePath := filepath.Join(dir, "app.js")
	if err := os.WriteFile(filePath, []byte(fixture), 0o644); err != nil {
		b.Fatalf("WriteFile() error = %v, want nil", err)
	}

	b.ResetTimer()
	for b.Loop() {
		_, err := engine.ParsePath(dir, filePath, false, Options{})
		if err != nil {
			b.Fatalf("ParsePath() error = %v, want nil", err)
		}
	}
}
