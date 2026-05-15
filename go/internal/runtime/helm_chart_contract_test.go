package runtime

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type helmManifest map[string]any

func TestHelmNornicDBNoSecretPathKeepsBoltCredentials(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(t, "--set-string", "neo4j.auth.secretName=")
	for _, manifest := range manifests {
		kind, _ := manifest["kind"].(string)
		if kind != "Deployment" && kind != "StatefulSet" {
			continue
		}

		podSpec := helmPodSpec(t, manifest)
		for _, containerGroup := range []string{"initContainers", "containers"} {
			for _, container := range helmMapSlice(podSpec[containerGroup]) {
				env := helmEnvByName(container)
				if _, ok := env["NEO4J_URI"]; !ok {
					continue
				}
				assertHelmLiteralEnv(t, env, "NEO4J_USERNAME", "neo4j")
				assertHelmLiteralEnv(t, env, "NEO4J_PASSWORD", "change-me")
			}
		}
	}
}

func TestHelmMCPDeploymentStartsHTTPTransport(t *testing.T) {
	t.Parallel()

	deployment := requireHelmManifest(t, renderHelmChart(t), "Deployment", "eshu-mcp-server")
	container := requireHelmContainer(t, deployment, "mcp-server")

	command := helmStringSlice(container["command"])
	want := []string{"eshu", "mcp", "start", "--transport", "http"}
	if !stringSlicesEqual(command, want) {
		t.Fatalf("mcp command = %#v, want %#v", command, want)
	}
}

func TestHelmHTTPRuntimesScrapeMetricsOnHTTPPort(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(t, "--set", "observability.prometheus.enabled=true")
	for _, serviceName := range []string{"eshu-api-metrics", "eshu-mcp-server-metrics"} {
		service := requireHelmManifest(t, manifests, "Service", serviceName)
		ports := helmMapSlice(helmMap(service["spec"])["ports"])
		if len(ports) != 1 {
			t.Fatalf("%s ports = %d, want 1", serviceName, len(ports))
		}
		if got, want := ports[0]["targetPort"], "http"; got != want {
			t.Fatalf("%s targetPort = %#v, want %q", serviceName, got, want)
		}
	}
}

