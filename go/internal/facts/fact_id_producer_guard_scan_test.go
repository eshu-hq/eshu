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
	// Floor, not an exact count: a scan that parsed little must not pass. A scan
	// of zero sites is already an error from the scanner itself.
	if sites < 25 {
		t.Fatalf("scanned %d FactID sites, want at least 25: the scan lost the tree", sites)
	}
	for _, violation := range violations {
		t.Errorf("%s", violation)
	}
	var stale []string
	for file, reason := range factIDProducerAllowlist {
		if !strings.Contains(file, "::") {
			t.Errorf("allowlist key %q is not file::function", file)
		}
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

// TestFactIDProducerGuardAllowlistIsPerFunction pins the file-plus-function
// key (#11203 ruling): exempting one minting function must not hide another
// function in the same file, and an exempted row that matches no site is
// reported as unused.
func TestFactIDProducerGuardAllowlistIsPerFunction(t *testing.T) {
	src := map[string]string{"p/p.go": `package p
func a(key string) facts.Envelope { return facts.Envelope{FactID: key} }
func b(key string) facts.Envelope { return facts.Envelope{FactID: key} }`}
	violations, sites, used, err := factIDProducerViolations(src, map[string]string{"p/p.go::a": "exempt", "p/p.go::gone": "stale"})
	if err != nil || sites != 2 {
		t.Fatalf("sites=%d err=%v, want 2 sites", sites, err)
	}
	if len(violations) != 1 || !strings.Contains(violations[0], "[p/p.go::b]") {
		t.Fatalf("violations = %v, want only function b flagged", violations)
	}
	if !used["p/p.go::a"] || used["p/p.go::gone"] {
		t.Fatalf("used = %v, want a used and gone unused", used)
	}
}

// TestFactIDProducerGuardFailsOnZeroSites is the vacuous-pass RED: a scan that
// sees no Envelope FactID site is an error, not a clean result.
func TestFactIDProducerGuardFailsOnZeroSites(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"no files":            {},
		"files without sites": {"p/p.go": "package p\nfunc f() {}\n"},
	} {
		if _, sites, _, err := factIDProducerViolations(files, nil); err == nil || sites != 0 {
			t.Fatalf("%s: err=%v sites=%d, want an error for a zero-site scan", name, err, sites)
		}
	}
}

// TestFactIDProducerGuardOtherShapes pins the mint shapes beyond the
// facts.Envelope literal that #11216 closed: reducer factwrite.Row and
// factwrite.VersionedRow literals, the positional fact id argument of
// factwrite.SingleInsertQuery, and an id assigned after construction. Each
// shape has a RED case (a bare id, one violation) and a GREEN case (an id
// hashed from a scope and a generation, none), so none of them can turn into a
// silent no-op.
func TestFactIDProducerGuardOtherShapes(t *testing.T) {
	const hashed = `facts.StableID("K", map[string]any{"scope_id": scopeID, "generation_id": generationID, "k": key})`
	cases := []struct {
		name string
		path string
		src  string
		want int
	}{
		{"factwrite.Row bare id", "r/w.go", `package r
func f(key string) { _ = factwrite.Row{FactID: key} }`, 1},
		{"factwrite.Row hashed id", "r/w.go", `package r
func f(scopeID, generationID, key string) { _ = factwrite.Row{FactID: ` + hashed + `} }`, 0},
		{"factwrite.VersionedRow bare id", "r/w.go", `package r
func f(key string) { _ = factwrite.VersionedRow{FactID: key} }`, 1},
		{"factwrite.VersionedRow hashed id", "r/w.go", `package r
func f(scopeID, generationID, key string) { _ = factwrite.VersionedRow{FactID: ` + hashed + `} }`, 0},
		{"bare Row inside the factwrite package", "internal/reducer/factwrite/x.go", `package factwrite
func f(key string) Row { return Row{FactID: key} }`, 1},
		{"a Row type elsewhere is not a fact row", "r/w.go", `package r
func f(key string) { _ = Row{FactID: key} ; _ = facts.Envelope{FactID: ` + hashed + `} }`, 0},
		{"SingleInsertQuery positional bare id", "r/w.go", `package r
func f(ctx, db, key string) { db.ExecContext(ctx, factwrite.SingleInsertQuery, key, "scope") }`, 1},
		{"SingleInsertQuery positional hashed id", "r/w.go", `package r
func f(ctx, db, scopeID, generationID, key string) { db.ExecContext(ctx, factwrite.SingleInsertQuery, ` + hashed + `, "scope") }`, 0},
		{"id assigned after construction, bare", "r/w.go", `package r
func f(env facts.Envelope, key string) { env.FactID = key }`, 1},
		{"id assigned after construction, hashed", "r/w.go", `package r
func f(env facts.Envelope, scopeID, generationID, key string) { env.FactID = ` + hashed + ` }`, 0},
		{"blanking an id mints nothing", "r/w.go", `package r
func f(env facts.Envelope) { env.FactID = "" }`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A hashed-only source has one site; a no-site source would be an error,
			// so every case carries at least one recognised site of its own shape.
			violations, sites, _, err := factIDProducerViolations(map[string]string{tc.path: tc.src}, nil)
			if tc.name == "a Row type elsewhere is not a fact row" || tc.name == "blanking an id mints nothing" {
				if err != nil && sites == 0 && tc.want == 0 {
					return // no recognised site: the zero-site error is the expected outcome
				}
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if len(violations) != tc.want || sites < 1 {
				t.Fatalf("sites=%d violations=%v, want %d violations and at least 1 site", sites, violations, tc.want)
			}
		})
	}
}

