// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/docs"
)

// TestPipConstraintsFilesAreParserInputNotDocuments proves a pip constraints
// file is routed to the requirements parser and not also emitted as a text
// document (#8821), the way requirements files already are. A plain notes file
// and look-alike names stay documents.
func TestPipConstraintsFilesAreParserInputNotDocuments(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"constraints.txt",
		"deploy/Constraints.TXT",
		"constraints-dev.txt",
		"constraints_prod.txt",
		"requirements.txt",
	} {
		if _, _, ok := docs.GitDocumentationSourceURIAndFormat(path); ok {
			t.Errorf("GitDocumentationSourceURIAndFormat(%q) ok = true, want false: parser input is not a document", path)
		}
	}
	for _, path := range []string{"notes.txt", "my-constraints.txt", "docs/constraints-explained.md"} {
		if _, _, ok := docs.GitDocumentationSourceURIAndFormat(path); !ok {
			t.Errorf("GitDocumentationSourceURIAndFormat(%q) ok = false, want true: still a document", path)
		}
	}
}
