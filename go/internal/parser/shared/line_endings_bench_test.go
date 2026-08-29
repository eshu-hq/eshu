// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"strings"
	"testing"
)

// benchBody renders a source file of roughly 64 KiB using the given line
// terminator, which is a large file by parser-input standards (the median
// repository source file is well under 8 KiB).
func benchBody(eol string) []byte {
	line := "func handler(ctx context.Context, request *Request) (*Response, error) {"
	var builder strings.Builder
	for builder.Len() < 64*1024 {
		builder.WriteString(line)
		builder.WriteString(eol)
	}
	return []byte(builder.String())
}

// BenchmarkNormalizeLineEndings measures the work ReadSource added per call,
// separated by line-ending style. LF and CRLF are the no-copy paths every
// real-world file takes; bare CR is the one that allocates.
func BenchmarkNormalizeLineEndings(b *testing.B) {
	for _, variant := range []struct {
		name string
		eol  string
	}{
		{name: "lf_no_copy", eol: "\n"},
		{name: "crlf_no_copy", eol: "\r\n"},
		{name: "bare_cr_copies", eol: "\r"},
	} {
		body := benchBody(variant.eol)
		b.Run(variant.name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if got := NormalizeLineEndings(body); len(got) != len(body) {
					b.Fatalf("length changed: %d != %d", len(got), len(body))
				}
			}
		})
	}
}
