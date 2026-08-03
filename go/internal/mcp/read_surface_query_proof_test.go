// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// languageParityClaimVerdict is why one (language, surface) pair passed or
// failed the semantic check, so both the gate and its own bite test can reason
// about the same decision instead of duplicating the rule.
type languageParityClaimVerdict struct {
	OK     bool
	Reason string
}

// evaluateLanguageParityClaim is the #5510 rule, extracted so
// TestLanguageParityQueryProofGateBites can drive it against a seeded row
// without mutating the committed ledger.
//
// A citation passes when any of these holds:
//   - the label is a generic read, so the row makes no per-language claim;
//   - a chained per-language proof is registered;
//   - the row declares partial features, i.e. it already says the claim is
//     incomplete;
//   - a tracked exemption names the issue that will close the gap.
func evaluateLanguageParityClaim(entry replaycoverage.LanguageLedgerEntry, surface string) languageParityClaimVerdict {
	class, classified := readSurfaceClaimClasses[surface]
	if !classified {
		return languageParityClaimVerdict{
			Reason: "read-surface label is not classified in readSurfaceClaimClasses; " +
				"decide whether it asserts per-language queryable truth or is a generic read",
		}
	}
	if class == claimGenericRead {
		return languageParityClaimVerdict{OK: true, Reason: "generic read surface: no per-language claim"}
	}
	if proof, ok := queryProofFor(entry.Language, surface); ok {
		return languageParityClaimVerdict{OK: true, Reason: "chained proof " + proof.Test}
	}
	if feature, ok := partialFeatureExcuses(surface, entry.PartialFeatures); ok {
		return languageParityClaimVerdict{OK: true, Reason: "row declares partial feature " + feature}
	}
	if reason, ok := exemptionFor(entry.Language, surface); ok {
		return languageParityClaimVerdict{OK: true, Reason: "tracked exemption: " + reason}
	}
	return languageParityClaimVerdict{
		Reason: "claims queryable truth with no registered chained proof, no partial_features, and no tracked exemption",
	}
}

