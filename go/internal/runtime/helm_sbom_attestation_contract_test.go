// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelmSBOMAttestationCollectorDeployment(t *testing.T) {
	t.Parallel()

	defaultManifests := renderHelmChart(t)
	if helmManifestExists(defaultManifests, "Deployment", "eshu-sbom-attestation-collector") {
		t.Fatal("default chart render included SBOM attestation collector deployment")
	}

	valuesPath := filepath.Join(t.TempDir(), "sbom-attestation-values.yaml")
	values := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
neo4j:
  auth:
    secretName: "neo4j-secrets"
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
    - instance_id: sbom-attestation-primary
      collector_kind: sbom_attestation
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - scope_id: sbom://configured/team-api
            source_type: configured_source
            artifact_kind: sbom
            document_format: cyclonedx
            document_url: https://sbom.example.test/team-api.cdx.json
            subject_digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
sbomAttestationCollector:
  enabled: true
  instanceId: sbom-attestation-primary
  pollInterval: 5s
  claimLeaseTTL: 90s
  heartbeatInterval: 20s
  collectorInstances:
    - instance_id: sbom-attestation-primary
      collector_kind: sbom_attestation
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - scope_id: sbom://configured/team-api
            source_type: configured_source
            artifact_kind: sbom
            document_format: cyclonedx
            document_url: https://sbom.example.test/team-api.cdx.json
            subject_digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
          - scope_id: attestation://oci/team-api
            source_type: oci_referrer
            artifact_kind: attestation
            document_format: in_toto
            registry: registry.example.test
            repository: team/api
            subject_digest: sha256:2222222222222222222222222222222222222222222222222222222222222222
            referrer_digest: sha256:3333333333333333333333333333333333333333333333333333333333333333
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write SBOM attestation collector values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-sbom-attestation-collector")
	container := requireHelmContainer(t, deployment, "sbom-attestation-collector")
	command := helmStringSlice(container["command"])
	if !stringSlicesEqual(command, []string{"/usr/local/bin/eshu-collector-sbom-attestation"}) {
		t.Fatalf("SBOM attestation collector command = %#v", command)
	}

	env := helmEnvByName(container)
	assertHelmLiteralEnv(t, env, "ESHU_SBOM_ATTESTATION_COLLECTOR_INSTANCE_ID", "sbom-attestation-primary")
	assertHelmLiteralEnv(t, env, "ESHU_SBOM_ATTESTATION_POLL_INTERVAL", "5s")
	assertHelmLiteralEnv(t, env, "ESHU_SBOM_ATTESTATION_CLAIM_LEASE_TTL", "90s")
	assertHelmLiteralEnv(t, env, "ESHU_SBOM_ATTESTATION_HEARTBEAT_INTERVAL", "20s")
	instances := helmString(env["ESHU_COLLECTOR_INSTANCES_JSON"]["value"])
	for _, want := range []string{
		`"collector_kind":"sbom_attestation"`,
		`"source_type":"configured_source"`,
		`"source_type":"oci_referrer"`,
		`"artifact_kind":"sbom"`,
		`"artifact_kind":"attestation"`,
		`"document_format":"in_toto"`,
	} {
		if !strings.Contains(instances, want) {
			t.Fatalf("ESHU_COLLECTOR_INSTANCES_JSON = %q, missing %s", instances, want)
		}
	}

	requireHelmManifest(t, manifests, "Service", "eshu-sbom-attestation-collector-metrics")
	requireHelmManifest(t, manifests, "ServiceMonitor", "eshu-sbom-attestation-collector-metrics")
	requireHelmManifest(t, manifests, "NetworkPolicy", "eshu-sbom-attestation-collector")
	requireHelmManifest(t, manifests, "PodDisruptionBudget", "eshu-sbom-attestation-collector")
}

func TestHelmSBOMAttestationCollectorRequiresMatchingCoordinatorInstance(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "sbom-attestation-missing-coordinator-instance.yaml")
	values := []byte(`
workflowCoordinator:
  enabled: true
  deploymentMode: active
  claimsEnabled: true
  collectorInstances:
    - instance_id: scanner-worker-source
      collector_kind: scanner_worker
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        analyzer: source_analysis
sbomAttestationCollector:
  enabled: true
  instanceId: sbom-attestation-primary
  collectorInstances:
    - instance_id: sbom-attestation-primary
      collector_kind: sbom_attestation
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - scope_id: sbom://configured/team-api
            source_type: configured_source
            artifact_kind: sbom
            document_format: cyclonedx
            document_url: https://sbom.example.test/team-api.cdx.json
            subject_digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write missing-coordinator-instance values: %v", err)
	}

	output := renderHelmChartFailure(t, "-f", valuesPath)
	if !strings.Contains(output, "workflowCoordinator.collectorInstances must contain an enabled claim-driven sbom_attestation instance") {
		t.Fatalf("helm template error = %q, want matching coordinator instance requirement", output)
	}
}

func TestHelmSBOMAttestationCollectorRequiresMatchingLocalInstance(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "sbom-attestation-mismatch.yaml")
	values := []byte(`
workflowCoordinator:
  enabled: true
  deploymentMode: active
  claimsEnabled: true
  collectorInstances:
    - instance_id: sbom-attestation-primary
      collector_kind: sbom_attestation
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - scope_id: sbom://configured/team-api
            source_type: configured_source
            artifact_kind: sbom
            document_format: cyclonedx
            document_url: https://sbom.example.test/team-api.cdx.json
            subject_digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
sbomAttestationCollector:
  enabled: true
  instanceId: sbom-attestation-primary
  collectorInstances:
    - instance_id: some-other-id
      collector_kind: sbom_attestation
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - scope_id: sbom://configured/team-api
            source_type: configured_source
            artifact_kind: sbom
            document_format: cyclonedx
            document_url: https://sbom.example.test/team-api.cdx.json
            subject_digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write mismatch values: %v", err)
	}

	output := renderHelmChartFailure(t, "-f", valuesPath)
	if !strings.Contains(output, "sbomAttestationCollector.collectorInstances must contain an enabled claim-driven sbom_attestation instance matching") {
		t.Fatalf("helm template error = %q, want local-mismatch requirement", output)
	}
}
