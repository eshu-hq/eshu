// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	activationPagerDutyDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	activationJiraDigest      = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

// componentActivationsHeader is the shared values prefix for activation
// renders. It deliberately has no workflowCoordinator volume wiring: the chart
// owns the registry volume in activations mode.
const componentActivationsHeader = `contentStore:
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
componentExtensionCollector:
  enabled: true
  trustMode: allowlist
  allowIds: dev.example.collector.pagerduty,dev.example.collector.jira
  allowPublishers: example
  extensionEgressPolicyJSON: '{"mode":"restricted"}'
  pollInterval: 13s
`

const pagerDutyActivationValues = `    - instanceId: pagerduty-a
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

const jiraActivationValues = `    - instanceId: jira-b
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

func componentActivationsValues(activations ...string) string {
	return componentActivationsHeader + "  activations:\n" + strings.Join(activations, "")
}

func writeComponentActivationValues(t *testing.T, values string) string {
	t.Helper()

	valuesPath := filepath.Join(t.TempDir(), "component-activations-values.yaml")
	if err := os.WriteFile(valuesPath, []byte(values), 0o600); err != nil {
		t.Fatalf("write component activation values: %v", err)
	}
	return valuesPath
}

func renderComponentActivations(t *testing.T, values string) []helmManifest {
	t.Helper()
	return renderHelmChart(t, "-f", writeComponentActivationValues(t, values))
}

func TestHelmComponentActivationsRenderOneDeploymentEach(t *testing.T) {
	t.Parallel()

	manifests := renderComponentActivations(t, componentActivationsValues(pagerDutyActivationValues, jiraActivationValues))
	if helmManifestExists(manifests, "Deployment", "eshu-component-extension-collector") {
		t.Fatal("activations mode rendered the single legacy worker Deployment")
	}
	seenInstances := map[string]string{}
	for _, tc := range []struct {
		name     string
		instance string
		replicas int
	}{
		{name: "eshu-cx-pagerduty-a", instance: "pagerduty-a", replicas: 1},
		{name: "eshu-cx-jira-b", instance: "jira-b", replicas: 2},
	} {
		deployment := requireHelmManifest(t, manifests, "Deployment", tc.name)
		if got := helmMap(deployment["spec"])["replicas"]; got != tc.replicas {
			t.Fatalf("%s replicas = %#v, want %d", tc.name, got, tc.replicas)
		}
		container := requireHelmContainer(t, deployment, "component-extension-collector")
		env := helmEnvByName(container)
		assertHelmLiteralEnv(t, env, "ESHU_COMPONENT_COLLECTOR_INSTANCE_ID", tc.instance)
		assertHelmLiteralEnv(t, env, "ESHU_COMPONENT_HOME", "/data/.eshu/components")
		assertHelmLiteralEnv(t, env, "ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL", "13s")
		if previous, dup := seenInstances[tc.instance]; dup {
			t.Fatalf("instance %s rendered twice (%s, %s)", tc.instance, previous, tc.name)
		}
		seenInstances[tc.instance] = tc.name

		// Each worker owns an emptyDir registry; no PVC or other shared volume.
		podSpec := helmPodSpec(t, deployment)
		home := requireHelmNamedVolume(t, podSpec, "component-home")
		if _, ok := home["emptyDir"]; !ok {
			t.Fatalf("%s component-home volume = %#v, want emptyDir", tc.name, home)
		}
		for _, volume := range helmMapSlice(podSpec["volumes"]) {
			if _, shared := volume["persistentVolumeClaim"]; shared {
				t.Fatalf("%s mounts a shared PVC volume %#v", tc.name, volume)
			}
		}
		requireHelmManifest(t, manifests, "PodDisruptionBudget", tc.name)
		requireHelmManifest(t, manifests, "Service", tc.name+"-metrics")
		selector := helmMap(helmMap(helmMap(deployment["spec"])["selector"])["matchLabels"])
		if selector["eshu.dev/component-instance"] != tc.instance {
			t.Fatalf("%s selector = %#v, want instance label %q", tc.name, selector, tc.instance)
		}
	}
	// One ServiceMonitor and NetworkPolicy select every activation by component
	// label, so parity with the single worker holds without duplicating them.
	requireHelmManifest(t, manifests, "ServiceMonitor", "eshu-component-extension-collector-metrics")
	requireHelmManifest(t, manifests, "NetworkPolicy", "eshu-component-extension-collector")
	if helmManifestExists(manifests, "Service", "eshu-component-extension-collector-metrics") {
		t.Fatal("activations mode kept the shared metrics Service, which would scrape every pod twice")
	}
}

