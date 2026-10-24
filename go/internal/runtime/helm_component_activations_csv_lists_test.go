// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"strconv"
	"strings"
	"testing"
)

// csvListWant is the normalized form every spaced variant below must render as.
const csvListWant = "dev.example.collector.pagerduty,dev.example.collector.jira"

// csvListVariants are comma-separated trust lists a human may type. Chart
// validation trims each entry, so they validate; the rendered env value and the
// install flags must then carry the same trimmed entries, because policy
// matching in the CLI compares exactly.
var csvListVariants = []struct{ name, raw string }{
	{"space after comma", "dev.example.collector.pagerduty, dev.example.collector.jira"},
	{"tabs", "dev.example.collector.pagerduty,\tdev.example.collector.jira\t"},
	{"leading and trailing spaces", "  dev.example.collector.pagerduty  ,dev.example.collector.jira  "},
	{"empty entries", ",dev.example.collector.pagerduty,, ,dev.example.collector.jira,"},
}

// csvListActivationValues returns activation values whose four trust lists all
// hold raw.
func csvListActivationValues(t *testing.T, raw string) string {
	t.Helper()

	quoted := strconv.Quote(raw)
	header := strings.Replace(componentActivationsHeader,
		"  allowIds: dev.example.collector.pagerduty,dev.example.collector.jira\n  allowPublishers: example\n",
		"  allowIds: "+quoted+"\n  allowPublishers: "+quoted+"\n  revokeIds: "+quoted+"\n  revokePublishers: "+quoted+"\n", 1)
	if header == componentActivationsHeader {
		t.Fatal("component activations header changed; update the csv list test")
	}
	return header + "  activations:\n" + pagerDutyActivationValues
}

func assertCSVListEnv(t *testing.T, env map[string]map[string]any, names ...string) {
	t.Helper()

	for _, name := range names {
		assertHelmLiteralEnv(t, env, name, csvListWant)
	}
}

// TestHelmComponentTrustListsRenderWithoutWhitespace proves a spaced list is
// normalized in every place the chart renders one: the activation reconcile
// init container, the activation worker, the workflow coordinator, and the
// legacy single worker.
func TestHelmComponentTrustListsRenderWithoutWhitespace(t *testing.T) {
	t.Parallel()

	for _, variant := range csvListVariants {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()

			manifests := renderComponentActivations(t, csvListActivationValues(t, variant.raw))
			deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-cx-pagerduty-a")
			_, reconcileEnv := reconcileScriptFromManifests(t, manifests)
			for _, name := range []string{"ALLOW_IDS", "ALLOW_PUBLISHERS", "REVOKE_IDS", "REVOKE_PUBLISHERS"} {
				if got := reconcileEnv[name]; got != csvListWant {
					t.Errorf("reconcile %s = %q, want %q", name, got, csvListWant)
				}
			}
			policy := []string{"ESHU_COMPONENT_ALLOW_IDS", "ESHU_COMPONENT_ALLOW_PUBLISHERS", "ESHU_COMPONENT_REVOKE_IDS", "ESHU_COMPONENT_REVOKE_PUBLISHERS"}
			assertCSVListEnv(t, helmEnvByName(requireHelmContainer(t, deployment, "component-extension-collector")), policy...)
			coordinator := requireHelmManifest(t, manifests, "Deployment", "eshu-workflow-coordinator")
			assertCSVListEnv(t, helmEnvByName(requireHelmContainer(t, coordinator, "workflow-coordinator")), policy...)
		})

		t.Run("legacy "+variant.name, func(t *testing.T) {
			t.Parallel()

			quoted := strconv.Quote(variant.raw)
			values := componentExtensionCollectorValuesWithAllowlist("allowlist", true, true, variant.raw, variant.raw)
			values = strings.Replace(values, "  allowPublishers: "+quoted+"\n",
				"  allowPublishers: "+quoted+"\n  revokeIds: "+quoted+"\n  revokePublishers: "+quoted+"\n", 1)
			manifests := renderHelmChart(t, "-f", writeComponentActivationValues(t, values))
			policy := []string{"ESHU_COMPONENT_ALLOW_IDS", "ESHU_COMPONENT_ALLOW_PUBLISHERS", "ESHU_COMPONENT_REVOKE_IDS", "ESHU_COMPONENT_REVOKE_PUBLISHERS"}
			worker := requireHelmManifest(t, manifests, "Deployment", "eshu-component-extension-collector")
			assertCSVListEnv(t, helmEnvByName(requireHelmContainer(t, worker, "component-extension-collector")), policy...)
			coordinator := requireHelmManifest(t, manifests, "Deployment", "eshu-workflow-coordinator")
			assertCSVListEnv(t, helmEnvByName(requireHelmContainer(t, coordinator, "workflow-coordinator")), policy...)
		})
	}
}

