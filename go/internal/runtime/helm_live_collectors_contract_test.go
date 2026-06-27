// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHelmLiveCollectorDeployments covers the kubernetes-live and vault-live
// collector chart surfaces. Both are off by default; when enabled they must
// render their Deployment, metrics Service, ServiceMonitor, NetworkPolicy, and
// PodDisruptionBudget. kubernetes-live additionally renders a read-only RBAC
// surface (ServiceAccount, ClusterRole, ClusterRoleBinding) for in-cluster
// auth, while vault-live wires the redaction key and per-target token through
// Secret references.
func TestHelmLiveCollectorDeployments(t *testing.T) {
	t.Parallel()

	defaultManifests := renderHelmChart(t)
	for _, name := range []string{
		"eshu-kubernetes-live-collector",
		"eshu-vault-live-collector",
	} {
		if helmManifestExists(defaultManifests, "Deployment", name) {
			t.Fatalf("default chart render included %s deployment", name)
		}
	}
	if helmManifestExists(defaultManifests, "ClusterRole", "eshu-kubernetes-live-collector") {
		t.Fatal("default chart render included kubernetes-live ClusterRole")
	}

	valuesPath := filepath.Join(t.TempDir(), "live-collector-values.yaml")
	values := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
neo4j:
  auth:
    secretName: "neo4j-secrets"
    username: example-user
    password: Example-Pass1
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
    - instance_id: vault-live-primary
      collector_kind: vault_live
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - vault_cluster_id: example-vault
            namespace: admin
            address: https://vault.example.test:8200
            token_env: VAULT_READONLY_TOKEN
kubernetesLiveCollector:
  enabled: true
  instanceId: kubernetes-live-primary
  pollInterval: 7m
  serviceAccount:
    create: true
  rbac:
    create: true
  clusters:
    - cluster_id: example-cluster
      display_name: Example Cluster
      provider: example
      environment: production
      source_uri: k8s://example-cluster
      auth_mode: in_cluster
      qps: 20
      burst: 30
vaultLiveCollector:
  enabled: true
  instanceId: vault-live-primary
  pollInterval: 8m
  claimLeaseTTL: 2m
  heartbeatInterval: 30s
  redaction:
    secretName: vault-live-redaction
    keyKey: redaction-key
  collectorInstances:
    - instance_id: vault-live-primary
      collector_kind: vault_live
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - vault_cluster_id: example-vault
            namespace: admin
            address: https://vault.example.test:8200
            token_env: VAULT_READONLY_TOKEN
  extraEnv:
    - name: VAULT_READONLY_TOKEN
      valueFrom:
        secretKeyRef:
          name: vault-live-tokens
          key: primary-token
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write live collector values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)

	assertLiveCollectorWorkload(t, manifests, "kubernetes-live-collector", "eshu-collector-kubernetes-live", map[string]string{
		"ESHU_KUBERNETES_LIVE_COLLECTOR_INSTANCE_ID": "kubernetes-live-primary",
		"ESHU_KUBERNETES_LIVE_POLL_INTERVAL":         "7m",
	})
	kubernetesContainer := requireHelmContainer(t, requireHelmManifest(t, manifests, "Deployment", "eshu-kubernetes-live-collector"), "kubernetes-live-collector")
	clustersJSON := helmString(helmEnvByName(kubernetesContainer)["ESHU_KUBERNETES_LIVE_CLUSTERS_JSON"]["value"])
	if !strings.Contains(clustersJSON, `"clusters":[`) {
		t.Fatalf("kubernetes-live clusters JSON = %q, want object with clusters array", clustersJSON)
	}
	if !strings.Contains(clustersJSON, `"auth_mode":"in_cluster"`) {
		t.Fatalf("kubernetes-live clusters JSON = %q, missing in_cluster auth", clustersJSON)
	}

	assertLiveCollectorWorkload(t, manifests, "vault-live-collector", "eshu-collector-vault-live", map[string]string{
		"ESHU_VAULT_LIVE_COLLECTOR_INSTANCE_ID": "vault-live-primary",
		"ESHU_VAULT_LIVE_POLL_INTERVAL":         "8m",
		"ESHU_VAULT_LIVE_CLAIM_LEASE_TTL":       "2m",
		"ESHU_VAULT_LIVE_HEARTBEAT_INTERVAL":    "30s",
	})
	vaultContainer := requireHelmContainer(t, requireHelmManifest(t, manifests, "Deployment", "eshu-vault-live-collector"), "vault-live-collector")
	vaultEnv := helmEnvByName(vaultContainer)
	if value := helmString(vaultEnv["ESHU_COLLECTOR_INSTANCES_JSON"]["value"]); !strings.Contains(value, `"collector_kind":"vault_live"`) {
		t.Fatalf("vault-live collector instances JSON = %q, missing vault_live kind", value)
	}
	if _, ok := vaultEnv["ESHU_VAULT_LIVE_REDACTION_KEY"]["valueFrom"]; !ok {
		t.Fatalf("vault-live ESHU_VAULT_LIVE_REDACTION_KEY = %#v, want valueFrom", vaultEnv["ESHU_VAULT_LIVE_REDACTION_KEY"])
	}
	if _, ok := vaultEnv["VAULT_READONLY_TOKEN"]["valueFrom"]; !ok {
		t.Fatalf("vault-live VAULT_READONLY_TOKEN = %#v, want valueFrom", vaultEnv["VAULT_READONLY_TOKEN"])
	}
	if _, ok := vaultEnv["ESHU_VAULT_LIVE_COLLECTOR_OWNER_ID"]["valueFrom"]; !ok {
		t.Fatalf("vault-live ESHU_VAULT_LIVE_COLLECTOR_OWNER_ID = %#v, want valueFrom", vaultEnv["ESHU_VAULT_LIVE_COLLECTOR_OWNER_ID"])
	}

	// kubernetes-live read-only RBAC surface.
	requireHelmManifest(t, manifests, "ServiceAccount", "eshu-kubernetes-live-collector")
	requireHelmManifest(t, manifests, "ClusterRole", "eshu-kubernetes-live-collector")
	binding := requireHelmManifest(t, manifests, "ClusterRoleBinding", "eshu-kubernetes-live-collector")
	roleRef := helmMap(binding["roleRef"])
	if roleRef["name"] != "eshu-kubernetes-live-collector" {
		t.Fatalf("kubernetes-live ClusterRoleBinding roleRef = %#v, want eshu-kubernetes-live-collector", roleRef)
	}
	subjects := helmMapSlice(binding["subjects"])
	if len(subjects) != 1 || subjects[0]["name"] != "eshu-kubernetes-live-collector" {
		t.Fatalf("kubernetes-live ClusterRoleBinding subjects = %#v, want bound collector ServiceAccount", subjects)
	}
}

