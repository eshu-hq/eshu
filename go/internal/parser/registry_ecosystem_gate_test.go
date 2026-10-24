// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ecosystemNone is the ledger value a language uses when it has no package or
// build ecosystem. The row must then carry ecosystem_reason.
const ecosystemNone = "none"

// ledgerEcosystemRow is the slice of one language_features row the gate reads.
// ManifestFiles and PinFiles stay nil when the ledger omits the key, which is
// how the gate tells an absent key from an explicit empty list.
type ledgerEcosystemRow struct {
	Language        string   `yaml:"language"`
	Ecosystem       string   `yaml:"ecosystem"`
	EcosystemReason string   `yaml:"ecosystem_reason"`
	ManifestFiles   []string `yaml:"manifest_files"`
	PinFiles        []string `yaml:"pin_files"`
}

// ecosystemGap is one documented exception to the gate: a ledger file name the
// registry does not admit yet, tracked by an open issue.
type ecosystemGap struct {
	Language string
	File     string
	Issue    string
}

// knownEcosystemGaps lists ledger files the real registry does not admit yet.
// Each entry names the language, the file, and the issue that closes it. The
// gate fails on an entry that stops being a gap, so the list shrinks as the
// registry grows and cannot rot. Do not add an entry without an issue.
var knownEcosystemGaps = []ecosystemGap{
	{Language: "csharp", File: "Directory.Packages.props", Issue: "#8824"},
	{Language: "java", File: "gradle-wrapper.properties", Issue: "#8822"},
}

type rowViolation struct {
	file    string
	message string
}

func loadEcosystemLedger(t *testing.T) []ledgerEcosystemRow {
	t.Helper()
	path := filepath.Join("..", "..", "..", "specs", "language-feature-parity-ledger.v1.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var ledger struct {
		LanguageFeatures []ledgerEcosystemRow `yaml:"language_features"`
	}
	if err := yaml.Unmarshal(raw, &ledger); err != nil {
		t.Fatalf("parse ledger: %v", err)
	}
	if len(ledger.LanguageFeatures) == 0 {
		t.Fatal("ledger has no language rows")
	}
	return ledger.LanguageFeatures
}

// admissionRank orders admission levels so a check can ask for "L1 or better".
func admissionRank(level Admission) int {
	switch level {
	case AdmissionContentText:
		return 1
	case AdmissionStructuredFact:
		return 2
	default:
		return 0
	}
}

// ecosystemViolations compares the expected set (ledger rows) with the observed
// set (registry lookups) and returns one message per gap. It reads nothing but
// its arguments, so a seeded registry or ledger row proves each rule.
func ecosystemViolations(registry Registry, rows []ledgerEcosystemRow, gaps []ecosystemGap) []string {
	var violations []string
	covered := make(map[string]bool, len(gaps))
	for _, row := range rows {
		for _, v := range rowViolations(registry, row) {
			if v.file != "" && gapListed(gaps, row.Language, v.file) {
				covered[row.Language+"/"+v.file] = true
				continue
			}
			violations = append(violations, fmt.Sprintf("%s: %s", row.Language, v.message))
		}
	}
	for _, gap := range gaps {
		switch {
		case strings.TrimSpace(gap.Issue) == "":
			violations = append(violations, fmt.Sprintf("allowlist %s/%s has no issue reference", gap.Language, gap.File))
		case !covered[gap.Language+"/"+gap.File]:
			violations = append(violations, fmt.Sprintf(
				"allowlist %s/%s (%s) is no longer a gap: remove the entry", gap.Language, gap.File, gap.Issue))
		}
	}
	return violations
}

func gapListed(gaps []ecosystemGap, language, file string) bool {
	return slices.ContainsFunc(gaps, func(gap ecosystemGap) bool {
		return gap.Language == language && gap.File == file
	})
}

