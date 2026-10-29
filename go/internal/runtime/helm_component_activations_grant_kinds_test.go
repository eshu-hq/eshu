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
	grantsKeyLine = regexp.MustCompile(`^(\s*)grants:\s*(#.*)?$`)
	grantKindLine = regexp.MustCompile(`^\s*(?:-\s+)?kind:\s*["']?([A-Za-z0-9_.]+)["']?\s*(?:#.*)?$`)
	commentLeadIn = regexp.MustCompile(`^\s*#\s?`)

	// Activation fixtures duplicated from the extracted chart contract
	// tests: this boundary test scans the real activation examples, and
	// the chart-side copies moved to eshu-deploy with their file.
	activationPagerDutyDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	activationJiraDigest      = "sha256:2222222222222222222222222222222222222222222222222222222222222222"

	pagerDutyActivationValues = `    - instanceId: pagerduty-a
      componentId: dev.example.collector.pagerduty
      componentVersion: 0.1.0
      seed:
        image: ghcr.io/example/collector-pagerduty@` + activationPagerDutyDigest + `
      host:
        sourceSystem: pagerduty
        scope:
          id: pagerduty:account:example
          kind: pagerduty_account
      process:
        args: ["--sdk-stdio"]
      config:
        api_token_env: PAGERDUTY_API_TOKEN
      grants:
        - kind: incident.record
          schemaVersions: ["1.0.0", "1.1.0"]
          scope: pagerduty
          expiresIn: 360h
      extraEnv:
        - name: PAGERDUTY_API_TOKEN
          valueFrom:
            secretKeyRef:
              name: pagerduty-credentials
              key: api-token
`
	jiraActivationValues = `    - instanceId: jira-b
      componentId: dev.example.collector.jira
      componentVersion: 0.2.0
      seed:
        image: ghcr.io/example/collector-jira@` + activationJiraDigest + `
      host:
        sourceSystem: jira
        scope:
          id: jira:site:example.atlassian.net
          kind: jira_site
      replicas: 2
`

	// grantExampleSrcCore lives in eshu; grantExampleSrcDeploy lives in the
	// pinned eshu-deploy checkout (checkout-relative paths).
	grantExampleSrcCore = []string{
		"docs/public/extend/reference-pagerduty-extension.md",
		"go/internal/runtime/testdata/8894-component-extension-activations.md",
	}
	grantExampleSrcDeploy = []string{
		"helm/eshu/values.yaml",
		"helm/eshu/README.md",
		"docs/kubernetes/run-an-extracted-collector.md",
		"docs/kubernetes/helm-collector-and-webhook-values.md",
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
	for _, rel := range grantExampleSrcCore {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		sources[rel] = string(body)
	}
	checkout := deployCheckoutRoot(t)
	for _, rel := range grantExampleSrcDeploy {
		body, err := os.ReadFile(filepath.Join(checkout, filepath.FromSlash(rel)))
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
		"helm/eshu/values.yaml",
		"docs/kubernetes/run-an-extracted-collector.md",
	} {
		if found[name] == 0 {
			t.Errorf("%s: no example grant found; the scan no longer sees it", name)
		}
	}
}