// TestHelmKubernetesLiveCollectorKubeconfigOnlyOmitsRBAC proves the read-only
// ClusterRole/ClusterRoleBinding render only when at least one configured
// cluster uses in_cluster auth. Kubeconfig-only targets authenticate through a
// mounted Secret and must not receive a cluster-wide grant on the local cluster.
func TestHelmKubernetesLiveCollectorKubeconfigOnlyOmitsRBAC(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "kubeconfig-only-values.yaml")
	values := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
neo4j:
  auth:
    secretName: "neo4j-secrets"
    username: example-user
    password: Example-Pass1
kubernetesLiveCollector:
  enabled: true
  instanceId: kubernetes-live-primary
  serviceAccount:
    create: true
  rbac:
    create: false
  kubeconfig:
    secretName: example-kubeconfig
  clusters:
    - cluster_id: example-cluster
      auth_mode: kubeconfig
      kubeconfig_path: /var/run/eshu/kubeconfig/config
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write kubeconfig-only values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	requireHelmManifest(t, manifests, "Deployment", "eshu-kubernetes-live-collector")
	requireHelmManifest(t, manifests, "ServiceAccount", "eshu-kubernetes-live-collector")
	if helmManifestExists(manifests, "ClusterRole", "eshu-kubernetes-live-collector") {
		t.Fatal("kubeconfig-only render included kubernetes-live ClusterRole")
	}
	if helmManifestExists(manifests, "ClusterRoleBinding", "eshu-kubernetes-live-collector") {
		t.Fatal("kubeconfig-only render included kubernetes-live ClusterRoleBinding")
	}
}

