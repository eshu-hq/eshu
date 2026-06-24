// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func renderHelmChartFailure(t *testing.T, args ...string) string {
	t.Helper()

	chartPath := filepath.Join(repositoryRoot(t), "deploy", "helm", "eshu")
	helmPath, err := exec.LookPath("helm")
	if err != nil {
		t.Skipf("helm binary not found in PATH; install Helm to run chart contract tests: %v", err)
	}
	cmdArgs := append([]string{"template", "eshu", chartPath}, args...)
	cmd := exec.Command(helmPath, cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("helm template succeeded, want failure:\n%s", output)
	}
	return string(output)
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
