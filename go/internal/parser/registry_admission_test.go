// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ecosystemsWithoutLedgerRow are registry ecosystems that span languages or
// have no row in specs/language-feature-parity-ledger.v1.yaml. Every other
// ecosystem must be a ledger language key.
var ecosystemsWithoutLedgerRow = []string{"dockerfile", "raw_text", "yaml"}

func ledgerLanguageKeys(t *testing.T) []string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "specs", "language-feature-parity-ledger.v1.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var ledger struct {
		LanguageFeatures []struct {
			Language string `yaml:"language"`
		} `yaml:"language_features"`
	}
	if err := yaml.Unmarshal(raw, &ledger); err != nil {
		t.Fatalf("parse ledger: %v", err)
	}
	keys := make([]string, 0, len(ledger.LanguageFeatures))
	for _, row := range ledger.LanguageFeatures {
		keys = append(keys, row.Language)
	}
	if len(keys) == 0 {
		t.Fatal("ledger has no language rows")
	}
	return keys
}

func TestDefaultDefinitionsAreLabelled(t *testing.T) {
	t.Parallel()

	ledger := ledgerLanguageKeys(t)
	for _, definition := range defaultDefinitions() {
		if strings.TrimSpace(definition.Ecosystem) == "" {
			t.Errorf("%s: Ecosystem is empty", definition.ParserKey)
			continue
		}
		if !slices.Contains(ledger, definition.Ecosystem) &&
			!slices.Contains(ecosystemsWithoutLedgerRow, definition.Ecosystem) {
			t.Errorf("%s: Ecosystem %q is neither a ledger language key nor a listed cross-cutting ecosystem",
				definition.ParserKey, definition.Ecosystem)
		}
		switch definition.Admission {
		case AdmissionContentText, AdmissionStructuredFact:
		default:
			t.Errorf("%s: Admission = %q, want %q or %q", definition.ParserKey,
				definition.Admission, AdmissionContentText, AdmissionStructuredFact)
		}
	}
}

func TestLookupsPreserveAdmissionLabels(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	byKey, ok := registry.LookupByParserKey("gomod")
	if !ok {
		t.Fatal("gomod not registered")
	}
	if byKey.Ecosystem != "go" || byKey.Admission != AdmissionStructuredFact {
		t.Fatalf("gomod labels = %q/%q, want go/%s", byKey.Ecosystem, byKey.Admission, AdmissionStructuredFact)
	}
	byPath, ok := registry.LookupByPath("conf/app.conf")
	if !ok {
		t.Fatal("conf/app.conf not registered")
	}
	if byPath.Admission != AdmissionContentText {
		t.Fatalf("raw_text Admission = %q, want %q", byPath.Admission, AdmissionContentText)
	}
	for _, definition := range registry.Definitions() {
		if definition.Ecosystem == "" || definition.Admission == "" {
			t.Errorf("Definitions() dropped labels for %s", definition.ParserKey)
		}
	}
}

// TestAdmissionLabelsDoNotMoveRegistryFingerprint proves Ecosystem and
// Admission stay out of the parser-set identity. Labels are documentation
// metadata: if they moved the fingerprint, every relabel would invalidate
// every repository's freshness hint (#7986) for no change in what discovery
// admits. The test compares the default registry against the same
// definitions with the labels cleared and with the labels rewritten, so a
// registration change (a new parser or file name, such as go.work in #8823)
// still moves the digest on its own with no hand-edited pin.
func TestAdmissionLabelsDoNotMoveRegistryFingerprint(t *testing.T) {
	t.Parallel()

	want := DefaultRegistry().Fingerprint()
	relabel := map[string]func(Definition) Definition{
		"cleared": func(definition Definition) Definition {
			definition.Ecosystem = ""
			definition.Admission = ""
			return definition
		},
		"rewritten": func(definition Definition) Definition {
			definition.Ecosystem = "relabelled-" + definition.Ecosystem
			definition.Admission = AdmissionContentText
			return definition
		},
	}
	for name, change := range relabel {
		definitions := defaultDefinitions()
		for i := range definitions {
			definitions[i] = change(definitions[i])
		}
		registry, err := NewRegistry(definitions)
		if err != nil {
			t.Fatalf("%s: NewRegistry() error = %v", name, err)
		}
		if got := registry.Fingerprint(); got != want {
			t.Fatalf("%s labels: Fingerprint() = %s, want %s: admission labels must not change discovery identity",
				name, got, want)
		}
	}
}

