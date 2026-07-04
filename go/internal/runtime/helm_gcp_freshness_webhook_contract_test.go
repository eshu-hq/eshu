// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestHelmGCPFreshnessWebhookDefaultOff proves the GCP freshness route stays
// unmounted by chart default: no env var, no ingress path, and no secret
// reference render unless an operator explicitly enables
// webhookListener.gcpFreshness, even when the webhook listener itself is
// enabled with another provider (AWS freshness) configured.
func TestHelmGCPFreshnessWebhookDefaultOff(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "webhook-listener-default-values.yaml")
	values := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
webhookListener:
  enabled: true
  awsFreshness:
    enabled: true
    path: /webhooks/aws/eventbridge
    secretName: eshu-aws-freshness-webhook
    tokenKey: token
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write webhook listener values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-webhook-listener")
	container := requireHelmContainer(t, deployment, "webhook-listener")
	env := helmEnvByName(container)
	for _, name := range []string{
		"ESHU_GCP_FRESHNESS_PATH",
		"ESHU_GCP_FRESHNESS_TOKEN",
	} {
		if _, ok := env[name]; ok {
			t.Fatalf("env %s present with GCP freshness disabled, want absent", name)
		}
	}

	if helmManifestExists(manifests, "Ingress", "eshu-webhook-listener") {
		ingress := requireHelmManifest(t, manifests, "Ingress", "eshu-webhook-listener")
		paths := helmWebhookIngressPaths(t, ingress)
		if stringSliceContains(paths, "/webhook/gcp-freshness") {
			t.Fatalf("ingress paths = %#v, must not include GCP freshness path when disabled", paths)
		}
	}
}

// TestHelmGCPFreshnessWebhookEnabledMountsReadOnlySecret proves that enabling
// webhookListener.gcpFreshness renders the route, ingress path, and a
// read-only secretKeyRef env for the shared token — never a literal secret
// value in the rendered manifest.
func TestHelmGCPFreshnessWebhookEnabledMountsReadOnlySecret(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "webhook-listener-gcp-freshness-values.yaml")
	values := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
observability:
  prometheus:
    enabled: true
    serviceMonitor:
      enabled: true
webhookListener:
  enabled: true
  gcpFreshness:
    enabled: true
    path: /webhook/gcp-freshness
    secretName: eshu-gcp-freshness-webhook
    tokenKey: token
  exposure:
    ingress:
      enabled: true
      hosts:
        - host: webhooks.example.test
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write GCP freshness webhook values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-webhook-listener")
	container := requireHelmContainer(t, deployment, "webhook-listener")
	env := helmEnvByName(container)

	assertHelmLiteralEnv(t, env, "ESHU_GCP_FRESHNESS_PATH", "/webhook/gcp-freshness")
	assertHelmSecretEnv(t, env, "ESHU_GCP_FRESHNESS_TOKEN", "eshu-gcp-freshness-webhook", "token")

	ingress := requireHelmManifest(t, manifests, "Ingress", "eshu-webhook-listener")
	paths := helmWebhookIngressPaths(t, ingress)
	if !stringSliceContains(paths, "/webhook/gcp-freshness") {
		t.Fatalf("ingress paths = %#v, missing /webhook/gcp-freshness", paths)
	}

	serviceMonitor := requireHelmManifest(t, manifests, "ServiceMonitor", "eshu-webhook-listener-metrics")
	spec := helmMap(serviceMonitor["spec"])
	endpoints := helmMapSlice(spec["endpoints"])
	if len(endpoints) == 0 {
		t.Fatal("webhook-listener ServiceMonitor has no endpoints")
	}

	// The real security property: ESHU_GCP_FRESHNESS_TOKEN is sourced only via
	// secretKeyRef, never as a literal `value` field, so the token never
	// appears as plaintext in the rendered manifest. assertHelmSecretEnv above
	// already proves the secretKeyRef shape; assert the negative directly here
	// rather than via a substring scan of unrelated env values.
	tokenEnv, ok := env["ESHU_GCP_FRESHNESS_TOKEN"]
	if !ok {
		t.Fatal("env ESHU_GCP_FRESHNESS_TOKEN missing")
	}
	if _, hasLiteral := tokenEnv["value"]; hasLiteral {
		t.Fatalf("env ESHU_GCP_FRESHNESS_TOKEN has a literal value field %#v, want valueFrom.secretKeyRef only", tokenEnv["value"])
	}
}