// TestFactIDProducerGuardSeededReducerViolations is the RED/GREEN pair on the
// real reducer writers (#11216): GREEN is the unmodified source, RED removes
// the scope from a real fact id and the guard must name that file. Two shapes
// are seeded: a ...FactID helper whose identity call is replaced by a bare map
// (service catalog), and a VersionedRow literal whose id loses its scope_id
// key (code divergence).
func TestFactIDProducerGuardSeededReducerViolations(t *testing.T) {
	all := readProductionGoFiles(t, goSourceRoot(t))
	cases := []struct {
		name, dir, file, old, new string
	}{
		{
			"FactID helper without the identity call",
			"internal/reducer/servicecatalog/", "internal/reducer/servicecatalog/service_catalog_correlation_writer.go",
			"serviceCatalogCorrelationIdentity(write, decision),\n\t)\n}\n\nfunc serviceCatalogCorrelationStableFactKey",
			"map[string]any{\"k\": decision},\n\t)\n}\n\nfunc serviceCatalogCorrelationStableFactKey",
		},
		{
			"VersionedRow id without its scope_id key",
			"internal/reducer/codedivergence/", "internal/reducer/codedivergence/writer.go",
			"\t\t\t\"scope_id\":      strings.TrimSpace(write.ScopeID),\n\t\t\t\"generation_id\": strings.TrimSpace(write.GenerationID),\n\t\t\t\"finding_id\":    findingID,",
			"\t\t\t\"generation_id\": strings.TrimSpace(write.GenerationID),\n\t\t\t\"finding_id\":    findingID,",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clean := map[string]string{}
			seeded := map[string]string{}
			for name, body := range all {
				if strings.HasPrefix(name, tc.dir) {
					clean[name] = body
					seeded[name] = body
				}
			}
			body, ok := clean[tc.file]
			if !ok || !strings.Contains(body, tc.old) {
				t.Fatalf("seed anchor missing in %s: the seeded case must be updated with the source", tc.file)
			}
			seeded[tc.file] = strings.Replace(body, tc.old, tc.new, 1)
			if violations, _, _, err := factIDProducerViolations(clean, factIDProducerAllowlist); err != nil || len(violations) != 0 {
				t.Fatalf("GREEN: clean source violations=%v err=%v", violations, err)
			}
			violations, _, _, err := factIDProducerViolations(seeded, factIDProducerAllowlist)
			if err != nil || len(violations) == 0 {
				t.Fatalf("RED: the seeded violation in %s was not reported: violations=%v err=%v", tc.file, violations, err)
			}
			named := false
			for _, v := range violations {
				named = named || strings.HasPrefix(v, tc.file+":")
			}
			if !named {
				t.Fatalf("RED: violations %v do not name %s", violations, tc.file)
			}
		})
	}
}
