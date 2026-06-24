package skillgen

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// repoRootDir resolves the repository root from this test file. The
// skill-fragments/ and expected/ directories live at the repo root; the
// Go module lives one level deeper at go/.
func repoRootDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
}

// loadCanonicalFragments loads the seven fragments from the repo-root
// skill-fragments/ directory. The test depends on these fragments because
// the roundtrip baseline lives next to them; if a fragment is renamed or
// removed, this helper breaks loudly instead of producing a silently
// vacuous assertion.
func loadCanonicalFragments(t *testing.T) []Fragment {
	t.Helper()
	dir := filepath.Join(repoRootDir(t), "skill-fragments")
	fragments, err := LoadFragments(dir)
	if err != nil {
		t.Fatalf("LoadFragments(%s): %v", dir, err)
	}
	if len(fragments) == 0 {
		t.Fatalf("skill-fragments/ is empty; the seven canonical fragments are required")
	}
	return fragments
}

func TestRenderAll_ProducesAllThreeHosts(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	results, err := RenderAll(fragments, DefaultCapabilities())
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	if got, want := len(results), 3; got != want {
		t.Fatalf("results = %d, want %d", got, want)
	}
	gotHosts := make([]string, 0, len(results))
	for _, r := range results {
		gotHosts = append(gotHosts, string(r.Host))
	}
	wantHosts := []string{"claude-code", "codex", "cursor"}
	sort.Strings(gotHosts)
	if !reflect.DeepEqual(gotHosts, wantHosts) {
		t.Fatalf("hosts = %v, want %v", gotHosts, wantHosts)
	}
}

func TestRenderAll_EmitsByteCitationBlockOnEveryHost(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	results, err := RenderAll(fragments, DefaultCapabilities())
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	for _, r := range results {
		if !bytes.HasPrefix(r.Bytes, []byte("<!-- eshu:byte-citation ")) {
			t.Errorf("host %s: output does not start with byte-citation block\nfirst 200 bytes: %q", r.Host, truncate(string(r.Bytes), 200))
			continue
		}
		// Every fragment's citation must appear as a comment line.
		for _, f := range fragments {
			normalized, err := NormalizeByteCitation(f.ByteCitation, f.SourcePath)
			if err != nil {
				t.Fatalf("NormalizeByteCitation: %v", err)
			}
			line := byteCitationPrefix + normalized + " -->"
			if !bytes.Contains(r.Bytes, []byte(line)) {
				t.Errorf("host %s: missing byte-citation line %q", r.Host, line)
			}
		}
	}
}

func TestRenderAll_ClaudeCodeFrontmatterHasNameAndDescription(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	results, err := RenderAll(fragments, DefaultCapabilities())
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	for _, r := range results {
		switch r.Host {
		case HostClaudeCode, HostCodex:
			if !bytes.Contains(r.Bytes, []byte("name: eshu\n")) {
				t.Errorf("host %s: output missing `name: eshu`", r.Host)
			}
			if !bytes.Contains(r.Bytes, []byte("description: |\n")) {
				t.Errorf("host %s: output missing `description: |`", r.Host)
			}
		case HostCursor:
			if !bytes.Contains(r.Bytes, []byte("alwaysApply: true\n")) {
				t.Errorf("host %s: output missing `alwaysApply: true`", r.Host)
			}
			if !bytes.Contains(r.Bytes, []byte("description: |\n")) {
				t.Errorf("host %s: output missing `description: |`", r.Host)
			}
		}
	}
}

func TestRenderAll_OutputPathsMatchTheHostMatrix(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	results, err := RenderAll(fragments, DefaultCapabilities())
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	wantPaths := map[Host]string{
		HostClaudeCode: ".claude/skills/eshu/SKILL.md",
		HostCursor:     ".cursor/rules/eshu.mdc",
		HostCodex:      ".codex/skills/eshu/SKILL.md",
	}
	for _, r := range results {
		if r.OutputPath != wantPaths[r.Host] {
			t.Errorf("host %s: OutputPath = %q, want %q", r.Host, r.OutputPath, wantPaths[r.Host])
		}
	}
}

