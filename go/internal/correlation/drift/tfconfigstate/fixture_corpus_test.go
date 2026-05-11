package tfconfigstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureCase describes one positive/negative/ambiguous fixture file. The
// classifier walks fixtures under testdata/<drift_kind>/<case>.json and
// asserts that Classify returns ExpectedDriftKind.
type fixtureCase struct {
	Description       string       `json:"description"`
	Config            *ResourceRow `json:"config,omitempty"`
	State             *ResourceRow `json:"state,omitempty"`
	Prior             *ResourceRow `json:"prior,omitempty"`
	ExpectedDriftKind string       `json:"expected_drift_kind"`
}

// TestFixtureCorpus walks the testdata/ tree and runs every fixture through
// Classify. Each drift-kind subdirectory must carry positive.json,
// negative.json, and ambiguous.json per the eshu-correlation-truth skill's
// fixture discipline (15 fixtures minimum).
func TestFixtureCorpus(t *testing.T) {
	t.Parallel()

	root := "testdata"
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read testdata dir: %v", err)
	}

	required := map[string]struct{}{
		"positive.json":  {},
		"negative.json":  {},
		"ambiguous.json": {},
	}

	seenKinds := map[string]int{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		kindDir := filepath.Join(root, e.Name())
		seenFiles := map[string]struct{}{}
		files, err := os.ReadDir(kindDir)
		if err != nil {
			t.Fatalf("read %s: %v", kindDir, err)
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			seenFiles[f.Name()] = struct{}{}
			runFixture(t, kindDir, f.Name())
			seenKinds[e.Name()]++
		}
		for r := range required {
			if _, ok := seenFiles[r]; !ok {
				t.Errorf("drift kind %q missing required fixture %q", e.Name(), r)
			}
		}
	}

	// Every drift kind must be represented; the test asserts the corpus is
	// at least 15 cases (5 kinds * 3 cases).
	if got := totalFixtureCount(seenKinds); got < 15 {
		t.Fatalf("total fixture count = %d, want >= 15", got)
	}
	for _, k := range AllDriftKinds() {
		if seenKinds[string(k)] < 3 {
			t.Errorf("drift kind %q has %d fixtures, want >= 3", k, seenKinds[string(k)])
		}
	}
}

func totalFixtureCount(m map[string]int) int {
	total := 0
	for _, n := range m {
		total += n
	}
	return total
}

func runFixture(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var fc fixtureCase
	if err := json.Unmarshal(data, &fc); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", path, err)
	}
	got := Classify(fc.Config, fc.State, fc.Prior)
	if string(got) != fc.ExpectedDriftKind {
		t.Errorf("Classify(%s) = %q, want %q (%s)", path, got, fc.ExpectedDriftKind, fc.Description)
	}
}