func rowViolations(registry Registry, row ledgerEcosystemRow) []rowViolation {
	ecosystem := strings.TrimSpace(row.Ecosystem)
	if ecosystem == "" {
		return []rowViolation{{message: fmt.Sprintf(
			"ledger row has no ecosystem key; declare a package ecosystem or %q with ecosystem_reason", ecosystemNone)}}
	}
	var out []rowViolation
	if ecosystem == ecosystemNone {
		if strings.TrimSpace(row.EcosystemReason) == "" {
			out = append(out, rowViolation{message: "ecosystem is none but ecosystem_reason is empty"})
		}
		if len(row.ManifestFiles) > 0 || len(row.PinFiles) > 0 {
			out = append(out, rowViolation{message: "ecosystem is none but manifest_files or pin_files are set"})
		}
		return out
	}

	if len(row.ManifestFiles) == 0 {
		out = append(out, rowViolation{message: fmt.Sprintf(
			"ecosystem %q declares no manifest_files; list the manifest and lock files", ecosystem)})
	}
	if row.PinFiles == nil {
		out = append(out, rowViolation{message: fmt.Sprintf(
			"ecosystem %q has no pin_files key; use an explicit [] when it has no version-pin convention", ecosystem)})
	}
	out = append(out, admissionViolations(registry, "manifest", row.ManifestFiles, AdmissionStructuredFact)...)
	out = append(out, admissionViolations(registry, "pin", row.PinFiles, AdmissionContentText)...)
	return out
}

// admissionViolations reports each name the registry does not admit at the
// wanted level or better.
func admissionViolations(registry Registry, kind string, names []string, want Admission) []rowViolation {
	var out []rowViolation
	for _, name := range names {
		definition, ok := registry.LookupByPath(name)
		if !ok || admissionRank(definition.Admission) < admissionRank(want) {
			out = append(out, rowViolation{file: name, message: fmt.Sprintf(
				"%s file %q is not admitted at %s or better", kind, name, want)})
		}
	}
	return out
}

// registryEdited returns the default definitions rebuilt with edit applied to
// each, so a test can plant a violation without a copy of the registry data.
func registryEdited(t *testing.T, edit func(Definition) Definition) Registry {
	t.Helper()
	definitions := defaultDefinitions()
	for i := range definitions {
		definitions[i] = edit(definitions[i])
	}
	registry, err := NewRegistry(definitions)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry
}

func dropExactName(definition Definition, name string) Definition {
	definition.ExactNames = slices.DeleteFunc(slices.Clone(definition.ExactNames), func(candidate string) bool {
		return strings.EqualFold(candidate, name)
	})
	return definition
}

func requireViolation(t *testing.T, violations []string, wants ...string) {
	t.Helper()
	for _, violation := range violations {
		matchesAll := true
		for _, want := range wants {
			if !strings.Contains(violation, want) {
				matchesAll = false
				break
			}
		}
		if matchesAll {
			return
		}
	}
	t.Fatalf("no violation contains all of %q; got:\n%s", wants, strings.Join(violations, "\n"))
}

// TestEcosystemManifestAndPinGateAcceptsRealRegistry is the GREEN case: the
// expected set comes from the ledger file and the observed set from the real
// registry. Gaps the registry has today carry an issue in knownEcosystemGaps.
func TestEcosystemManifestAndPinGateAcceptsRealRegistry(t *testing.T) {
	t.Parallel()

	violations := ecosystemViolations(DefaultRegistry(), loadEcosystemLedger(t), knownEcosystemGaps)
	if len(violations) > 0 {
		t.Fatalf("ledger and registry disagree on manifest and pin files:\n%s", strings.Join(violations, "\n"))
	}
}

// TestEcosystemGateReportsMissingGoManifest is RED: a throwaway registry
// without go.mod makes the gate report go missing its manifest.
func TestEcosystemGateReportsMissingGoManifest(t *testing.T) {
	t.Parallel()

	registry := registryEdited(t, func(definition Definition) Definition {
		return dropExactName(definition, "go.mod")
	})
	violations := ecosystemViolations(registry, loadEcosystemLedger(t), knownEcosystemGaps)
	requireViolation(t, violations, "go:", `manifest file "go.mod"`)
}

// TestEcosystemGateReportsMissingPinFile is RED for the pin side: without
// .nvmrc in the registry, the node-based languages lose their pin file.
func TestEcosystemGateReportsMissingPinFile(t *testing.T) {
	t.Parallel()

	registry := registryEdited(t, func(definition Definition) Definition {
		return dropExactName(definition, ".nvmrc")
	})
	violations := ecosystemViolations(registry, loadEcosystemLedger(t), knownEcosystemGaps)
	requireViolation(t, violations, "javascript:", `pin file ".nvmrc"`)
}