// TestHelmGCPFreshnessWebhookHasOIDCConfigSurface proves that
// webhookListener.gcpFreshness.oidc is a real, schema-declared, functional
// values block: setting it renders the ESHU_GCP_FRESHNESS_OIDC_* environment
// variables that go/cmd/webhook-listener's OIDC verification path consumes
// (see gcp_freshness_oidc.go and config.go). Issue #4659 replaced the
// negative "no OIDC surface" assertion this test previously enforced —
// go/cmd/webhook-listener now implements real Pub/Sub push OIDC verification,
// so a schema-validated "oidc" block no longer implies protection the
// endpoint does not have.
func TestHelmGCPFreshnessWebhookHasOIDCConfigSurface(t *testing.T) {
	t.Parallel()

	valuesYAML := readRepositoryFile(t, "../../..", "deploy/helm/eshu/values.yaml")
	var values map[string]any
	if err := yaml.Unmarshal([]byte(valuesYAML), &values); err != nil {
		t.Fatalf("parse deploy/helm/eshu/values.yaml: %v", err)
	}
	gcpFreshnessDefaults := helmMap(helmMap(values["webhookListener"])["gcpFreshness"])
	oidcDefaults := helmMap(gcpFreshnessDefaults["oidc"])
	if enabled, _ := oidcDefaults["enabled"].(bool); enabled {
		t.Fatalf("webhookListener.gcpFreshness.oidc.enabled default = %#v, want false (default-off)", oidcDefaults["enabled"])
	}

	schemaJSON := readRepositoryFile(t, "../../..", "deploy/helm/eshu/values.schema.json")
	var schema map[string]any
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		t.Fatalf("parse deploy/helm/eshu/values.schema.json: %v", err)
	}
	webhookListenerSchema := helmMap(helmMap(schema["properties"])["webhookListener"])
	gcpFreshnessSchema := helmMap(helmMap(webhookListenerSchema["properties"])["gcpFreshness"])
	gcpFreshnessSchemaProps := helmMap(gcpFreshnessSchema["properties"])
	oidcSchema := helmMap(gcpFreshnessSchemaProps["oidc"])
	if _, ok := oidcSchema["properties"]; !ok {
		t.Fatalf("webhookListener.gcpFreshness.oidc missing from deploy/helm/eshu/values.schema.json properties, want declared object (see issue #4659)")
	}

	valuesPath := filepath.Join(t.TempDir(), "webhook-listener-gcp-freshness-oidc-values.yaml")
	oidcValues := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
webhookListener:
  enabled: true
  gcpFreshness:
    enabled: true
    path: /webhook/gcp-freshness
    secretName: eshu-gcp-freshness-webhook
    tokenKey: token
    oidc:
      enabled: true
      audience: https://eshu.example.test/webhook/gcp-freshness
      allowedServiceAccountEmail: push-invoker@example-project.iam.gserviceaccount.com
`)
	if err := os.WriteFile(valuesPath, oidcValues, 0o600); err != nil {
		t.Fatalf("write GCP freshness oidc values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-webhook-listener")
	container := requireHelmContainer(t, deployment, "webhook-listener")
	env := helmEnvByName(container)

	assertHelmLiteralEnv(t, env, "ESHU_GCP_FRESHNESS_OIDC_AUDIENCE", "https://eshu.example.test/webhook/gcp-freshness")
	assertHelmLiteralEnv(t, env, "ESHU_GCP_FRESHNESS_OIDC_ALLOWED_SA", "push-invoker@example-project.iam.gserviceaccount.com")
}

// TestHelmGCPFreshnessWebhookOIDCDisabledRendersNoOIDCEnv proves that leaving
// webhookListener.gcpFreshness.oidc at its default-off value renders none of
// the ESHU_GCP_FRESHNESS_OIDC_* environment variables, even when the GCP
// freshness route itself is enabled via the shared token.
func TestHelmGCPFreshnessWebhookOIDCDisabledRendersNoOIDCEnv(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "webhook-listener-gcp-freshness-oidc-disabled-values.yaml")
	values := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
webhookListener:
  enabled: true
  gcpFreshness:
    enabled: true
    path: /webhook/gcp-freshness
    secretName: eshu-gcp-freshness-webhook
    tokenKey: token
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write GCP freshness oidc-disabled values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-webhook-listener")
	container := requireHelmContainer(t, deployment, "webhook-listener")
	env := helmEnvByName(container)
	for name := range env {
		if strings.Contains(strings.ToUpper(name), "OIDC") {
			t.Fatalf("env %s rendered with oidc.enabled unset (default-off), want absent", name)
		}
	}
}

func TestGCPFreshnessWebhookValuesAreDocumented(t *testing.T) {
	t.Parallel()

	for file, want := range map[string]string{
		"deploy/helm/eshu/values.yaml":        "gcpFreshness:",
		"deploy/helm/eshu/values.schema.json": "gcpFreshness",
	} {
		content := readRepositoryFile(t, "../../..", file)
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", file, want)
		}
	}
}
