// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPythonEmitsRationaleComments(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "cache.py")
	source := `# NOTE: unrelated module comment

# WHY: memoize because recompute is expensive
def expensive():
    return 1


# HACK: works around upstream bug 123
class Widget:
    pass
`
	if err := os.WriteFile(filePath, []byte(source), 0o644); err != nil {
		t.Fatalf("write source error = %v", err)
	}

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	parsed, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	fn := findNamedItem(t, parsed, "functions", "expensive")
	assertRationale(t, fn, "WHY", "memoize because recompute is expensive")

	class := findNamedItem(t, parsed, "classes", "Widget")
	assertRationale(t, class, "HACK", "works around upstream bug 123")
}

func findNamedItem(t *testing.T, parsed map[string]any, bucket string, name string) map[string]any {
	t.Helper()
	items, ok := parsed[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s type = %T, want []map[string]any", bucket, parsed[bucket])
	}
	for _, item := range items {
		if item["name"] == name {
			return item
		}
	}
	t.Fatalf("%s %q not found in %#v", bucket, name, items)
	return nil
}

func assertRationale(t *testing.T, item map[string]any, wantKind string, wantText string) {
	t.Helper()
	rationale, ok := item["rationale_comments"].([]map[string]any)
	if !ok {
		t.Fatalf("rationale_comments type = %T, want []map[string]any (item=%#v)", item["rationale_comments"], item)
	}
	if len(rationale) != 1 {
		t.Fatalf("len(rationale_comments) = %d, want 1 (%#v)", len(rationale), rationale)
	}
	if got := rationale[0]["kind"]; got != wantKind {
		t.Errorf("rationale kind = %#v, want %#v", got, wantKind)
	}
	if got := rationale[0]["text"]; got != wantText {
		t.Errorf("rationale text = %#v, want %#v", got, wantText)
	}
}
