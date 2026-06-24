// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelmGCPCloudCollectorDeployment(t *testing.T) {
	t.Parallel()

	defaultManifests := renderHelmChart(t)
	if helmManifestExists(defaultManifests, "Deployment", "eshu-gcp-cloud-collector") {
		t.Fatal("default chart render included eshu-gcp-cloud-collector deployment")
	}

	valuesPath := filepath.Join(t.TempDir(), "gcp-cloud-values.yaml")
	values := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
neo4j:
  auth:
    secretName: ""
observability:
  prometheus:
    enabled: true
    serviceMonitor:
      enabled: true
workflowCoordinator:
  enabled: true
  deploymentMode: active
  claimsEnabled: true
  collectorInstances:
    - instance_id: gcp-primary
      collector_kind: gcp
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        live_collection_enabled: true
        scopes:
          - enabled: true
            parent_scope_kind: project
            parent_scope_id: sanitized-project
            asset_type_family: compute
            content_family: resource
            location_bucket: global
            credential_ref: workload-identity
gcpCloudCollector:
  enabled: true
  instanceId: gcp-primary
  pollInterval: 11s
  claimLeaseTTL: 2m
  heartbeatInterval: 25s
  serviceAccount:
    create: true
    annotations:
      iam.gke.io/gcp-service-account: collector@example.invalid
  redaction:
    secretName: gcp-redaction
    keyKey: redaction-key
  collectorInstances:
    - instance_id: gcp-primary
      collector_kind: gcp
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        live_collection_enabled: true
        scopes:
          - enabled: true
            parent_scope_kind: project
            parent_scope_id: sanitized-project
            asset_type_family: compute
            content_family: resource
            location_bucket: global
            credential_ref: workload-identity
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write GCP collector values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	assertClaimCollector(t, manifests, "gcp-cloud-collector", "eshu-collector-gcp-cloud", map[string]string{
		"ESHU_GCP_COLLECTOR_INSTANCE_ID":        "gcp-primary",
		"ESHU_GCP_COLLECTOR_POLL_INTERVAL":      "11s",
		"ESHU_GCP_COLLECTOR_CLAIM_LEASE_TTL":    "2m",
		"ESHU_GCP_COLLECTOR_HEARTBEAT_INTERVAL": "25s",
	})

	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-gcp-cloud-collector")
	container := requireHelmContainer(t, deployment, "gcp-cloud-collector")
	args := helmStringSlice(container["args"])
	wantArgs := []string{"-mode", "claimed-live", "-redaction-key-file", "/var/run/eshu/gcp-redaction/redaction-key"}
	if !stringSlicesEqual(args, wantArgs) {
		t.Fatalf("gcp collector args = %#v, want %#v", args, wantArgs)
	}

	podSpec := helmPodSpec(t, deployment)
	if got, want := helmString(podSpec["serviceAccountName"]), "eshu-gcp-cloud-collector"; got != want {
		t.Fatalf("gcp collector serviceAccountName = %q, want %q", got, want)
	}
	assertHelmReadOnlyVolumeMount(t, container, "gcp-redaction", "/var/run/eshu/gcp-redaction")
	assertHelmSecretVolume(t, podSpec, "gcp-redaction", "gcp-redaction", "redaction-key")

	serviceAccount := requireHelmManifest(t, manifests, "ServiceAccount", "eshu-gcp-cloud-collector")
	annotations := helmMap(helmMap(serviceAccount["metadata"])["annotations"])
	if got, want := helmString(annotations["iam.gke.io/gcp-service-account"]), "collector@example.invalid"; got != want {
		t.Fatalf("GCP service account annotation = %q, want %q", got, want)
	}

	env := helmEnvByName(container)
	instances := helmString(env["ESHU_COLLECTOR_INSTANCES_JSON"]["value"])
	for _, want := range []string{`"collector_kind":"gcp"`, `"live_collection_enabled":true`, `"credential_ref":"workload-identity"`} {
		if !strings.Contains(instances, want) {
			t.Fatalf("GCP collector instances JSON = %q, missing %s", instances, want)
		}
	}
}

