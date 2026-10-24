// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

var (
	grantsKeyLine   = regexp.MustCompile(`^(\s*)grants:\s*(#.*)?$`)
	grantKindLine   = regexp.MustCompile(`^\s*(?:-\s+)?kind:\s*["']?([A-Za-z0-9_.]+)["']?\s*(?:#.*)?$`)
	commentLeadIn   = regexp.MustCompile(`^\s*#\s?`)
	grantExampleSrc = []string{
		"deploy/helm/eshu/values.yaml",
		"deploy/helm/eshu/README.md",
		"docs/public/deploy/kubernetes/run-an-extracted-collector.md",
		"docs/public/reference/helm-collector-and-webhook-values.md",
		"docs/public/extend/reference-pagerduty-extension.md",
		"docs/internal/evidence/8894-component-extension-activations.md",
	}
)

// grantKindsIn returns every `kind:` under a `grants:` key in text. Lines of a
// commented example (values.yaml) are read with their leading "# " removed, so
// the same scan covers YAML, Markdown code blocks and commented values.
func grantKindsIn(text string) []string {
	var kinds []string
	grantsIndent := -1
	for _, raw := range strings.Split(text, "\n") {
		line := commentLeadIn.ReplaceAllString(raw, "")
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if match := grantsKeyLine.FindStringSubmatch(line); match != nil {
			grantsIndent = len(match[1])
			continue
		}
		if grantsIndent >= 0 && indent <= grantsIndent {
			grantsIndent = -1
		}
		if grantsIndent < 0 {
			continue
		}
		if match := grantKindLine.FindStringSubmatch(line); match != nil {
			kinds = append(kinds, match[1])
		}
	}
	return kinds
}

// TestGrantKindScannerFindsAPlantedKind seeds a violation for the scanner
// itself: the guard below is only worth trusting if it can see a bad kind.
func TestGrantKindScannerFindsAPlantedKind(t *testing.T) {
	t.Parallel()

	planted := "  #   grants:                 # core-owned fact kinds only\n" +
		"  #     - kind: incident_routing\n" +
		"  #       scope: pagerduty\n" +
		"  #   pollInterval: 30s\n" +
		"  #   other:\n" +
		"  #     - kind: not_a_grant_kind\n"
	got := grantKindsIn(planted)
	if !slices.Equal(got, []string{"incident_routing"}) {
		t.Fatalf("grantKindsIn(planted) = %v, want only the kind under grants", got)
	}
	if facts.IsCoreFactKind(got[0]) {
		t.Fatalf("%q is a core fact kind; the planted violation is not a violation", got[0])
	}
}

// TestComponentActivationExampleGrantKindsAreCoreOwned keeps every example
// grant honest. `eshu component grant` refuses a kind that is not in the core
// fact registry, so a copied example with a made-up kind leaves the reconcile
// init container in Init:Error.
func TestComponentActivationExampleGrantKindsAreCoreOwned(t *testing.T) {
	t.Parallel()

	sources := map[string]string{
		"fixture pagerdutyActivationValues": pagerDutyActivationValues,
		"fixture jiraActivationValues":      jiraActivationValues,
	}
	root := repositoryRoot(t)
	for _, rel := range grantExampleSrc {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		sources[rel] = string(body)
	}

	found := map[string]int{}
	for name, text := range sources {
		for _, kind := range grantKindsIn(text) {
			found[name]++
			if !facts.IsCoreFactKind(kind) {
				t.Errorf("%s: example grant kind %q is not in facts.CoreFactKinds(); `eshu component grant` rejects it", name, kind)
			}
		}
	}
	// A scanner that finds nothing would pass vacuously. These sources must
	// carry at least one example grant.
	for _, name := range []string{
		"fixture pagerdutyActivationValues",
		"deploy/helm/eshu/values.yaml",
		"docs/public/deploy/kubernetes/run-an-extracted-collector.md",
	} {
		if found[name] == 0 {
			t.Errorf("%s: no example grant found; the scan no longer sees it", name)
		}
	}
}