// TestEcosystemGateRejectsContentTextManifest is RED: a manifest admitted only
// as L1 text is not an L2 manifest.
func TestEcosystemGateRejectsContentTextManifest(t *testing.T) {
	t.Parallel()

	registry := registryEdited(t, func(definition Definition) Definition {
		if definition.ParserKey == "raw_text" {
			definition.ExactNames = append(slices.Clone(definition.ExactNames), "fakepkg.lock")
		}
		return definition
	})
	rows := []ledgerEcosystemRow{{
		Language: "go", Ecosystem: "go-modules",
		ManifestFiles: []string{"fakepkg.lock"}, PinFiles: []string{},
	}}
	requireViolation(t, ecosystemViolations(registry, rows, nil),
		"go:", `"fakepkg.lock"`, string(AdmissionStructuredFact))
}

// TestEcosystemGateReportsUnregisteredEcosystem is RED: a ledger language
// fakelang with ecosystem fakepkg and no registry entry fails on its
// unadmitted manifest and pin file.
func TestEcosystemGateReportsUnregisteredEcosystem(t *testing.T) {
	t.Parallel()

	rows := []ledgerEcosystemRow{{
		Language: "fakelang", Ecosystem: "fakepkg",
		ManifestFiles: []string{"fakepkg.lock"}, PinFiles: []string{".fakelang-version"},
	}}
	violations := ecosystemViolations(DefaultRegistry(), rows, nil)
	requireViolation(t, violations, "fakelang:", `manifest file "fakepkg.lock"`)
	requireViolation(t, violations, "fakelang:", `pin file ".fakelang-version"`)
}

// TestEcosystemGateFailsOnMissingKeys is RED: a missing key fails instead of
// being skipped. The last case is GREEN and shows an explicit [] passes.
func TestEcosystemGateFailsOnMissingKeys(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	cases := map[string]struct {
		row  ledgerEcosystemRow
		want string
	}{
		"no ecosystem key":    {ledgerEcosystemRow{Language: "go"}, "no ecosystem key"},
		"none without reason": {ledgerEcosystemRow{Language: "sql", Ecosystem: ecosystemNone}, "ecosystem_reason is empty"},
		"none with manifests": {ledgerEcosystemRow{
			Language: "sql", Ecosystem: ecosystemNone, EcosystemReason: "x", ManifestFiles: []string{"go.mod"},
		}, "manifest_files or pin_files are set"},
		"no manifest list": {ledgerEcosystemRow{
			Language: "go", Ecosystem: "go-modules", PinFiles: []string{},
		}, "no manifest_files"},
		"no pin key": {ledgerEcosystemRow{
			Language: "go", Ecosystem: "go-modules", ManifestFiles: []string{"go.mod"},
		}, "no pin_files key"},
		"explicit empty pins pass": {ledgerEcosystemRow{
			Language: "go", Ecosystem: "go-modules", ManifestFiles: []string{"go.mod"}, PinFiles: []string{},
		}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			violations := ecosystemViolations(registry, []ledgerEcosystemRow{tc.row}, nil)
			if tc.want == "" {
				if len(violations) != 0 {
					t.Errorf("unexpected violations: %v", violations)
				}
				return
			}
			requireViolation(t, violations, tc.want)
		})
	}
}

// TestEcosystemGateAllowlistIsStrict proves the allowlist cannot hide a stale
// or issue-less entry: a covered gap passes, a fixed gap and a missing issue
// reference both fail.
func TestEcosystemGateAllowlistIsStrict(t *testing.T) {
	t.Parallel()

	rows := []ledgerEcosystemRow{{
		Language: "go", Ecosystem: "go-modules",
		ManifestFiles: []string{"go.mod", "missing.lock"}, PinFiles: []string{},
	}}
	registry := DefaultRegistry()

	covered := []ecosystemGap{{Language: "go", File: "missing.lock", Issue: "#1"}}
	if violations := ecosystemViolations(registry, rows, covered); len(violations) != 0 {
		t.Fatalf("documented gap should pass, got %v", violations)
	}
	stale := append(slices.Clone(covered), ecosystemGap{Language: "go", File: "go.mod", Issue: "#2"})
	requireViolation(t, ecosystemViolations(registry, rows, stale), "allowlist go/go.mod", "no longer a gap")
	noIssue := []ecosystemGap{{Language: "go", File: "missing.lock"}}
	requireViolation(t, ecosystemViolations(registry, rows, noIssue), "allowlist go/missing.lock", "no issue reference")
	requireViolation(t, ecosystemViolations(registry, rows, nil), "go:", `"missing.lock"`)
}
