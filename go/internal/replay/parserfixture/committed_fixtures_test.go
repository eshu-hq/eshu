// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parserfixture_test

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/parserfixture"
	"gopkg.in/yaml.v3"
)

// updateFixtures regenerates the committed parser fixtures from the live parser.
// Run `go test ./internal/replay/parserfixture/ -update-fixtures` after a
// deliberate parser change, then review the fixture diff. Without it the golden
// assertion fails on any drift, which is the point: a parser change that drops or
// mis-attributes a fact (or its provenance) shows up as a fixture diff in CI.
var updateFixtures = flag.Bool("update-fixtures", false, "regenerate the committed parser fixtures from the live parser")

// ledgerCase is one parser-backing-ledger parser plus the package-local,
// parser-focused source tree and committed fixture that exercise it. These are
// the C-3 (#4175) worklist: every parser in specs/parser-backing-ledger.v1.yaml.
type ledgerCase struct {
	// parser is the specs/parser-backing-ledger.v1.yaml parser name (the coverage
	// key the manifest maps as parser:<parser>).
	parser string
	// treeDir is the package-local source tree under testdata/trees/<treeDir>.
	treeDir string
	// language is the parser language label every recorded fact must carry, proving
	// the intended parser — not a sibling — produced the fixture.
	language string
	// signatureArray is a parsed_file_data array that must be non-empty in at least
	// one fact, proving the parser's domain extraction ran (not just a bare file
	// fact). It distinguishes, e.g., a CloudFormation template from a plain YAML.
	signatureArray string
}

// ledgerCases enumerates the four parser-backing-ledger parsers. Keep this in
// lockstep with specs/parser-backing-ledger.v1.yaml: TestLedgerCasesMatchSpec
// fails if the spec adds or removes a parser without a fixture here.
func ledgerCases() []ledgerCase {
	return []ledgerCase{
		{parser: "cloudformation", treeDir: "cloudformation", language: "yaml", signatureArray: "cloudformation_resources"},
		{parser: "dockerfile", treeDir: "dockerfile", language: "dockerfile", signatureArray: "dockerfile_stages"},
		{parser: "hcl", treeDir: "hcl", language: "hcl", signatureArray: "terraform_resources"},
		{parser: "yaml", treeDir: "yaml", language: "yaml", signatureArray: "k8s_resources"},
	}
}

// packageDir is the directory holding this test file (the parserfixture package).
func packageDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Dir(file)
}

// repoRoot resolves the repository root four levels above the package directory
// (parserfixture -> replay -> internal -> go -> repo root). It is the root the
// committed fixtures are portable against.
func repoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join(packageDir(t), "..", "..", "..", ".."))
}

func (tc ledgerCase) treePath(t *testing.T) string {
	return filepath.Join(packageDir(t), "testdata", "trees", tc.treeDir)
}

func (tc ledgerCase) fixturePath(t *testing.T) string {
	return filepath.Join(packageDir(t), "testdata", "fixtures", tc.parser+".fixture.json")
}

// newLedgerEmitter builds an emitter over the case's package-local tree.
func (tc ledgerCase) newLedgerEmitter(t *testing.T) *parserfixture.Emitter {
	t.Helper()
	em, err := parserfixture.NewEmitter(parserfixture.EmitterOptions{
		ScopeID:  "parser_fixture:" + tc.parser,
		RepoID:   tc.parser,
		TreePath: tc.treePath(t),
	})
	if err != nil {
		t.Fatalf("%s: NewEmitter: %v", tc.parser, err)
	}
	return em
}