func TestHelmComponentActivationsInitContainersSeedThenReconcile(t *testing.T) {
	t.Parallel()

	manifests := renderComponentActivations(t, componentActivationsValues(pagerDutyActivationValues, jiraActivationValues))
	pagerDuty := requireHelmManifest(t, manifests, "Deployment", "eshu-cx-pagerduty-a")
	inits := helmMapSlice(helmPodSpec(t, pagerDuty)["initContainers"])
	if len(inits) != 2 {
		t.Fatalf("pagerduty-a initContainers = %d, want seed then reconcile", len(inits))
	}
	seed, reconcile := inits[0], inits[1]
	if seed["name"] != "seed-pagerduty-a" || reconcile["name"] != "reconcile-pagerduty-a" {
		t.Fatalf("init order = %v, %v, want seed before reconcile", seed["name"], reconcile["name"])
	}
	wantImage := "ghcr.io/example/collector-pagerduty@" + activationPagerDutyDigest
	if seed["image"] != wantImage {
		t.Fatalf("seed image = %v, want %s", seed["image"], wantImage)
	}
	if got := helmStringSlice(seed["command"]); !stringSlicesEqual(got, []string{"/collector", "seed", "--dest", "/data/.eshu/components/runtime/pagerduty-a", "--image-ref", wantImage}) {
		t.Fatalf("seed command = %#v", got)
	}
	if image := helmString(reconcile["image"]); !strings.Contains(image, "eshu") || strings.Contains(image, "collector-pagerduty") {
		t.Fatalf("reconcile image = %q, want the Eshu image", image)
	}

	script := strings.Join(helmStringSlice(reconcile["command"]), "\n")
	grant := `--kind 'incident.record' --schema-version '1.0.0' --schema-version '1.1.0' --scope 'pagerduty' --expires-in '360h'`
	for _, want := range []string{"eshu component grant", grant, "eshu component install", "eshu component enable", "--claims", `--config "${CONFIG_PATH}"`} {
		if !strings.Contains(script, want) {
			t.Fatalf("reconcile script missing %q:\n%s", want, script)
		}
	}
	grantAt := strings.Index(script, "eshu component grant")
	installAt := strings.Index(script, "eshu component install")
	enableAt := strings.Index(script, "eshu component enable")
	if grantAt >= installAt || installAt >= enableAt {
		t.Fatalf("script order grant=%d install=%d enable=%d, want grant, install, enable", grantAt, installAt, enableAt)
	}
	env := helmEnvByName(reconcile)
	assertHelmLiteralEnv(t, env, "COMPONENT_ID", "dev.example.collector.pagerduty")
	assertHelmLiteralEnv(t, env, "COMPONENT_VERSION", "0.1.0")
	assertHelmLiteralEnv(t, env, "INSTANCE_ID", "pagerduty-a")
	assertHelmLiteralEnv(t, env, "CONFIG_PATH", "/etc/eshu/component-activations/pagerduty-a.yaml")
	assertHelmLiteralEnv(t, env, "ALLOW_IDS", "dev.example.collector.pagerduty,dev.example.collector.jira")

	// The Jira activation declares no grants, so its script has no grant step.
	jira := requireHelmManifest(t, manifests, "Deployment", "eshu-cx-jira-b")
	jiraInits := helmMapSlice(helmPodSpec(t, jira)["initContainers"])
	if len(jiraInits) != 2 {
		t.Fatalf("jira-b initContainers = %d, want 2", len(jiraInits))
	}
	if strings.Contains(strings.Join(helmStringSlice(jiraInits[1]["command"]), "\n"), "eshu component grant") {
		t.Fatal("jira-b reconcile script grants without declared grants")
	}
}

// assertHelmInitContainerHardened fails unless init is non-root, has a
// read-only root filesystem, cannot escalate privileges and drops every
// capability. The seed container runs a third-party image with write access to
// the registry volume, so each of these properties must hold for it and for the
// reconcile container.
func assertHelmInitContainerHardened(t *testing.T, owner string, init map[string]any) {
	t.Helper()

	name := helmString(init["name"])
	sc := helmMap(init["securityContext"])
	if sc["runAsNonRoot"] != true {
		t.Errorf("%s init container %s runAsNonRoot = %#v, want true", owner, name, sc["runAsNonRoot"])
	}
	if sc["readOnlyRootFilesystem"] != true {
		t.Errorf("%s init container %s readOnlyRootFilesystem = %#v, want true", owner, name, sc["readOnlyRootFilesystem"])
	}
	if sc["allowPrivilegeEscalation"] != false {
		t.Errorf("%s init container %s allowPrivilegeEscalation = %#v, want false", owner, name, sc["allowPrivilegeEscalation"])
	}
	if drop := helmStringSlice(helmMap(sc["capabilities"])["drop"]); !stringSlicesEqual(drop, []string{"ALL"}) {
		t.Errorf("%s init container %s capabilities.drop = %#v, want [ALL]", owner, name, drop)
	}
}

