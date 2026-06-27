// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHelmAWSCloudCollectorUsesDedicatedServiceAccount(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "aws-values.yaml")
	values := []byte(`
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-shared
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
neo4j:
  auth:
    secretName: "neo4j-secrets"
workflowCoordinator:
  enabled: true
  deploymentMode: active
  claimsEnabled: true
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
awsCloudCollector:
  enabled: true
  instanceId: aws-primary
  serviceAccount:
    create: true
    annotations:
      eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-aws-collector
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
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write AWS collector values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-aws-cloud-collector")
	podSpec := helmPodSpec(t, deployment)
	if got, want := helmString(podSpec["serviceAccountName"]), "eshu-aws-cloud-collector"; got != want {
		t.Fatalf("aws collector serviceAccountName = %q, want %q", got, want)
	}

	awsServiceAccount := requireHelmManifest(t, manifests, "ServiceAccount", "eshu-aws-cloud-collector")
	awsAnnotations := helmMap(helmMap(awsServiceAccount["metadata"])["annotations"])
	if got, want := helmString(awsAnnotations["eks.amazonaws.com/role-arn"]), "arn:aws:iam::123456789012:role/eshu-aws-collector"; got != want {
		t.Fatalf("AWS collector IRSA annotation = %q, want %q", got, want)
	}

	sharedServiceAccount := requireHelmManifest(t, manifests, "ServiceAccount", "eshu")
	sharedAnnotations := helmMap(helmMap(sharedServiceAccount["metadata"])["annotations"])
	if got, want := helmString(sharedAnnotations["eks.amazonaws.com/role-arn"]), "arn:aws:iam::123456789012:role/eshu-shared"; got != want {
		t.Fatalf("shared IRSA annotation = %q, want %q", got, want)
	}
}

func TestHelmAWSCloudCollectorOwnsServiceAccountDefaults(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(repositoryRoot(t), "deploy", "helm", "eshu", "values.yaml")
	content, err := os.ReadFile(valuesPath)
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}
	var values map[string]any
	if err := yaml.Unmarshal(content, &values); err != nil {
		t.Fatalf("decode values.yaml: %v", err)
	}
	awsCollector := helmMap(values["awsCloudCollector"])
	if _, ok := awsCollector["serviceAccount"]; !ok {
		t.Fatal("awsCloudCollector.serviceAccount missing from values.yaml")
	}
	terraformStateCollector := helmMap(values["terraformStateCollector"])
	if _, ok := terraformStateCollector["serviceAccount"]; ok {
		t.Fatal("terraformStateCollector.serviceAccount is present, want AWS-only service account defaults")
	}
}

func TestHelmClaimDrivenCollectorsRequireWorkflowCoordinator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values string
	}{
		{
			name: "terraform_state",
			values: `
terraformStateCollector:
  enabled: true
  instanceId: terraform-state-primary
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
`,
		},
		{
			name: "aws_cloud",
			values: `
awsCloudCollector:
  enabled: true
  instanceId: aws-cloud-primary
  collectorInstances:
    - instance_id: aws-cloud-primary
      collector_kind: aws
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        target_scopes:
          - target_scope_id: aws-dev
            provider: aws
            account_ids: ["123456789012"]
            regions: [us-east-1]
`,
		},
		{
			name: "package_registry",
			values: `
packageRegistryCollector:
  enabled: true
  instanceId: package-registry-primary
  collectorInstances:
    - instance_id: package-registry-primary
      collector_kind: package_registry
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: npm
            ecosystem: npm
            registry: https://registry.npmjs.org
            scope_id: npm://registry.npmjs.org/lodash
            packages: [lodash]
            package_limit: 1
            version_limit: 2
            metadata_url: https://registry.npmjs.org/lodash
`,
		},
		{
			name: "scanner_worker",
			values: `
scannerWorker:
  enabled: true
  instanceId: scanner-worker-source
  analyzer: source_analysis
  collectorInstances:
    - instance_id: scanner-worker-source
      collector_kind: scanner_worker
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        analyzer: source_analysis
`,
		},
		{
			name: "vulnerability_intelligence",
			values: `
vulnerabilityIntelligenceCollector:
  enabled: true
  instanceId: vulnerability-intelligence-primary
  collectorInstances:
    - instance_id: vulnerability-intelligence-primary
      collector_kind: vulnerability_intelligence
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - source: cisa_kev
            scope_id: vuln-intel://cisa/kev
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			valuesPath := filepath.Join(t.TempDir(), "claim-values.yaml")
			if err := os.WriteFile(valuesPath, []byte(tt.values), 0o600); err != nil {
				t.Fatalf("write claim collector values: %v", err)
			}

			output := renderHelmChartFailure(t, "-f", valuesPath)
			if !strings.Contains(output, "workflowCoordinator.enabled=true is required when claim-driven collectors are enabled") {
				t.Fatalf("helm template error = %q, want workflow coordinator requirement", output)
			}
		})
	}
}

