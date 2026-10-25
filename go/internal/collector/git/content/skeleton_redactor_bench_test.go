// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"strings"
	"testing"
)

// BenchmarkRedactSkeleton measures the skeleton redactor on a typical tfvars
// body, a 1 MiB body of allowlisted values, and a 1 MiB body where every fifth
// line is a detector finding. Like the marker redactor it runs twice per
// governed file per collection, so its cost scales with governed bytes.
func BenchmarkRedactSkeleton(b *testing.B) {
	typical := "region = \"us-east-1\"\ninstance_count = 3\nenabled = true\nengine_version = \"14.7\"\ndb_password = \"abcdef0123456789\"\ntags = {\n  Name = \"web\"\n}\n"
	clean := strings.Repeat("instance_count = 3\nenabled = true\n", 1<<20/34)
	dense := strings.Repeat("a = 1\nb = true\nc = var.x\nd = \"1.2.3\"\ndb_password = \"abcdef0123456789\"\n", 1<<20/72)
	// nested_interp_1MiB_open is the sticky-failure path: 349,525 `"${` opens
	// pass maxScanDepth on the first line and every later byte is skipped.
	// heredoc_4MiB is an unclosed heredoc at MaxGovernedBodyBytes, whose lines
	// are scanned as template text and stored whole.
	nested := strings.Repeat(`"${`, 1<<20/3)
	heredoc := "x = <<EOF\n" + strings.Repeat("some heredoc text, a line of it\n", MaxGovernedBodyBytes/32-1)
	for name, body := range map[string]string{
		"typical": typical, "clean_1MiB": clean, "dense_1MiB": dense,
		"nested_interp_1MiB_open": nested, "heredoc_4MiB": heredoc,
	} {
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := redactSkeleton(body); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkRedactMarkerURLKeys measures the marker redactor with the
// .yarnrc.yml URL keys listed, on the same typical body BenchmarkRedactMarker
// uses but with userinfo in the registry URL.
func BenchmarkRedactMarkerURLKeys(b *testing.B) {
	keys := []string{"npmAuthToken", "npmAuthIdent"}
	urlKeys := []string{"npmRegistryServer", "npmPublishRegistry", "httpProxy", "httpsProxy"}
	body := "nodeLinker: node-modules\nnpmRegistryServer: \"https://user:pass@registry.example.com/\"\nnpmAuthToken: abcdef0123456789\nenableTelemetry: false\n"
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := redactMarkerWithURLKeys(body, keys, urlKeys); err != nil {
			b.Fatal(err)
		}
	}
}
