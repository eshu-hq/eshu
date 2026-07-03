// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package envregistry

import (
	"strings"
	"testing"
)

// TestRenderMarkdownSingleTrailingNewline asserts the rendered document ends
// with exactly one newline. The committed doc is compared byte-for-byte by
// TestEnvRegistryReferenceDocUpToDate, and CI's whitespace gate
// (git show --check) rejects a blank line at EOF, so a double trailing
// newline makes the generated doc uncommittable.
func TestRenderMarkdownSingleTrailingNewline(t *testing.T) {
	got := Default().RenderMarkdown()
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("RenderMarkdown output must end with a newline")
	}
	if strings.HasSuffix(got, "\n\n") {
		t.Fatalf("RenderMarkdown output ends with a blank line at EOF; the whitespace gate (git show --check) rejects it")
	}
}