func TestHelmComponentActivationsInitContainersAreHardened(t *testing.T) {
	t.Parallel()

	manifests := renderComponentActivations(t, componentActivationsValues(pagerDutyActivationValues, jiraActivationValues))
	for _, tc := range []struct {
		deployment string
		wantInits  int
	}{
		{deployment: "eshu-cx-pagerduty-a", wantInits: 2},
		{deployment: "eshu-cx-jira-b", wantInits: 2},
		{deployment: "eshu-workflow-coordinator", wantInits: 4},
	} {
		inits := helmMapSlice(helmPodSpec(t, requireHelmManifest(t, manifests, "Deployment", tc.deployment))["initContainers"])
		if len(inits) != tc.wantInits {
			t.Fatalf("%s initContainers = %d, want %d (seed and reconcile per activation)", tc.deployment, len(inits), tc.wantInits)
		}
		for _, init := range inits {
			assertHelmInitContainerHardened(t, tc.deployment, init)
		}
	}
}

func TestHelmComponentActivationsKeepCredentialsPerWorker(t *testing.T) {
	t.Parallel()

	manifests := renderComponentActivations(t, componentActivationsValues(pagerDutyActivationValues, jiraActivationValues))
	pagerDuty := requireHelmContainer(t, requireHelmManifest(t, manifests, "Deployment", "eshu-cx-pagerduty-a"), "component-extension-collector")
	jira := requireHelmContainer(t, requireHelmManifest(t, manifests, "Deployment", "eshu-cx-jira-b"), "component-extension-collector")
	entry, ok := helmEnvByName(pagerDuty)["PAGERDUTY_API_TOKEN"]
	if !ok || entry["valueFrom"] == nil {
		t.Fatalf("pagerduty-a PAGERDUTY_API_TOKEN = %#v, want a secretKeyRef", entry)
	}
	if _, leaked := helmEnvByName(jira)["PAGERDUTY_API_TOKEN"]; leaked {
		t.Fatal("jira-b worker received pagerduty-a's credential env")
	}

	config := requireHelmManifest(t, manifests, "ConfigMap", "eshu-component-activations")
	data := helmMap(config["data"])
	file := helmString(data["pagerduty-a.yaml"])
	for _, want := range []string{
		"sourceSystem: pagerduty",
		"id: pagerduty:account:example",
		"kind: pagerduty_account",
		"command: /data/.eshu/components/runtime/pagerduty-a/collector",
		"api_token_env: PAGERDUTY_API_TOKEN",
	} {
		if !strings.Contains(file, want) {
			t.Fatalf("pagerduty-a activation config missing %q:\n%s", want, file)
		}
	}
	if strings.Contains(file, "pagerduty-credentials") {
		t.Fatalf("activation config holds the Secret reference:\n%s", file)
	}
	if _, ok := data["jira-b.yaml"]; !ok {
		t.Fatal("ConfigMap missing jira-b.yaml")
	}
}

func TestHelmComponentActivationsGiveCoordinatorTheSameRegistry(t *testing.T) {
	t.Parallel()

	manifests := renderComponentActivations(t, componentActivationsValues(pagerDutyActivationValues, jiraActivationValues))
	coordinator := requireHelmManifest(t, manifests, "Deployment", "eshu-workflow-coordinator")
	podSpec := helmPodSpec(t, coordinator)
	var names []string
	for _, init := range helmMapSlice(podSpec["initContainers"]) {
		names = append(names, helmString(init["name"]))
	}
	want := []string{"seed-pagerduty-a", "reconcile-pagerduty-a", "seed-jira-b", "reconcile-jira-b"}
	if !stringSlicesEqual(names, want) {
		t.Fatalf("coordinator init containers = %v, want %v", names, want)
	}
	container := requireHelmContainer(t, coordinator, "workflow-coordinator")
	assertHelmLiteralEnv(t, helmEnvByName(container), "ESHU_COMPONENT_HOME", "/data/.eshu/components")
	assertHelmReadOnlyVolumeMount(t, container, "component-activations", "/etc/eshu/component-activations")
	assertHelmReadOnlyVolumeMount(t, container, "component-home", "/data/.eshu/components")
	requireHelmNamedVolume(t, podSpec, "component-activations")
	requireHelmNamedVolume(t, podSpec, "component-home")

	// A ConfigMap edit must roll the coordinator and every worker together.
	checksum := func(m helmManifest) string {
		template := helmMap(helmMap(m["spec"])["template"])
		return helmString(helmMap(helmMap(template["metadata"])["annotations"])["checksum/component-activations"])
	}
	sum := checksum(coordinator)
	if sum == "" {
		t.Fatal("coordinator pod has no checksum/component-activations annotation")
	}
	for _, name := range []string{"eshu-cx-pagerduty-a", "eshu-cx-jira-b"} {
		if got := checksum(requireHelmManifest(t, manifests, "Deployment", name)); got != sum {
			t.Fatalf("%s checksum = %q, want coordinator's %q", name, got, sum)
		}
	}
}

