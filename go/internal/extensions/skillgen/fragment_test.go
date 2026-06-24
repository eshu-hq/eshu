package skillgen

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// writeFragmentFile writes a fragment file under dir/name with the given
// body and returns the absolute path.
func writeFragmentFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fragment %s: %v", path, err)
	}
	return path
}

func TestLoadFragments_ParsesFrontmatter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFragmentFile(t, dir, "alpha.md", `---
id: alpha
version: 1.0.0
requires:
  - beta
byte_citation: docs/foo.md#1-5
description: |
  The alpha fragment is the first one.
---

# Alpha

This is the body of alpha.
`)

	fragments, err := LoadFragments(dir)
	if err != nil {
		t.Fatalf("LoadFragments: %v", err)
	}
	if got, want := len(fragments), 1; got != want {
		t.Fatalf("fragment count = %d, want %d", got, want)
	}
	f := fragments[0]
	if f.ID != "alpha" {
		t.Errorf("ID = %q, want alpha", f.ID)
	}
	if f.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", f.Version)
	}
	if !reflect.DeepEqual(f.Requires, []string{"beta"}) {
		t.Errorf("Requires = %v, want [beta]", f.Requires)
	}
	if f.ByteCitation != "docs/foo.md#1-5" {
		t.Errorf("ByteCitation = %q, want docs/foo.md#1-5", f.ByteCitation)
	}
	if !strings.Contains(f.Description, "alpha fragment") {
		t.Errorf("Description = %q, want contains 'alpha fragment'", f.Description)
	}
	if !strings.Contains(f.Body, "This is the body of alpha.") {
		t.Errorf("Body = %q, want contains body text", f.Body)
	}
	if !strings.HasPrefix(f.Body, "# Alpha") {
		t.Errorf("Body = %q, want starts with # Alpha", f.Body)
	}
}

func TestLoadFragments_SortsByID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFragmentFile(t, dir, "zeta.md", minimalFragment("zeta", "docs/z.md#1-1"))
	writeFragmentFile(t, dir, "alpha.md", minimalFragment("alpha", "docs/a.md#1-1"))
	writeFragmentFile(t, dir, "mu.md", minimalFragment("mu", "docs/m.md#1-1"))

	fragments, err := LoadFragments(dir)
	if err != nil {
		t.Fatalf("LoadFragments: %v", err)
	}
	got := make([]string, 0, len(fragments))
	for _, f := range fragments {
		got = append(got, f.ID)
	}
	want := []string{"alpha", "mu", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fragment ids = %v, want %v", got, want)
	}
}

func TestLoadFragments_SkipsNonMarkdown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFragmentFile(t, dir, "alpha.md", minimalFragment("alpha", "docs/a.md#1-1"))
	writeFragmentFile(t, dir, "README", "not a fragment")
	writeFragmentFile(t, dir, "config.yaml", "id: alpha")

	fragments, err := LoadFragments(dir)
	if err != nil {
		t.Fatalf("LoadFragments: %v", err)
	}
	if got, want := len(fragments), 1; got != want {
		t.Fatalf("fragment count = %d, want %d", got, want)
	}
	if fragments[0].ID != "alpha" {
		t.Errorf("ID = %q, want alpha", fragments[0].ID)
	}
}

func TestLoadFragments_RequiresMissingByteCitationFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFragmentFile(t, dir, "alpha.md", `---
id: alpha
version: 1.0.0
description: no citation
---

# Alpha
`)
	_, err := LoadFragments(dir)
	if err == nil {
		t.Fatal("LoadFragments() error = nil, want ErrFragmentMissingByteCitation")
	}
	if !errors.Is(err, ErrFragmentMissingByteCitation) {
		t.Fatalf("error = %v, want ErrFragmentMissingByteCitation", err)
	}
}

func TestLoadFragments_DuplicateIDFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFragmentFile(t, dir, "alpha-1.md", minimalFragment("alpha", "docs/a.md#1-1"))
	writeFragmentFile(t, dir, "alpha-2.md", minimalFragment("alpha", "docs/a.md#2-3"))

	_, err := LoadFragments(dir)
	if err == nil {
		t.Fatal("LoadFragments() error = nil, want duplicate id error")
	}
	if !strings.Contains(err.Error(), "duplicate fragment id") {
		t.Fatalf("error = %v, want contains 'duplicate fragment id'", err)
	}
}

func TestLoadFragments_MissingFrontmatterFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFragmentFile(t, dir, "alpha.md", "# No frontmatter here\n")

	_, err := LoadFragments(dir)
	if err == nil {
		t.Fatal("LoadFragments() error = nil, want missing frontmatter error")
	}
}

func TestLoadFragments_StripsLeadingH1FromBodyPreservesIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The body is the rendered content after the closing "---". The fragment
	// contract intentionally keeps the H1 inside the body; the host adapter is
	// responsible for re-sectioning if needed.
	writeFragmentFile(t, dir, "alpha.md", `---
id: alpha
version: 1.0.0
byte_citation: docs/a.md#1-1
description: alpha
---

# Alpha Title

Body paragraph.
`)
	fragments, err := LoadFragments(dir)
	if err != nil {
		t.Fatalf("LoadFragments: %v", err)
	}
	if !strings.HasPrefix(fragments[0].Body, "# Alpha Title") {
		t.Errorf("Body = %q, want starts with # Alpha Title", fragments[0].Body)
	}
}

func TestFragment_IDsAreStable(t *testing.T) {
	t.Parallel()
	// The seven canonical fragment ids must all be present and sorted
	// deterministically. This guards against a renames-and-shipping
	// change.
	dir := t.TempDir()
	ids := []string{
		"operating-standard",
		"truth-labels",
		"capability-profiles",
		"reducer-invariant",
		"local-first",
		"bundle-reproduction",
		"per-collector-matrix",
	}
	sort.Strings(ids)
	for _, id := range ids {
		writeFragmentFile(t, dir, id+".md", minimalFragment(id, "docs/"+id+".md#1-1"))
	}
	fragments, err := LoadFragments(dir)
	if err != nil {
		t.Fatalf("LoadFragments: %v", err)
	}
	got := make([]string, 0, len(fragments))
	for _, f := range fragments {
		got = append(got, f.ID)
	}
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("ids = %v, want %v", got, ids)
	}
}

func minimalFragment(id, citation string) string {
	return "---\n" +
		"id: " + id + "\n" +
		"version: 1.0.0\n" +
		"byte_citation: " + citation + "\n" +
		"description: test fragment " + id + "\n" +
		"---\n\n" +
		"# " + id + "\n"
}