func TestHelmOCIRegistryCollectorDeployment(t *testing.T) {
	t.Parallel()

	defaultManifests := renderHelmChart(t)
	if helmManifestExists(defaultManifests, "Deployment", "eshu-oci-registry-collector") {
		t.Fatal("default chart render included OCI registry collector deployment")
	}

	valuesPath := filepath.Join(t.TempDir(), "oci-values.yaml")
	values := []byte(`
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-oci-registry-collector
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
ociRegistryCollector:
  enabled: true
  instanceId: oci-registry-primary
  pollInterval: 10m
  aws:
    region: us-east-1
  targets:
    - provider: ecr
      registry_id: "123456789012"
      region: us-east-1
      repository: team/api
      references: ["latest"]
    - provider: dockerhub
      repository: library/busybox
      references: ["latest"]
  extraEnv:
    - name: JFROG_USERNAME
      valueFrom:
        secretKeyRef:
          name: jfrog-oci-credentials
          key: username
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write OCI registry collector values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-oci-registry-collector")
	container := requireHelmContainer(t, deployment, "oci-registry-collector")
	command := helmStringSlice(container["command"])
	if !stringSlicesEqual(command, []string{"/usr/local/bin/eshu-collector-oci-registry"}) {
		t.Fatalf("oci registry collector command = %#v", command)
	}

	env := helmEnvByName(container)
	assertHelmLiteralEnv(t, env, "ESHU_OCI_REGISTRY_COLLECTOR_INSTANCE_ID", "oci-registry-primary")
	assertHelmLiteralEnv(t, env, "ESHU_OCI_REGISTRY_POLL_INTERVAL", "10m")
	assertHelmLiteralEnv(t, env, "AWS_REGION", "us-east-1")
	assertHelmLiteralEnv(t, env, "AWS_DEFAULT_REGION", "us-east-1")
	targets := helmString(env["ESHU_OCI_REGISTRY_TARGETS_JSON"]["value"])
	for _, want := range []string{`"provider":"ecr"`, `"provider":"dockerhub"`} {
		if !strings.Contains(targets, want) {
			t.Fatalf("ESHU_OCI_REGISTRY_TARGETS_JSON = %q, missing %s", targets, want)
		}
	}
	if _, ok := env["JFROG_USERNAME"]["valueFrom"]; !ok {
		t.Fatalf("JFROG_USERNAME env = %#v, want valueFrom secret ref", env["JFROG_USERNAME"])
	}

	serviceAccount := requireHelmManifest(t, manifests, "ServiceAccount", "eshu")
	annotations := helmMap(helmMap(serviceAccount["metadata"])["annotations"])
	if got, want := helmString(annotations["eks.amazonaws.com/role-arn"]), "arn:aws:iam::123456789012:role/eshu-oci-registry-collector"; got != want {
		t.Fatalf("IRSA annotation = %q, want %q", got, want)
	}
	requireHelmManifest(t, manifests, "Service", "eshu-oci-registry-collector-metrics")
	requireHelmManifest(t, manifests, "ServiceMonitor", "eshu-oci-registry-collector-metrics")
}

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
    secretName: ""
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

func renderHelmChart(t *testing.T, args ...string) []helmManifest {
	t.Helper()

	chartPath := filepath.Join(repositoryRoot(t), "deploy", "helm", "eshu")
	helmPath, err := exec.LookPath("helm")
	if err != nil {
		t.Skipf("helm binary not found in PATH; install Helm to run chart contract tests: %v", err)
	}
	cmdArgs := append([]string{"template", "eshu", chartPath}, args...)
	cmd := exec.Command(helmPath, cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, output)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(output))
	var manifests []helmManifest
	for {
		var manifest helmManifest
		if err := decoder.Decode(&manifest); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("parse helm template output: %v", err)
		}
		if len(manifest) == 0 {
			continue
		}
		manifests = append(manifests, manifest)
	}
	return manifests
}

func repositoryRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test filename")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

func helmManifestExists(manifests []helmManifest, kind, name string) bool {
	for _, manifest := range manifests {
		if manifest["kind"] != kind {
			continue
		}
		metadata := helmMap(manifest["metadata"])
		if metadata["name"] == name {
			return true
		}
	}
	return false
}

func requireHelmManifest(t *testing.T, manifests []helmManifest, kind, name string) helmManifest {
	t.Helper()

	var seen []string
	for _, manifest := range manifests {
		metadata := helmMap(manifest["metadata"])
		seen = append(seen, manifest["kind"].(string)+"/"+helmString(metadata["name"]))
		if manifest["kind"] != kind {
			continue
		}
		if metadata["name"] == name {
			return manifest
		}
	}
	t.Fatalf("manifest %s/%s missing; saw %#v", kind, name, seen)
	return nil
}

func helmPodSpec(t *testing.T, manifest helmManifest) map[string]any {
	t.Helper()

	spec := helmMap(manifest["spec"])
	if manifest["kind"] == "StatefulSet" {
		spec = helmMap(spec["template"])
		spec = helmMap(spec["spec"])
		return spec
	}
	template := helmMap(spec["template"])
	return helmMap(template["spec"])
}

func requireHelmContainer(t *testing.T, manifest helmManifest, name string) map[string]any {
	t.Helper()

	for _, container := range helmMapSlice(helmPodSpec(t, manifest)["containers"]) {
		if container["name"] == name {
			return container
		}
	}
	t.Fatalf("container %s missing", name)
	return nil
}

func helmEnvByName(container map[string]any) map[string]map[string]any {
	env := make(map[string]map[string]any)
	for _, entry := range helmMapSlice(container["env"]) {
		name, _ := entry["name"].(string)
		if name == "" {
			continue
		}
		env[name] = entry
	}
	return env
}

func assertHelmLiteralEnv(t *testing.T, env map[string]map[string]any, name, want string) {
	t.Helper()

	entry, ok := env[name]
	if !ok {
		t.Fatalf("env %s missing", name)
	}
	if _, ok := entry["valueFrom"]; ok {
		t.Fatalf("env %s uses valueFrom, want literal value", name)
	}
	if got := entry["value"]; got != want {
		t.Fatalf("env %s = %#v, want %q", name, got, want)
	}
}

func helmMap(raw any) map[string]any {
	switch value := raw.(type) {
	case helmManifest:
		return map[string]any(value)
	case map[string]any:
		return value
	case map[any]any:
		converted := make(map[string]any, len(value))
		for key, item := range value {
			if keyString, ok := key.(string); ok {
				converted[keyString] = item
			}
		}
		return converted
	default:
		return nil
	}
}

func helmMapSlice(raw any) []map[string]any {
	items, _ := raw.([]any)
	values := make([]map[string]any, 0, len(items))
	for _, item := range items {
		switch value := item.(type) {
		case helmManifest:
			values = append(values, map[string]any(value))
		case map[string]any:
			values = append(values, value)
		}
	}
	return values
}

func helmStringSlice(raw any) []string {
	items, _ := raw.([]any)
	values := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			values = append(values, value)
		}
	}
	return values
}

func helmString(raw any) string {
	value, _ := raw.(string)
	return value
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
