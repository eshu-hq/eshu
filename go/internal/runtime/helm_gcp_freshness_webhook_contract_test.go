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

// TestHelmGCPFreshnessWebhookHasNoOIDCConfigSurface proves that
// webhookListener.gcpFreshness specifically has no OIDC-related values key.
// go/cmd/webhook-listener does not implement Pub/Sub push OIDC verification
// (verifyGCPPushOIDC in gcp_freshness_handler.go is stubbed to always return
// false and is never called from the request path); a schema-validated
// "oidc" values block that enforces nothing would be a security footgun — an
// operator could set it and believe the push path is OIDC-authenticated when
// it is not. OIDC verification and its paired Helm values block are tracked
// together in issue #4659, to land in lockstep. The assertion is scoped to
// the gcpFreshness subtree only (not a whole-file "oidc" ban), so it does not
// collide with an OIDC surface a different provider or component might add
// later. If a caller sets oidc.* on gcpFreshness anyway (an unknown key the
// schema does not declare), Helm ignores it silently; this test also proves
// that even then no OIDC-named environment variable is ever rendered.
func TestHelmGCPFreshnessWebhookHasNoOIDCConfigSurface(t *testing.T) {
	t.Parallel()

	valuesYAML := readRepositoryFile(t, "../../..", "deploy/helm/eshu/values.yaml")
	var values map[string]any
	if err := yaml.Unmarshal([]byte(valuesYAML), &values); err != nil {
		t.Fatalf("parse deploy/helm/eshu/values.yaml: %v", err)
	}
	gcpFreshnessDefaults := helmMap(helmMap(values["webhookListener"])["gcpFreshness"])
	if _, ok := gcpFreshnessDefaults["oidc"]; ok {
		t.Fatalf("webhookListener.gcpFreshness.oidc present in deploy/helm/eshu/values.yaml = %#v, want absent (see issue #4659)", gcpFreshnessDefaults["oidc"])
	}

	schemaJSON := readRepositoryFile(t, "../../..", "deploy/helm/eshu/values.schema.json")
	var schema map[string]any
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		t.Fatalf("parse deploy/helm/eshu/values.schema.json: %v", err)
	}
	webhookListenerSchema := helmMap(helmMap(schema["properties"])["webhookListener"])
	gcpFreshnessSchema := helmMap(helmMap(webhookListenerSchema["properties"])["gcpFreshness"])
	gcpFreshnessSchemaProps := helmMap(gcpFreshnessSchema["properties"])
	if _, ok := gcpFreshnessSchemaProps["oidc"]; ok {
		t.Fatalf("webhookListener.gcpFreshness.oidc present in deploy/helm/eshu/values.schema.json properties = %#v, want absent (see issue #4659)", gcpFreshnessSchemaProps["oidc"])
	}

	valuesPath := filepath.Join(t.TempDir(), "webhook-listener-gcp-freshness-unknown-oidc-key-values.yaml")
	unknownOIDCKeyValues := []byte(`
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
	if err := os.WriteFile(valuesPath, unknownOIDCKeyValues, 0o600); err != nil {
		t.Fatalf("write GCP freshness unknown-oidc-key values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-webhook-listener")
	container := requireHelmContainer(t, deployment, "webhook-listener")
	env := helmEnvByName(container)
	for name := range env {
		if strings.Contains(strings.ToUpper(name), "OIDC") {
			t.Fatalf("env %s rendered, but go/cmd/webhook-listener has no OIDC verification consumer (see issue #4659)", name)
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