func TestHelmWorkflowCoordinatorActiveModeForClaimDrivenCollectors(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "active-values.yaml")
	values := []byte(`
workflowCoordinator:
  enabled: true
  deploymentMode: active
  claimsEnabled: true
  collectorInstances:
    - instance_id: package-registry-primary
      collector_kind: package_registry
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: npm
            ecosystem: npm
            registry: https://registry.npmjs.org
            scope_id: npm://registry.npmjs.org/lodash
            packages: [lodash]
            package_limit: 1
            version_limit: 2
            metadata_url: https://registry.npmjs.org/lodash
packageRegistryCollector:
  enabled: true
  instanceId: package-registry-primary
  collectorInstances:
    - instance_id: package-registry-primary
      collector_kind: package_registry
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: npm
            ecosystem: npm
            registry: https://registry.npmjs.org
            scope_id: npm://registry.npmjs.org/lodash
            packages: [lodash]
            package_limit: 1
            version_limit: 2
            metadata_url: https://registry.npmjs.org/lodash
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write active coordinator values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	coordinator := requireHelmManifest(t, manifests, "Deployment", "eshu-workflow-coordinator")
	env := helmEnvByName(requireHelmContainer(t, coordinator, "workflow-coordinator"))
	assertHelmLiteralEnv(t, env, "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE", "active")
	assertHelmLiteralEnv(t, env, "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED", "true")
	if !strings.Contains(helmString(env["ESHU_COLLECTOR_INSTANCES_JSON"]["value"]), `"collector_kind":"package_registry"`) {
		t.Fatalf("ESHU_COLLECTOR_INSTANCES_JSON = %#v, want package_registry instance", env["ESHU_COLLECTOR_INSTANCES_JSON"])
	}
	requireHelmManifest(t, manifests, "Deployment", "eshu-package-registry-collector")
}

func TestHelmPodSecurityContextUsesOnRootMismatch(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "repo-sync-values.yaml")
	values := []byte(`
repoSync:
  enabled: true
  auth:
    method: none
  source:
    mode: filesystem
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write repo sync values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	ingester := requireHelmManifest(t, manifests, "StatefulSet", "eshu")
	securityContext := helmMap(helmPodSpec(t, ingester)["securityContext"])
	if got, want := helmString(securityContext["fsGroupChangePolicy"]), "OnRootMismatch"; got != want {
		t.Fatalf("fsGroupChangePolicy = %q, want %q", got, want)
	}
}

func TestHelmRendersShardEnvForHorizontalIngester(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(t, "--kube-version", "1.32.0", "--set", "ingester.replicas=3")
	ingester := requireHelmManifest(t, manifests, "StatefulSet", "eshu")
	spec := helmMap(ingester["spec"])
	if got, want := spec["replicas"], 3; got != want {
		t.Fatalf("ingester replicas = %#v, want %d", got, want)
	}
	templates := helmMapSlice(spec["volumeClaimTemplates"])
	if len(templates) != 1 {
		t.Fatalf("volumeClaimTemplates length = %d, want 1", len(templates))
	}
	metadata := helmMap(templates[0]["metadata"])
	if got, want := metadata["name"], "data"; got != want {
		t.Fatalf("volumeClaimTemplates[0].metadata.name = %#v, want %q", got, want)
	}

	env := helmEnvByName(requireHelmContainer(t, ingester, "ingester"))
	assertHelmLiteralEnv(t, env, "ESHU_REPO_SHARD_COUNT", "3")
	shardIndex := env["ESHU_REPO_SHARD_INDEX"]
	if shardIndex == nil {
		t.Fatal("ESHU_REPO_SHARD_INDEX env missing")
	}
	valueFrom := helmMap(shardIndex["valueFrom"])
	fieldRef := helmMap(valueFrom["fieldRef"])
	if got, want := fieldRef["fieldPath"], "metadata.labels['apps.kubernetes.io/pod-index']"; got != want {
		t.Fatalf("ESHU_REPO_SHARD_INDEX fieldPath = %#v, want %q", got, want)
	}
}