// TestHelmKubernetesLiveCollectorRBACWithoutInClusterFails proves leaving
// rbac.create=true with kubeconfig-only targets fails render rather than
// silently granting an unused cluster-wide read.
func TestHelmKubernetesLiveCollectorRBACWithoutInClusterFails(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "rbac-no-incluster-values.yaml")
	values := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
neo4j:
  auth:
    secretName: "neo4j-secrets"
    username: example-user
    password: Example-Pass1
kubernetesLiveCollector:
  enabled: true
  instanceId: kubernetes-live-primary
  rbac:
    create: true
  kubeconfig:
    secretName: example-kubeconfig
  clusters:
    - cluster_id: example-cluster
      auth_mode: kubeconfig
      kubeconfig_path: /var/run/eshu/kubeconfig/config
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write rbac-no-incluster values: %v", err)
	}

	output := renderHelmChartFailure(t, "-f", valuesPath)
	if !strings.Contains(output, "kubernetesLiveCollector.rbac.create=true requires at least one cluster with auth_mode=in_cluster") {
		t.Fatalf("helm template error = %q, want in_cluster RBAC requirement", output)
	}
}

// TestHelmVaultLiveCollectorRequiresMatchingLocalInstance proves the collector's
// collectorInstances must contain an enabled claim-driven vault_live instance
// whose instance_id matches vaultLiveCollector.instanceId; a mismatch would make
// the pod claim nothing and fail in selectVaultLiveInstance at startup.
func TestHelmVaultLiveCollectorRequiresMatchingLocalInstance(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "vault-mismatch-values.yaml")
	values := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
neo4j:
  auth:
    secretName: "neo4j-secrets"
    username: example-user
    password: Example-Pass1
workflowCoordinator:
  enabled: true
  deploymentMode: active
  claimsEnabled: true
  collectorInstances:
    - instance_id: vault-b
      collector_kind: vault_live
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - vault_cluster_id: example-vault
            address: https://vault.example.test:8200
            token_env: VAULT_READONLY_TOKEN
vaultLiveCollector:
  enabled: true
  instanceId: vault-a
  redaction:
    secretName: vault-live-redaction
    keyKey: redaction-key
  collectorInstances:
    - instance_id: vault-b
      collector_kind: vault_live
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - vault_cluster_id: example-vault
            address: https://vault.example.test:8200
            token_env: VAULT_READONLY_TOKEN
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write vault-mismatch values: %v", err)
	}

	output := renderHelmChartFailure(t, "-f", valuesPath)
	if !strings.Contains(output, "vaultLiveCollector.collectorInstances must contain an enabled claim-driven vault_live instance matching") {
		t.Fatalf("helm template error = %q, want vault-live local-mismatch requirement", output)
	}
}

func assertLiveCollectorWorkload(
	t *testing.T,
	manifests []helmManifest,
	component string,
	binary string,
	wantEnv map[string]string,
) {
	t.Helper()

	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-"+component)
	container := requireHelmContainer(t, deployment, component)
	if command := helmStringSlice(container["command"]); !stringSlicesEqual(command, []string{"/usr/local/bin/" + binary}) {
		t.Fatalf("%s command = %#v, want %s", component, command, binary)
	}
	env := helmEnvByName(container)
	for name, want := range wantEnv {
		assertHelmLiteralEnv(t, env, name, want)
	}
	requireHelmManifest(t, manifests, "Service", "eshu-"+component+"-metrics")
	requireHelmManifest(t, manifests, "ServiceMonitor", "eshu-"+component+"-metrics")
	requireHelmManifest(t, manifests, "NetworkPolicy", "eshu-"+component)
	requireHelmManifest(t, manifests, "PodDisruptionBudget", "eshu-"+component)
}
