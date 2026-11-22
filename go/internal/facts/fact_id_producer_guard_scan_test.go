// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// goSourceRoot returns the go/ directory this test file lives under.
func goSourceRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// .../go/internal/facts/<this file>
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// readProductionGoFiles returns every non-test Go file under root, keyed by
// slash path relative to root. The vendor and testdata trees are skipped.
func readProductionGoFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if base := entry.Name(); base == "vendor" || base == "testdata" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		out[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// TestFactIDProducersDeriveFromScopeAndGeneration is the GREEN half of the
// #11203 guard: on the real tree every Envelope literal that sets FactID
// either hashes a scope and a generation or has a reasoned allowlist row, and
// no allowlist row is stale.
func TestFactIDProducersDeriveFromScopeAndGeneration(t *testing.T) {
	files := readProductionGoFiles(t, goSourceRoot(t))
	violations, sites, used, err := factIDProducerViolations(files, factIDProducerAllowlist)
	if err != nil {
		t.Fatalf("factIDProducerViolations() error = %v", err)
	}
	// Floor, not an exact count: a scan that parsed nothing must not pass.
	if sites < 25 {
		t.Fatalf("scanned %d FactID sites, want at least 25: the scan lost the tree", sites)
	}
	for _, violation := range violations {
		t.Errorf("%s", violation)
	}
	var stale []string
	for file, reason := range factIDProducerAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("allowlist row %q has no reason", file)
		}
		if !used[file] {
			stale = append(stale, file)
		}
	}
	sort.Strings(stale)
	for _, file := range stale {
		t.Errorf("allowlist row %q matches no unscoped FactID site; delete it", file)
	}
}

// TestFactIDProducerGuardSeededViolations is the RED half: the guard must
// fail on a planted violation. The two semantic emitters are the original
// #11203 offenders, so the seed rewrites each real emitter back to the bare
// stable key and requires the guard to flag exactly those two files, while
// the unmodified emitters stay clean.
func TestFactIDProducerGuardSeededViolations(t *testing.T) {
	root := goSourceRoot(t)
	emitters := []string{
		"internal/semanticdocs/emitter.go",
		"internal/semanticcode/emitter.go",
	}
	all := readProductionGoFiles(t, root)

	clean := map[string]string{}
	seeded := map[string]string{}
	for _, name := range emitters {
		body, ok := all[name]
		if !ok {
			t.Fatalf("emitter %s not found under %s", name, root)
		}
		clean[name] = body
		start := strings.Index(body, "FactID:")
		if start < 0 {
			t.Fatalf("%s has no FactID literal to seed", name)
		}
		end := start + strings.Index(body[start:], "\n")
		// Plant the exact original defect: the stable key alone as the id.
		seeded[name] = body[:start] + "FactID:           stableKey," + body[end:]
	}

	// The seed needs the sibling helper files the scan resolves through.
	for name, body := range all {
		if strings.HasPrefix(name, "internal/semanticdocs/") || strings.HasPrefix(name, "internal/semanticcode/") {
			if _, isEmitter := clean[name]; !isEmitter {
				clean[name] = body
				seeded[name] = body
			}
		}
	}

	if violations, sites, _, err := factIDProducerViolations(clean, nil); err != nil || len(violations) != 0 || sites != 2 {
		t.Fatalf("clean emitters: violations=%v sites=%d err=%v, want none, 2, nil", violations, sites, err)
	}
	violations, sites, _, err := factIDProducerViolations(seeded, nil)
	if err != nil {
		t.Fatalf("seeded scan error = %v", err)
	}
	if sites != 2 || len(violations) != 2 {
		t.Fatalf("seeded emitters: sites=%d violations=%v, want 2 sites and 2 violations", sites, violations)
	}
	for _, name := range emitters {
		found := false
		for _, violation := range violations {
			found = found || strings.HasPrefix(violation, name+":")
		}
		if !found {
			t.Errorf("seeded violation in %s not reported: %v", name, violations)
		}
	}
}

// TestFactIDProducerGuardShapes pins the classifier on small planted sources.
func TestFactIDProducerGuardShapes(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int // violations
	}{
		{"scoped hash", `package p
func f(scopeID, generationID, key string) facts.Envelope {
	return facts.Envelope{FactID: facts.StableID("K", map[string]any{"k": key, "scope_id": scopeID, "generation_id": generationID})}
}`, 0},
		{"hash omits generation", `package p
func f(scopeID, key string) facts.Envelope {
	return facts.Envelope{FactID: facts.StableID("K", map[string]any{"k": key, "scope_id": scopeID})}
}`, 1},
		{"bare key", `package p
func f(key string) facts.Envelope { return facts.Envelope{FactID: key} }`, 1},
		{"local var hashed without generation", `package p
func f(scopeID, key string) facts.Envelope {
	id := facts.StableID("K", map[string]any{"k": key, "scope": scopeID})
	return facts.Envelope{FactID: id}
}`, 1},
		{"local var hashed with generation", `package p
func f(scopeID, generationID, key string) facts.Envelope {
	id := facts.StableID("K", map[string]any{"k": key, "scope": scopeID, "generation": generationID})
	return facts.Envelope{FactID: id}
}`, 0},
		{"helper ignores scope", `package p
func mint(kind, key, scopeID, generationID string) string { return facts.StableID(kind, key) }
func f(scopeID, generationID, key string) facts.Envelope {
	return facts.Envelope{FactID: mint("K", key, scopeID, generationID)}
}`, 1},
		{"helper uses scope", `package p
func mint(kind, key, scopeID, generationID string) string {
	return facts.StableID(kind, map[string]any{"k": key, "scope_id": scopeID, "generation_id": generationID})
}
func f(scopeID, generationID, key string) facts.Envelope {
	return facts.Envelope{FactID: mint("K", key, scopeID, generationID)}
}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations, sites, _, err := factIDProducerViolations(map[string]string{"p/p.go": tc.src}, nil)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if sites != 1 || len(violations) != tc.want {
				t.Fatalf("sites=%d violations=%v, want 1 site and %d violations", sites, violations, tc.want)
			}
		})
	}
}
