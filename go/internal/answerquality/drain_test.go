// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerquality

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestQueryRootDrained fails while any file in this package still imports the
// handler-heavy query root (row-8 remainder of
// REMOVAL_CONSUMER_DRAIN_CHECKLIST.md, per the arbiter ruling). Truth
// envelopes and truth classes come from querycontract, answer packets from
// querycontract/answer, and playbook existence from playbook. Unlike the
// ask/engine and serviceintel precedents this census also covers _test.go
// files: the ruling repoints this package's tests too, so a test re-importing
// the root must fail here rather than drift back silently.
func TestQueryRootDrained(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == "github.com/eshu-hq/eshu/go/internal/query" {
				t.Errorf("%s still imports the query root; retarget to querycontract, querycontract/answer, and playbook", name)
			}
		}
	}
}