// TestHelmComponentRevokeListOfOnlySeparatorsRendersNoRevocation keeps an
// all-blank revoke list from reaching the CLI as a lone separator.
func TestHelmComponentRevokeListOfOnlySeparatorsRendersNoRevocation(t *testing.T) {
	t.Parallel()

	values := strings.Replace(csvListActivationValues(t, csvListWant), strconv.Quote(csvListWant)+"\n  revokePublishers: "+strconv.Quote(csvListWant),
		`" , ,"`+"\n  revokePublishers: "+`"\t"`, 1)
	if !strings.Contains(values, `revokePublishers: "\t"`) {
		t.Fatal("blank revoke values were not applied")
	}
	manifests := renderComponentActivations(t, values)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-cx-pagerduty-a")
	env := helmEnvByName(requireHelmContainer(t, deployment, "component-extension-collector"))
	for _, name := range []string{"ESHU_COMPONENT_REVOKE_IDS", "ESHU_COMPONENT_REVOKE_PUBLISHERS"} {
		if _, ok := env[name]; ok {
			t.Errorf("%s rendered for an all-blank list", name)
		}
	}
	_, reconcileEnv := reconcileScriptFromManifests(t, manifests)
	for _, name := range []string{"REVOKE_IDS", "REVOKE_PUBLISHERS"} {
		if got := reconcileEnv[name]; got != "" {
			t.Errorf("reconcile %s = %q, want empty", name, got)
		}
	}
}

// TestHelmComponentActivationReconcileScriptPassesTrimmedListArguments runs the
// rendered script from a spaced-list chart against a stub that logs every
// argument it receives, and requires the allow and revoke arguments to arrive
// whole and without whitespace.
func TestHelmComponentActivationReconcileScriptPassesTrimmedListArguments(t *testing.T) {
	t.Parallel()

	logArgs := `for a in "$@"; do printf '[%s]' "$a" >> "${STUB_LOG}"; done; printf '\n' >> "${STUB_LOG}"`
	stub := strings.Replace(stubEshu, `printf '%s\n' "$*" >> "${STUB_LOG}"`, logArgs, 1)
	if stub == stubEshu {
		t.Fatal("stub eshu changed; update the argument-logging stub")
	}
	for _, variant := range csvListVariants {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()

			manifests := renderComponentActivations(t, csvListActivationValues(t, variant.raw))
			script, chartEnv := reconcileScriptFromManifests(t, manifests)
			run := runReconcileScriptWithStub(t, stub, script, chartEnv, map[string]string{"STUB_LIST_JSON": componentListJSON(listedComponent{scriptPagerDutyID, "0.1.0"})}, seedRuntime)
			if run.exit != 0 {
				t.Fatalf("script exit = %d, output:\n%s", run.exit, run.output)
			}
			var install string
			for _, call := range run.calls {
				if strings.HasPrefix(call, "[component][install]") {
					install = call
				}
			}
			if install == "" {
				t.Fatalf("no install call in %q", run.calls)
			}
			for _, flag := range []string{"--allow-id", "--allow-publisher", "--revoke-id", "--revoke-publisher"} {
				want := "[" + flag + "][" + csvListWant + "]"
				if !strings.Contains(install, want) {
					t.Errorf("install call %q missing %s", install, want)
				}
			}
		})
	}
}