// TestLanguageParityQueryProofGate is the #5510 semantic gate.
//
// The existing existence gate proves a cited tool is registered. This proves the
// stronger thing the ledger actually asserts: that the language's claim is
// answerable end to end. A row citing a queryable-truth surface must either
// register a chained proof, say in the ledger that the feature is partial, or
// carry a tracked exemption naming the gap.
func TestLanguageParityQueryProofGate(t *testing.T) {
	t.Parallel()

	ledger, err := replaycoverage.LoadLanguageLedger(
		filepath.Join(readSurfaceGateSpecsDir(t), replaycoverage.LanguageLedgerFileName))
	if err != nil {
		t.Fatalf("LoadLanguageLedger: %v", err)
	}
	if len(ledger.Languages) == 0 {
		t.Fatal("language ledger is empty; the gate would pass vacuously")
	}

	checked := 0
	for _, entry := range ledger.Languages {
		for _, surface := range entry.ReadSurfaces {
			verdict := evaluateLanguageParityClaim(entry, surface)
			checked++
			if !verdict.OK {
				t.Errorf("language %q cites read surface %q: %s -- either register the chained "+
					"proof in languageParityQueryProofs, mark the feature partial in the ledger, "+
					"or add a tracked exemption naming the issue",
					entry.Language, surface, verdict.Reason)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no citations were evaluated; the gate would pass vacuously")
	}
	t.Logf("evaluated %d read-surface citations across %d languages", checked, len(ledger.Languages))
}

// TestLanguageParityQueryProofGateBites is the requirement that the gate
// actually fail. A gate nobody has watched fail is a gate nobody knows works —
// which is the whole complaint #5510 makes about its predecessor.
//
// Seeds an unbacked claim, asserts the gate rejects it and names it, then
// asserts the same row passes once the claim is backed each of the three
// legitimate ways.
func TestLanguageParityQueryProofGateBites(t *testing.T) {
	t.Parallel()

	const surface = "trace_route_callers" // a queryable-truth label

	unbacked := replaycoverage.LanguageLedgerEntry{
		Language:     "seeded-unbacked-language",
		ReadSurfaces: []string{surface},
	}
	verdict := evaluateLanguageParityClaim(unbacked, surface)
	if verdict.OK {
		t.Fatal("gate accepted a queryable-truth claim with no proof, no partial_features and no exemption; it does not bite")
	}
	if !strings.Contains(verdict.Reason, "no registered chained proof") {
		t.Errorf("rejection reason = %q, want it to name the missing proof", verdict.Reason)
	}

	// 1. Backed by declaring the feature partial.
	partial := unbacked
	partial.PartialFeatures = []string{"seeded-partial-route-truth"}
	if v := evaluateLanguageParityClaim(partial, surface); !v.OK {
		t.Errorf("row declaring partial_features was rejected: %s", v.Reason)
	}

	// 2. Backed by a registered chained proof. Java is the original one.
	javaEntry := replaycoverage.LanguageLedgerEntry{Language: "java", ReadSurfaces: []string{surface}}
	v := evaluateLanguageParityClaim(javaEntry, surface)
	if !v.OK {
		t.Errorf("java route claim was rejected despite a registered proof: %s", v.Reason)
	}
	if !strings.Contains(v.Reason, "TestHandleRouteToCallerResolvesJavaSpringHandler") {
		t.Errorf("acceptance reason = %q, want it to name the proof test", v.Reason)
	}

	// 3. A generic read surface never demands a per-language proof.
	generic := replaycoverage.LanguageLedgerEntry{Language: "seeded-unbacked-language", ReadSurfaces: []string{"execute_language_query"}}
	if v := evaluateLanguageParityClaim(generic, "execute_language_query"); !v.OK {
		t.Errorf("generic read surface was rejected: %s", v.Reason)
	}

	// An unclassified label fails closed rather than defaulting either way.
	if v := evaluateLanguageParityClaim(unbacked, "surface_nobody_classified"); v.OK {
		t.Error("gate accepted an unclassified read-surface label; it must fail closed")
	}
}

// TestLanguageParityClaimClassesCoverEveryCitedLabel keeps the classification
// map honest in both directions: every label the ledger cites must be
// classified, and a class entry for a label nobody cites is stale.
func TestLanguageParityClaimClassesCoverEveryCitedLabel(t *testing.T) {
	t.Parallel()

	ledger, err := replaycoverage.LoadLanguageLedger(
		filepath.Join(readSurfaceGateSpecsDir(t), replaycoverage.LanguageLedgerFileName))
	if err != nil {
		t.Fatalf("LoadLanguageLedger: %v", err)
	}

	cited := map[string]bool{}
	for _, entry := range ledger.Languages {
		for _, surface := range entry.ReadSurfaces {
			cited[surface] = true
			if _, ok := readSurfaceClaimClasses[surface]; !ok {
				t.Errorf("read surface %q (cited by %q) has no claim class", surface, entry.Language)
			}
		}
	}

	var stale []string
	for label := range readSurfaceClaimClasses {
		if label == languageParityReadSurfaceNone {
			continue // the sentinel is legitimately unused by every row
		}
		if !cited[label] {
			stale = append(stale, label)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("claim classes exist for labels no ledger row cites: %v -- remove them so the map "+
			"cannot drift into describing surfaces that are gone", stale)
	}
}

// TestLanguageParityQueryProofRegistryIsNotStale rejects a registered proof or
// exemption for a (language, surface) pair the ledger no longer cites. A
// registry that outlives its rows starts asserting coverage for claims nobody
// makes.
func TestLanguageParityQueryProofRegistryIsNotStale(t *testing.T) {
	t.Parallel()

	ledger, err := replaycoverage.LoadLanguageLedger(
		filepath.Join(readSurfaceGateSpecsDir(t), replaycoverage.LanguageLedgerFileName))
	if err != nil {
		t.Fatalf("LoadLanguageLedger: %v", err)
	}

	cited := map[string]map[string]bool{}
	for _, entry := range ledger.Languages {
		cited[entry.Language] = map[string]bool{}
		for _, surface := range entry.ReadSurfaces {
			cited[entry.Language][surface] = true
		}
	}

	for _, language := range sortedLanguages(languageParityQueryProofs) {
		for surface := range languageParityQueryProofs[language] {
			if !cited[language][surface] {
				t.Errorf("proof registered for %q/%q, which the ledger no longer cites", language, surface)
			}
		}
	}
	for language, bySurface := range languageParityQueryProofExemptions {
		for surface := range bySurface {
			if !cited[language][surface] {
				t.Errorf("exemption registered for %q/%q, which the ledger no longer cites", language, surface)
			}
			if _, proven := queryProofFor(language, surface); proven {
				t.Errorf("%q/%q has both a chained proof and an exemption; drop the exemption", language, surface)
			}
		}
	}
}

// TestLanguageParityQueryProofTestsExist closes the second half of the
// registry's honesty problem.
//
// languageQueryProof.Test is free-form text. Go does not fail when a test
// disappears, so a renamed or deleted proof leaves the registry asserting a
// chain that nothing runs — the map entry keeps excusing the claim, and the
// gate keeps passing. That is the same "cited artifact does not exist" defect
// the existence gate was built for, reintroduced one layer up.
//
// Resolves each registered Test back to a real `func Test...` declaration in
// the repository, stripping the package qualifier and any /subtest suffix.
func TestLanguageParityQueryProofTestsExist(t *testing.T) {
	t.Parallel()

	declared := declaredGoTestNames(t)
	if len(declared) < 100 {
		t.Fatalf("only found %d test declarations; the scan is broken and would pass vacuously", len(declared))
	}

	for _, language := range sortedLanguages(languageParityQueryProofs) {
		for surface, proof := range languageParityQueryProofs[language] {
			name := proof.Test
			if idx := strings.Index(name, "/"); idx >= 0 {
				name = name[:idx] // drop a /subtest suffix
			}
			if idx := strings.LastIndex(name, "."); idx >= 0 {
				name = name[idx+1:] // drop the package qualifier
			}
			if !declared[name] {
				t.Errorf("%q/%q registers proof %q, but no func %s exists in the repository -- "+
					"the proof was renamed or removed and the registry is now excusing a claim "+
					"nothing runs", language, surface, proof.Test, name)
			}
			if strings.TrimSpace(proof.Chain) == "" {
				t.Errorf("%q/%q registers proof %q with no Chain description; a reviewer cannot tell "+
					"a real chain from a handler test with a hand-built fixture", language, surface, proof.Test)
			}
		}
	}
}

// declaredGoTestNames scans the repository's Go test files for top-level test
// function declarations.
func declaredGoTestNames(t *testing.T) map[string]bool {
	t.Helper()

	root := filepath.Join(readSurfaceGateSpecsDir(t), "..", "go")
	declared := map[string]bool{}
	pattern := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil //nolint:nilerr // an unreadable entry is skipped, not fatal
		}
		body, readErr := os.ReadFile(path) // #nosec G304 -- repository-local test sources
		if readErr != nil {
			return nil //nolint:nilerr // same
		}
		for _, match := range pattern.FindAllSubmatch(body, -1) {
			declared[string(match[1])] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan test declarations: %v", err)
	}
	return declared
}