func TestRoundtripAgainstCommittedBaseline(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	results, err := RenderAll(fragments, DefaultCapabilities())
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	expectedRoot := filepath.Join(repoRootDir(t), "expected")
	if _, err := os.Stat(expectedRoot); err != nil {
		t.Fatalf("expected/ baseline missing at %s: %v", expectedRoot, err)
	}
	drifts, err := CheckDrift(expectedRoot, results)
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if len(drifts) > 0 {
		var b strings.Builder
		b.WriteString("committed baseline drifted from the canonical fragments:\n")
		for _, d := range drifts {
			b.WriteString("  - ")
			b.WriteString(string(d.Host))
			b.WriteString(": ")
			b.WriteString(d.Path)
			b.WriteString(" (")
			b.WriteString(d.Reason)
			b.WriteString(")\n")
		}
		b.WriteString("\nrun `go run ./cmd/skillgen gen` to regenerate the baseline.\n")
		t.Fatal(b.String())
	}
}

func TestCheckDrift_DetectsContentMismatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	results := []RenderResult{{
		Host:       HostClaudeCode,
		OutputPath: ".claude/skills/eshu/SKILL.md",
		Bytes:      []byte("alpha\n"),
	}}
	if err := WriteExpected(root, results); err != nil {
		t.Fatalf("WriteExpected: %v", err)
	}
	// Mutate the on-disk baseline.
	drifted := results[0]
	drifted.Bytes = []byte("beta\n")
	drifts, err := CheckDrift(root, []RenderResult{drifted})
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if len(drifts) != 1 {
		t.Fatalf("drifts = %d, want 1", len(drifts))
	}
	if drifts[0].Reason != "content_mismatch" {
		t.Errorf("Reason = %q, want content_mismatch", drifts[0].Reason)
	}
}

func TestCheckDrift_DetectsMissingFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	results := []RenderResult{{
		Host:       HostClaudeCode,
		OutputPath: ".claude/skills/eshu/SKILL.md",
		Bytes:      []byte("alpha\n"),
	}}
	drifts, err := CheckDrift(root, results)
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if len(drifts) != 1 {
		t.Fatalf("drifts = %d, want 1", len(drifts))
	}
	if drifts[0].Reason != "missing" {
		t.Errorf("Reason = %q, want missing", drifts[0].Reason)
	}
}

func TestWriteExpected_CreatesNestedDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	results := []RenderResult{{
		Host:       HostClaudeCode,
		OutputPath: ".claude/skills/eshu/SKILL.md",
		Bytes:      []byte("hello\n"),
	}}
	if err := WriteExpected(root, results); err != nil {
		t.Fatalf("WriteExpected: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, string(HostClaudeCode), ".claude/skills/eshu/SKILL.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello\n" {
		t.Errorf("file content = %q, want hello\\n", string(got))
	}
}

func TestRenderAll_AllFragmentBodiesAppearOnEveryHost(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	results, err := RenderAll(fragments, DefaultCapabilities())
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	for _, r := range results {
		// Each fragment body is a Markdown document; the rendered skill
		// must include a recognizable substring from each body. We use the
		// first sentence of each body as the discriminator.
		for _, f := range fragments {
			marker := firstSentence(f.Body)
			if marker == "" {
				continue
			}
			if !bytes.Contains(r.Bytes, []byte(marker)) {
				t.Errorf("host %s: missing fragment %s body marker %q", r.Host, f.ID, marker)
			}
		}
	}
}

func firstSentence(body string) string {
	// Strip the leading H1 and any leading whitespace, then take the first
	// non-empty line as the marker. This is intentionally simple; the
	// fragments are author-controlled.
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "# "))
		if trimmed == "" || strings.HasPrefix(trimmed, "# ") {
			continue
		}
		return trimmed
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