// TestCommittedParserFixturesAreCurrent is the C-3 golden gate: for every
// parser-backing-ledger parser, re-recording its tree (portable, repo-root
// tokenized) must byte-match the committed fixture. A parser change that drops or
// mis-attributes a fact, or changes its provenance, breaks this — exactly the
// regression the issue (#4175) targets. `-update-fixtures` regenerates them.
func TestCommittedParserFixturesAreCurrent(t *testing.T) {
	root := repoRoot(t)
	for _, tc := range ledgerCases() {
		t.Run(tc.parser, func(t *testing.T) {
			fixturePath := tc.fixturePath(t)
			tmp := filepath.Join(t.TempDir(), tc.parser+".json")
			if err := parserfixture.Record(context.Background(), parserfixture.RecordOptions{
				Emitter:  tc.newLedgerEmitter(t),
				Path:     tmp,
				RepoRoot: root,
			}); err != nil {
				t.Fatalf("Record: %v", err)
			}
			fresh, err := os.ReadFile(tmp)
			if err != nil {
				t.Fatalf("read fresh recording: %v", err)
			}

			if *updateFixtures {
				if err := os.MkdirAll(filepath.Dir(fixturePath), 0o755); err != nil {
					t.Fatalf("mkdir fixtures: %v", err)
				}
				// #nosec G306 -- committed, world-readable test fixture, not a secret.
				if err := os.WriteFile(fixturePath, fresh, 0o644); err != nil {
					t.Fatalf("write fixture: %v", err)
				}
				t.Logf("updated %s", fixturePath)
				return
			}

			committed, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("read committed fixture %q (run with -update-fixtures to create it): %v", fixturePath, err)
			}
			if string(committed) != string(fresh) {
				t.Errorf("committed fixture %q is stale; re-run with -update-fixtures and review the diff", fixturePath)
			}
			// A committed fixture must be portable: no machine-specific absolute path.
			if filepath.IsAbs(root) && bytes.Contains(committed, []byte(root)) {
				t.Errorf("committed fixture %q leaks an absolute checkout path", fixturePath)
			}
		})
	}
}

// TestCommittedParserFixturesReplayGreenWithProvenance is the R-7 replay proof:
// loading each committed fixture (rehydrated to the local checkout) and replaying
// it reproduces the live parser's envelopes exactly, including SourceRef
// provenance, and the recorded facts carry the intended parser's language and a
// non-empty domain signature — so the fixture proves that specific parser ran.
func TestCommittedParserFixturesReplayGreenWithProvenance(t *testing.T) {
	root := repoRoot(t)
	for _, tc := range ledgerCases() {
		t.Run(tc.parser, func(t *testing.T) {
			live := drainEnvelopes(t, tc.newLedgerEmitter(t))
			if len(live) == 0 {
				t.Fatalf("%s: live emitter produced no envelopes", tc.parser)
			}

			src, err := parserfixture.NewSourceRehydrated(tc.fixturePath(t), root)
			if err != nil {
				t.Fatalf("NewSourceRehydrated: %v", err)
			}
			replayed := drainEnvelopes(t, src)
			if len(replayed) != len(live) {
				t.Fatalf("%s: envelope count drift: live=%d replayed=%d", tc.parser, len(live), len(replayed))
			}

			sawSignature := false
			for i := range live {
				assertEnvelopeEqual(t, live[i], replayed[i])
				// Provenance must be present and absolute (rehydrated to this checkout).
				if replayed[i].SourceRef.SourceURI == "" {
					t.Errorf("%s: fact %q lost SourceURI provenance", tc.parser, replayed[i].StableFactKey)
				}
				if pfd, ok := replayed[i].Payload["parsed_file_data"].(map[string]any); ok {
					if arr, ok := pfd[tc.signatureArray].([]any); ok && len(arr) > 0 {
						sawSignature = true
					}
				}
				if lang, _ := replayed[i].Payload["language"].(string); lang != tc.language {
					t.Errorf("%s: fact %q language=%q want %q", tc.parser, replayed[i].StableFactKey, lang, tc.language)
				}
			}
			if !sawSignature {
				t.Errorf("%s: no fact carries a non-empty %q; the fixture does not prove this parser's extraction", tc.parser, tc.signatureArray)
			}
		})
	}
}

// TestLedgerCasesMatchSpec keeps the C-3 fixtures in lockstep with the
// parser-backing ledger: if a parser is added to or removed from
// specs/parser-backing-ledger.v1.yaml without a corresponding fixture case here,
// this fails — so parser coverage cannot silently drop below 100% of the ledger.
func TestLedgerCasesMatchSpec(t *testing.T) {
	specPath := filepath.Join(repoRoot(t), "specs", "parser-backing-ledger.v1.yaml")
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read ledger spec: %v", err)
	}
	var spec struct {
		Parsers []struct {
			Parser string `yaml:"parser"`
		} `yaml:"parser_backing"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse ledger spec: %v", err)
	}
	specSet := map[string]bool{}
	for _, p := range spec.Parsers {
		specSet[p.Parser] = true
	}
	caseSet := map[string]bool{}
	for _, tc := range ledgerCases() {
		caseSet[tc.parser] = true
	}
	for p := range specSet {
		if !caseSet[p] {
			t.Errorf("parser %q is in the ledger spec but has no committed-fixture case (C-3 must cover every ledger parser)", p)
		}
	}
	for p := range caseSet {
		if !specSet[p] {
			t.Errorf("fixture case %q is not in the ledger spec; remove it or fix the parser name", p)
		}
	}
}