func TestHelmWorkspaceSetupInitIsPersistentVolumeRetrySafe(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(t, "--kube-version", "1.32.0", "--set", "ingester.replicas=2")
	ingester := requireHelmManifest(t, manifests, "StatefulSet", "eshu")
	podSpec := helmPodSpec(t, ingester)
	var workspaceSetup map[string]any
	for _, container := range helmMapSlice(podSpec["initContainers"]) {
		if container["name"] == "workspace-setup" {
			workspaceSetup = container
			break
		}
	}
	if workspaceSetup == nil {
		t.Fatal("workspace-setup init container missing")
	}

	securityContext := helmMap(workspaceSetup["securityContext"])
	if got, want := securityContext["runAsNonRoot"], true; got != want {
		t.Fatalf("workspace-setup runAsNonRoot = %#v, want %v", got, want)
	}
	if got, want := securityContext["runAsUser"], 10001; got != want {
		t.Fatalf("workspace-setup runAsUser = %#v, want %d", got, want)
	}
	if got, want := securityContext["runAsGroup"], 10001; got != want {
		t.Fatalf("workspace-setup runAsGroup = %#v, want %d", got, want)
	}
	capabilities := helmMap(securityContext["capabilities"])
	if got, want := helmStringSlice(capabilities["drop"]), []string{"ALL"}; !stringSlicesEqual(got, want) {
		t.Fatalf("workspace-setup dropped capabilities = %#v, want %#v", got, want)
	}
	if got := helmStringSlice(capabilities["add"]); len(got) != 0 {
		t.Fatalf("workspace-setup added capabilities = %#v, want none", got)
	}

	command := strings.Join(helmStringSlice(workspaceSetup["command"]), "\n")
	for _, want := range []string{
		"mkdir -p /data/.eshu /data/repos",
		`tmp="$(mktemp /data/repos/.eshuignore.XXXXXX)"`,
		`cp /var/run/eshu-config/.eshuignore "$tmp"`,
		`chmod 0644 "$tmp"`,
		`mv -f "$tmp" /data/repos/.eshuignore`,
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("workspace-setup command = %q, want %q", command, want)
		}
	}
	for _, forbidden := range []string{
		"chown ",
		"cp /var/run/eshu-config/.eshuignore /data/repos/.eshuignore",
	} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("workspace-setup command = %q, want no %q", command, forbidden)
		}
	}
}

func TestHelmIngesterDoesNotRenderShardPodIndexEnv(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(t)
	ingester := requireHelmManifest(t, manifests, "StatefulSet", "eshu")
	env := helmEnvByName(requireHelmContainer(t, ingester, "ingester"))
	for _, name := range []string{"ESHU_REPO_SHARD_COUNT", "ESHU_REPO_SHARD_INDEX"} {
		if _, ok := env[name]; ok {
			t.Fatalf("%s rendered in Helm ingester env; shard env must stay unset until horizontal ingesters are guarded", name)
		}
	}
}

func TestHelmRejectsHorizontalIngesterStaticShardEnvOverrides(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "global count", args: []string{"--kube-version", "1.32.0", "--set", "ingester.replicas=2", "--set-string", "env.ESHU_REPO_SHARD_COUNT=9"}},
		{name: "global index", args: []string{"--kube-version", "1.32.0", "--set", "ingester.replicas=2", "--set-string", "env.ESHU_REPO_SHARD_INDEX=1"}},
		{name: "ingester count", args: []string{"--kube-version", "1.32.0", "--set", "ingester.replicas=2", "--set-string", "ingester.env.ESHU_REPO_SHARD_COUNT=9"}},
		{name: "ingester index", args: []string{"--kube-version", "1.32.0", "--set", "ingester.replicas=2", "--set-string", "ingester.env.ESHU_REPO_SHARD_INDEX=1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := renderHelmChartFailure(t, tc.args...)
			if !strings.Contains(output, "horizontal ingesters manage ESHU_REPO_SHARD_COUNT and ESHU_REPO_SHARD_INDEX") {
				t.Fatalf("helm template error = %q, want static shard env override guard", output)
			}
		})
	}
}

func TestHelmRejectsHorizontalIngesterWithSharedExistingClaim(t *testing.T) {
	t.Parallel()

	output := renderHelmChartFailure(
		t,
		"--kube-version", "1.32.0",
		"--set", "ingester.replicas=2",
		"--set", "ingester.persistence.existingClaim=shared-data",
	)
	if !strings.Contains(output, "ingester.persistence.existingClaim cannot be used with ingester.replicas greater than 1") {
		t.Fatalf("helm template error = %q, want shared PVC guard", output)
	}
}

func TestHelmRejectsHorizontalIngesterOnOldKubernetes(t *testing.T) {
	t.Parallel()

	output := renderHelmChartFailure(
		t,
		"--kube-version", "1.31.0",
		"--set", "ingester.replicas=2",
	)
	if !strings.Contains(output, "requires Kubernetes 1.32 or newer") {
		t.Fatalf("helm template error = %q, want Kubernetes version guard", output)
	}
}
