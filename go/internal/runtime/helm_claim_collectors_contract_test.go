package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelmClaimDrivenCollectorDeployments(t *testing.T) {
	t.Parallel()

	defaultManifests := renderHelmChart(t)
	for _, name := range []string{
		"eshu-terraform-state-collector",
		"eshu-aws-cloud-collector",
		"eshu-package-registry-collector",
	} {
		if helmManifestExists(defaultManifests, "Deployment", name) {
			t.Fatalf("default chart render included %s deployment", name)
		}
	}

	valuesPath := filepath.Join(t.TempDir(), "claim-collector-values.yaml")
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
terraformStateCollector:
  enabled: true
  instanceId: terraform-state-primary
  pollInterval: 15s
  claimLeaseTTL: 2m
  heartbeatInterval: 30s
  redaction:
    secretName: tfstate-redaction
    keyKey: redaction-key
    rulesetVersion: schema-v1
  collectorInstances:
    - instance_id: terraform-state-primary
      collector_kind: terraform_state
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        target_scopes:
          - target_scope_id: aws-prod
            provider: aws
            deployment_mode: central
            credential_mode: local_workload_identity
            allowed_regions: [us-east-1]
            allowed_backends: [s3]
awsCloudCollector:
  enabled: true
  instanceId: aws-primary
  pollInterval: 20s
  claimLeaseTTL: 3m
  heartbeatInterval: 45s
  redaction:
    secretName: aws-redaction
    keyKey: redaction-key
  collectorInstances:
    - instance_id: aws-primary
      collector_kind: aws
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        target_scopes:
          - account_id: "123456789012"
            allowed_regions: [us-east-1]
            allowed_services: [iam]
            credentials:
              mode: local_workload_identity
packageRegistryCollector:
  enabled: true
  instanceId: package-registry-primary
  pollInterval: 25s
  claimLeaseTTL: 4m
  heartbeatInterval: 50s
  collectorInstances:
    - instance_id: package-registry-primary
      collector_kind: package_registry
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: jfrog
            ecosystem: npm
            registry: https://artifacts.example.test
            scope_id: npm://artifacts.example.test/team/app
            packages: ["@team/app"]
            metadata_url: https://artifacts.example.test/api/npm/team/app
            document_format: artifactory_package
            username_env: PACKAGE_REGISTRY_USERNAME
            password_env: PACKAGE_REGISTRY_PASSWORD
  extraEnv:
    - name: PACKAGE_REGISTRY_USERNAME
      valueFrom:
        secretKeyRef:
          name: package-registry-credentials
          key: username
    - name: PACKAGE_REGISTRY_PASSWORD
      valueFrom:
        secretKeyRef:
          name: package-registry-credentials
          key: password
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write claim collector values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	assertClaimCollector(t, manifests, "terraform-state-collector", "eshu-collector-terraform-state", map[string]string{
		"ESHU_TFSTATE_COLLECTOR_INSTANCE_ID":        "terraform-state-primary",
		"ESHU_TFSTATE_COLLECTOR_POLL_INTERVAL":      "15s",
		"ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL":    "2m",
		"ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL": "30s",
		"ESHU_TFSTATE_REDACTION_RULESET_VERSION":    "schema-v1",
	})
	assertClaimCollector(t, manifests, "aws-cloud-collector", "eshu-collector-aws-cloud", map[string]string{
		"ESHU_AWS_COLLECTOR_INSTANCE_ID":        "aws-primary",
		"ESHU_AWS_COLLECTOR_POLL_INTERVAL":      "20s",
		"ESHU_AWS_COLLECTOR_CLAIM_LEASE_TTL":    "3m",
		"ESHU_AWS_COLLECTOR_HEARTBEAT_INTERVAL": "45s",
	})
	assertClaimCollector(t, manifests, "package-registry-collector", "eshu-collector-package-registry", map[string]string{
		"ESHU_PACKAGE_REGISTRY_COLLECTOR_INSTANCE_ID": "package-registry-primary",
		"ESHU_PACKAGE_REGISTRY_POLL_INTERVAL":         "25s",
		"ESHU_PACKAGE_REGISTRY_CLAIM_LEASE_TTL":       "4m",
		"ESHU_PACKAGE_REGISTRY_HEARTBEAT_INTERVAL":    "50s",
	})
	assertHelmValueFromEnv(t, manifests, "terraform-state-collector", "ESHU_TFSTATE_REDACTION_KEY")
	assertHelmValueFromEnv(t, manifests, "aws-cloud-collector", "ESHU_AWS_REDACTION_KEY")
	assertHelmValueFromEnv(t, manifests, "package-registry-collector", "PACKAGE_REGISTRY_USERNAME")
	assertHelmValueFromEnv(t, manifests, "package-registry-collector", "PACKAGE_REGISTRY_PASSWORD")
}

func assertClaimCollector(
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
	if value := helmString(env["ESHU_COLLECTOR_INSTANCES_JSON"]["value"]); !strings.Contains(value, `"claims_enabled":true`) {
		t.Fatalf("%s collector instances JSON = %q, missing claims_enabled=true", component, value)
	}
	requireHelmManifest(t, manifests, "Service", "eshu-"+component+"-metrics")
	requireHelmManifest(t, manifests, "ServiceMonitor", "eshu-"+component+"-metrics")
	requireHelmManifest(t, manifests, "NetworkPolicy", "eshu-"+component)
	requireHelmManifest(t, manifests, "PodDisruptionBudget", "eshu-"+component)
}

func assertHelmValueFromEnv(t *testing.T, manifests []helmManifest, component, name string) {
	t.Helper()

	container := requireHelmContainer(t, requireHelmManifest(t, manifests, "Deployment", "eshu-"+component), component)
	env := helmEnvByName(container)
	if _, ok := env[name]["valueFrom"]; !ok {
		t.Fatalf("%s env %s = %#v, want valueFrom", component, name, env[name])
	}
}