func TestHelmGCPCloudCollectorRejectsIncompleteLiveConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		replace func(string) string
		want    string
	}{
		{
			name: "missing_redaction_secret",
			replace: func(values string) string {
				return strings.ReplaceAll(values, "secretName: gcp-redaction", `secretName: ""`)
			},
			want: "/gcpCloudCollector/redaction/secretName",
		},
		{
			name: "live_collection_disabled",
			replace: func(values string) string {
				return strings.ReplaceAll(values, "live_collection_enabled: true", "live_collection_enabled: false")
			},
			want: "live_collection_enabled=true",
		},
		{
			name: "missing_credential_ref",
			replace: func(values string) string {
				return strings.ReplaceAll(values, "credential_ref: workload-identity", `credential_ref: ""`)
			},
			want: "enabled scopes must set credential_ref",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			valuesPath := filepath.Join(t.TempDir(), "gcp-cloud-invalid-values.yaml")
			if err := os.WriteFile(valuesPath, []byte(test.replace(validGCPCloudCollectorValues())), 0o600); err != nil {
				t.Fatalf("write GCP collector values: %v", err)
			}
			output := renderHelmChartFailure(t, "-f", valuesPath)
			if !strings.Contains(output, test.want) {
				t.Fatalf("helm failure = %q, want substring %q", output, test.want)
			}
		})
	}
}

func TestGCPCloudCollectorBinaryIsBuiltInstalledAndDocumented(t *testing.T) {
	t.Parallel()

	for file, want := range map[string]string{
		"Dockerfile":                        "-o /go-bin/eshu-collector-gcp-cloud ./cmd/collector-gcp-cloud",
		"scripts/install-local-binaries.sh": "go build -trimpath -ldflags=\"$LDFLAGS\" -o \"$INSTALL_DIR/eshu-collector-gcp-cloud\" ./cmd/collector-gcp-cloud",
		"go/cmd/README.md":                  "`eshu-collector-gcp-cloud`",
		"docs/public/deployment/service-runtimes-collectors.md":          "deploy/helm/eshu/templates/deployment-gcp-cloud-collector.yaml",
		"docs/public/reference/environment-collectors.md":                "ESHU_GCP_COLLECTOR_INSTANCE_ID",
		"deploy/helm/eshu/templates/deployment-gcp-cloud-collector.yaml": "/usr/local/bin/eshu-collector-gcp-cloud",
	} {
		content := readRepositoryFile(t, "../../..", file)
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", file, want)
		}
	}
}

func validGCPCloudCollectorValues() string {
	return `
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
neo4j:
  auth:
    secretName: ""
workflowCoordinator:
  enabled: true
  deploymentMode: active
  claimsEnabled: true
  collectorInstances:
    - instance_id: gcp-primary
      collector_kind: gcp
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        live_collection_enabled: true
        scopes:
          - enabled: true
            parent_scope_kind: project
            parent_scope_id: sanitized-project
            credential_ref: workload-identity
gcpCloudCollector:
  enabled: true
  instanceId: gcp-primary
  redaction:
    secretName: gcp-redaction
    keyKey: redaction-key
  collectorInstances:
    - instance_id: gcp-primary
      collector_kind: gcp
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        live_collection_enabled: true
        scopes:
          - enabled: true
            parent_scope_kind: project
            parent_scope_id: sanitized-project
            credential_ref: workload-identity
`
}

func assertHelmReadOnlyVolumeMount(t *testing.T, container map[string]any, name, mountPath string) {
	t.Helper()

	for _, mount := range helmMapSlice(container["volumeMounts"]) {
		if helmString(mount["name"]) != name {
			continue
		}
		if got := helmString(mount["mountPath"]); got != mountPath {
			t.Fatalf("volume mount %s path = %q, want %q", name, got, mountPath)
		}
		if got, ok := mount["readOnly"].(bool); !ok || !got {
			t.Fatalf("volume mount %s readOnly = %#v, want true", name, mount["readOnly"])
		}
		return
	}
	t.Fatalf("volume mount %s missing", name)
}

func assertHelmSecretVolume(t *testing.T, podSpec map[string]any, name, secretName, key string) {
	t.Helper()

	for _, volume := range helmMapSlice(podSpec["volumes"]) {
		if helmString(volume["name"]) != name {
			continue
		}
		secret := helmMap(volume["secret"])
		if got := helmString(secret["secretName"]); got != secretName {
			t.Fatalf("volume %s secretName = %q, want %q", name, got, secretName)
		}
		items := helmMapSlice(secret["items"])
		if len(items) != 1 {
			t.Fatalf("volume %s secret items = %#v, want one item", name, items)
		}
		if got := helmString(items[0]["key"]); got != key {
			t.Fatalf("volume %s secret item key = %q, want %q", name, got, key)
		}
		if got := helmString(items[0]["path"]); got != key {
			t.Fatalf("volume %s secret item path = %q, want %q", name, got, key)
		}
		return
	}
	t.Fatalf("secret volume %s missing", name)
}
