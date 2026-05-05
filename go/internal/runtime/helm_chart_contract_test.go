package runtime

import (
	"bytes"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
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

func renderHelmChart(t *testing.T, args ...string) []helmManifest {
	t.Helper()

	chartPath := filepath.Join(repositoryRoot(t), "deploy", "helm", "eshu")
	cmdArgs := append([]string{"template", "eshu", chartPath}, args...)
	cmd := exec.Command("helm", cmdArgs...)
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