func TestHelmComponentActivationsDisabledAndEmptyAreUnchanged(t *testing.T) {
	t.Parallel()

	disabled := strings.Replace(componentActivationsValues(pagerDutyActivationValues), "componentExtensionCollector:\n  enabled: true", "componentExtensionCollector:\n  enabled: false", 1)
	disabled = strings.Replace(disabled, "  deploymentMode: active\n  claimsEnabled: true\n", "", 1)
	manifests := renderComponentActivations(t, disabled)
	for _, name := range []string{"eshu-cx-pagerduty-a", "eshu-component-extension-collector"} {
		if helmManifestExists(manifests, "Deployment", name) {
			t.Fatalf("enabled=false rendered Deployment %s", name)
		}
	}
	if helmManifestExists(manifests, "ConfigMap", "eshu-component-activations") {
		t.Fatal("enabled=false rendered the activation ConfigMap")
	}
	coordinator := helmPodSpec(t, requireHelmManifest(t, manifests, "Deployment", "eshu-workflow-coordinator"))
	if _, ok := coordinator["initContainers"]; ok {
		t.Fatal("enabled=false gave the coordinator init containers")
	}

	// enabled with an empty list is the legacy path: it still demands the
	// hand-mounted registry and fails with the existing message.
	empty := componentActivationsHeader + "  activations: []\n"
	output := renderHelmChartFailure(t, "-f", writeComponentActivationValues(t, empty))
	if !strings.Contains(output, "workflowCoordinator.extraVolumes must mount the component registry") {
		t.Fatalf("empty activations error = %q, want the legacy mount message", output)
	}
}

func TestHelmComponentActivationsOverrideLeaseTimings(t *testing.T) {
	t.Parallel()

	values := componentActivationsValues(
		strings.Replace(pagerDutyActivationValues, "      process:\n        args: [\"--sdk-stdio\"]\n",
			"      claimLeaseTTL: 5m\n      heartbeatInterval: 30s\n      pollInterval: 7s\n"+
				"      process:\n        args: [\"--sdk-stdio\"]\n        stdoutLimitBytes: 33554432\n", 1),
		jiraActivationValues,
	)
	values = strings.Replace(values, "  pollInterval: 13s\n", "  pollInterval: 13s\n  claimLeaseTTL: 90s\n  heartbeatInterval: 45s\n", 1)
	manifests := renderComponentActivations(t, values)

	// The host kills the collector at the initial lease expiry, so each worker
	// must carry its own lease: an activation value wins, the global one is the default.
	for _, tc := range []struct {
		deployment, lease, heartbeat, poll string
	}{
		{"eshu-cx-pagerduty-a", "5m", "30s", "7s"},
		{"eshu-cx-jira-b", "90s", "45s", "13s"},
	} {
		env := helmEnvByName(requireHelmContainer(t, requireHelmManifest(t, manifests, "Deployment", tc.deployment), "component-extension-collector"))
		assertHelmLiteralEnv(t, env, "ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL", tc.lease)
		assertHelmLiteralEnv(t, env, "ESHU_COMPONENT_COLLECTOR_HEARTBEAT_INTERVAL", tc.heartbeat)
		assertHelmLiteralEnv(t, env, "ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL", tc.poll)
	}
	config := helmString(helmMap(requireHelmManifest(t, manifests, "ConfigMap", "eshu-component-activations")["data"])["pagerduty-a.yaml"])
	if !strings.Contains(config, "stdout_limit_bytes: 33554432") {
		t.Fatalf("pagerduty-a activation config missing stdout_limit_bytes:\n%s", config)
	}
}