func TestAdmissionTableIsDeterministicAndComplete(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	first := AdmissionTableMarkdown(registry)
	if second := AdmissionTableMarkdown(registry); first != second {
		t.Fatal("AdmissionTableMarkdown is not deterministic")
	}
	for _, definition := range registry.Definitions() {
		if !strings.Contains(first, "`"+definition.ParserKey+"`") {
			t.Errorf("table misses parser key %s", definition.ParserKey)
		}
	}
}

func TestAdmissionTableDocIsCurrent(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", AdmissionDocPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", AdmissionDocPath, err)
	}
	want, err := ReplaceAdmissionTable(string(raw), AdmissionTableMarkdown(DefaultRegistry()))
	if err != nil {
		t.Fatalf("locate admission table markers: %v", err)
	}
	if want != string(raw) {
		t.Fatalf("%s admission table is stale; run scripts/generate-parser-admission-doc.sh", AdmissionDocPath)
	}
}

func TestReplaceAdmissionTableRequiresMarkers(t *testing.T) {
	t.Parallel()

	if _, err := ReplaceAdmissionTable("no markers here\n", "table\n"); err == nil {
		t.Fatal("expected an error when the markers are missing")
	}
	stale := AdmissionTableBegin + "\nold\n" + AdmissionTableEnd + "\n"
	got, err := ReplaceAdmissionTable(stale, "new\n")
	if err != nil {
		t.Fatalf("ReplaceAdmissionTable: %v", err)
	}
	if want := AdmissionTableBegin + "\nnew\n" + AdmissionTableEnd + "\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReplaceAdmissionTableRejectsReversedAndDuplicateMarkers(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"reversed":         AdmissionTableEnd + "\nx\n" + AdmissionTableBegin + "\n",
		"duplicate begin":  AdmissionTableBegin + "\n" + AdmissionTableBegin + "\n" + AdmissionTableEnd + "\n",
		"duplicate end":    AdmissionTableBegin + "\n" + AdmissionTableEnd + "\n" + AdmissionTableEnd + "\n",
		"duplicate region": strings.Repeat(AdmissionTableBegin+"\nold\n"+AdmissionTableEnd+"\n", 2),
		"begin only":       AdmissionTableBegin + "\nold\n",
	}
	for name, doc := range cases {
		if _, err := ReplaceAdmissionTable(doc, "new\n"); err == nil {
			t.Errorf("%s: expected an error, got none", name)
		}
	}
}

func TestAnchorDefinitionsKeepTheirAdmissionLevel(t *testing.T) {
	t.Parallel()

	want := map[string]struct {
		ecosystem string
		admission Admission
	}{
		"gomod":               {"go", AdmissionStructuredFact},
		"json":                {"json", AdmissionStructuredFact},
		"yaml":                {"yaml", AdmissionStructuredFact},
		"python_requirements": {"python", AdmissionStructuredFact},
		"node_lockfile":       {"javascript", AdmissionStructuredFact},
		"__dockerfile__":      {"dockerfile", AdmissionStructuredFact},
		"raw_text":            {"raw_text", AdmissionContentText},
	}
	registry := DefaultRegistry()
	for key, expected := range want {
		definition, ok := registry.LookupByParserKey(key)
		if !ok {
			t.Errorf("%s is not registered", key)
			continue
		}
		if definition.Ecosystem != expected.ecosystem || definition.Admission != expected.admission {
			t.Errorf("%s labels = %q/%q, want %q/%q", key,
				definition.Ecosystem, definition.Admission, expected.ecosystem, expected.admission)
		}
	}
	for _, definition := range registry.Definitions() {
		if definition.Admission == AdmissionContentText && definition.ParserKey != "raw_text" {
			t.Errorf("%s is L1; only raw_text is expected to be content-text only", definition.ParserKey)
		}
	}
}
