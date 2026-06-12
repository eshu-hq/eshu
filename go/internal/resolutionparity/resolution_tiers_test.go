package resolutionparity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// entityBuckets are the parsed_file_data buckets whose items the reducer indexes
// by uid. The parser emits names and line spans but assigns uids downstream in
// the ingester, so the golden harness injects deterministic synthetic uids to
// stand in for content-entity identity.
var entityBuckets = []string{"functions", "classes", "structs", "interfaces", "type_aliases"}

func assignSyntheticUIDs(parsed map[string]any, relativePath string) {
	for _, bucket := range entityBuckets {
		switch typed := parsed[bucket].(type) {
		case []map[string]any:
			for i := range typed {
				ensureSyntheticUID(typed[i], bucket, relativePath)
			}
		case []any:
			for _, item := range typed {
				if asMap, ok := item.(map[string]any); ok {
					ensureSyntheticUID(asMap, bucket, relativePath)
				}
			}
		}
	}
}

func ensureSyntheticUID(item map[string]any, bucket string, relativePath string) {
	if existing, _ := item["uid"].(string); strings.TrimSpace(existing) != "" {
		return
	}
	name, _ := item["name"].(string)
	item["uid"] = fmt.Sprintf("ent:%s:%s:%s:%v", relativePath, bucket, name, item["line_number"])
}

// goldenPath is the checked-in per-language resolution-tier distribution.
const goldenPath = "testdata/resolution_tiers.golden.json"

// languageFixtures pins which sample-project corpus and file extensions feed
// each language's resolution-tier tally. Keep the set small and deterministic;
// these corpora are the parser fixtures already used elsewhere.
var languageFixtures = []struct {
	language string
	dir      string
	exts     []string
}{
	{"go", "sample_project_go", []string{".go"}},
	{"python", "sample_project", []string{".py"}},
	{"typescript", "sample_project_typescript", []string{".ts", ".tsx"}},
	{"java", "sample_project_java", []string{".java"}},
}

// tallyResolutionTiers parses every matching file in one language corpus with
// the real parser engine, runs the reducer's code-call extraction, and counts
// the resolution_method of each emitted row. A row that somehow lacks a method
// is tallied as unspecified so a dropped-method regression is visible.
func tallyResolutionTiers(t *testing.T, dir string, exts []string) map[string]int {
	t.Helper()

	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", "tests", "fixtures", "sample_projects", dir))
	if err != nil {
		t.Fatalf("filepath.Abs(%q) error = %v", dir, err)
	}
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v", err)
	}

	var paths []string
	walkErr := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		for _, ext := range exts {
			if strings.EqualFold(filepath.Ext(path), ext) {
				paths = append(paths, path)
				break
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %q error = %v", repoRoot, walkErr)
	}
	sort.Strings(paths)

	envelopes := make([]facts.Envelope, 0, len(paths))
	for _, path := range paths {
		parsed, err := engine.ParsePath(repoRoot, path, false, parser.Options{})
		if err != nil || parsed == nil {
			continue
		}
		relativePath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relativePath = path
		}
		assignSyntheticUIDs(parsed, relativePath)
		envelopes = append(envelopes, facts.Envelope{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "resolutionparity-" + dir,
				"relative_path":    relativePath,
				"parsed_file_data": parsed,
			},
		})
	}

	tally := make(map[string]int)
	_, rows := reducer.ExtractCodeCallRows(envelopes)
	for _, row := range rows {
		method, _ := row["resolution_method"].(string)
		if strings.TrimSpace(method) == "" {
			method = codeprovenance.MethodUnspecified
		}
		tally[method]++
	}
	return tally
}

// TestResolutionTierGoldens is the per-language resolution parity gate. It runs
// in the normal CI matrix and fails when the resolution-tier distribution drifts
// from the checked-in golden. Set ESHU_UPDATE_RESOLUTION_GOLDENS=1 to rewrite
// the golden after an intended, explained tier change.
func TestResolutionTierGoldens(t *testing.T) {
	t.Parallel()

	got := make(map[string]map[string]int, len(languageFixtures))
	for _, fixture := range languageFixtures {
		tally := tallyResolutionTiers(t, fixture.dir, fixture.exts)
		// Every classified method emitted MUST be in the closed vocabulary.
		for method := range tally {
			if !codeprovenance.Valid(method) {
				t.Errorf("language %q emitted unknown resolution_method %q (not in ADR #2222 vocabulary)", fixture.language, method)
			}
		}
		got[fixture.language] = tally
	}

	if os.Getenv("ESHU_UPDATE_RESOLUTION_GOLDENS") == "1" {
		writeGolden(t, got)
		t.Log("updated resolution-tier golden")
		return
	}

	want := readGolden(t)
	for _, fixture := range languageFixtures {
		assertTallyEqual(t, fixture.language, want[fixture.language], got[fixture.language])
	}
}

func assertTallyEqual(t *testing.T, language string, want, got map[string]int) {
	t.Helper()
	if want == nil {
		t.Fatalf("golden missing language %q; regenerate with ESHU_UPDATE_RESOLUTION_GOLDENS=1", language)
	}
	keys := map[string]struct{}{}
	for k := range want {
		keys[k] = struct{}{}
	}
	for k := range got {
		keys[k] = struct{}{}
	}
	for method := range keys {
		if want[method] != got[method] {
			t.Errorf("language %q tier %q = %d, want %d (resolution regression; if intended, regenerate with ESHU_UPDATE_RESOLUTION_GOLDENS=1)",
				language, method, got[method], want[method])
		}
	}
}

func readGolden(t *testing.T) map[string]map[string]int {
	t.Helper()
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %q error = %v; regenerate with ESHU_UPDATE_RESOLUTION_GOLDENS=1", goldenPath, err)
	}
	var golden map[string]map[string]int
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("unmarshal golden error = %v", err)
	}
	return golden
}

func writeGolden(t *testing.T, golden map[string]map[string]int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
		t.Fatalf("mkdir testdata error = %v", err)
	}
	raw, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden error = %v", err)
	}
	if err := os.WriteFile(goldenPath, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write golden error = %v", err)
	}
}
