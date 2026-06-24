// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHelmWebhookListenerRendersIncidentFreshnessProviders(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "webhook-listener-values.yaml")
	values := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
webhookListener:
  enabled: true
  pagerDuty:
    enabled: true
    path: /webhooks/pagerduty
    secretName: eshu-pagerduty-webhook
    secretKey: secret
    scopeId: pagerduty:account:example
  jira:
    enabled: true
    path: /webhooks/jira
    secretName: eshu-jira-webhook
    secretKey: secret
    scopeId: jira:site:example
  exposure:
    ingress:
      enabled: true
      hosts:
        - host: webhooks.example.test
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write webhook listener values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-webhook-listener")
	env := helmEnvByName(requireHelmContainer(t, deployment, "webhook-listener"))
	assertHelmLiteralEnv(t, env, "ESHU_WEBHOOK_PAGERDUTY_PATH", "/webhooks/pagerduty")
	assertHelmLiteralEnv(t, env, "ESHU_WEBHOOK_PAGERDUTY_SCOPE_ID", "pagerduty:account:example")
	assertHelmLiteralEnv(t, env, "ESHU_WEBHOOK_JIRA_PATH", "/webhooks/jira")
	assertHelmLiteralEnv(t, env, "ESHU_WEBHOOK_JIRA_SCOPE_ID", "jira:site:example")
	assertHelmSecretEnv(t, env, "ESHU_WEBHOOK_PAGERDUTY_SECRET", "eshu-pagerduty-webhook", "secret")
	assertHelmSecretEnv(t, env, "ESHU_WEBHOOK_JIRA_SECRET", "eshu-jira-webhook", "secret")

	ingress := requireHelmManifest(t, manifests, "Ingress", "eshu-webhook-listener")
	paths := helmWebhookIngressPaths(t, ingress)
	for _, want := range []string{"/webhooks/pagerduty", "/webhooks/jira"} {
		if !stringSliceContains(paths, want) {
			t.Fatalf("webhook ingress paths = %#v, missing %q", paths, want)
		}
	}
}

func assertHelmSecretEnv(t *testing.T, env map[string]map[string]any, name, secretName, secretKey string) {
	t.Helper()

	entry, ok := env[name]
	if !ok {
		t.Fatalf("env %s missing", name)
	}
	valueFrom := helmMap(entry["valueFrom"])
	secretKeyRef := helmMap(valueFrom["secretKeyRef"])
	if got := helmString(secretKeyRef["name"]); got != secretName {
		t.Fatalf("env %s secret name = %q, want %q", name, got, secretName)
	}
	if got := helmString(secretKeyRef["key"]); got != secretKey {
		t.Fatalf("env %s secret key = %q, want %q", name, got, secretKey)
	}
}

func helmWebhookIngressPaths(t *testing.T, ingress helmManifest) []string {
	t.Helper()

	spec := helmMap(ingress["spec"])
	paths := []string{}
	for _, rule := range helmMapSlice(spec["rules"]) {
		http := helmMap(rule["http"])
		for _, path := range helmMapSlice(http["paths"]) {
			if value := helmString(path["path"]); value != "" {
				paths = append(paths, value)
			}
		}
	}
	return paths
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
