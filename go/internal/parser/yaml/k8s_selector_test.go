// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseKubernetesServiceCapturesSelector proves the parser captures
// spec.selector on a Service, normalized as a sorted k=v string, so the
// query-time SELECTS matcher can evaluate real selector semantics instead of
// falling back to name+namespace matching. See #5343.
func TestParseKubernetesServiceCapturesSelector(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "service.yaml")
	source := []byte(`
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: prod
spec:
  selector:
    tier: web
    app: frontend
`)
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	resources := got["k8s_resources"].([]map[string]any)
	if len(resources) != 1 {
		t.Fatalf("k8s_resources length = %d, want 1", len(resources))
	}
	if gotSelector, want := resources[0]["selector"], "app=frontend,tier=web"; gotSelector != want {
		t.Fatalf("selector = %#v, want %#v", gotSelector, want)
	}
}

// TestParseKubernetesServiceEmitsEmptySelectorWhenAbsent proves the
// "selector" metadata key is always emitted, even as an empty string, so
// downstream tri-state matching can distinguish "genuinely selectorless"
// (key present, empty) from "pre-upgrade data" (key absent).
func TestParseKubernetesServiceEmitsEmptySelectorWhenAbsent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "service.yaml")
	source := []byte(`
apiVersion: v1
kind: Service
metadata:
  name: external
  namespace: prod
spec:
  type: ExternalName
  externalName: example.com
`)
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	resources := got["k8s_resources"].([]map[string]any)
	if len(resources) != 1 {
		t.Fatalf("k8s_resources length = %d, want 1", len(resources))
	}
	selector, ok := resources[0]["selector"]
	if !ok {
		t.Fatalf("selector key missing, want present with empty value")
	}
	if selector != "" {
		t.Fatalf("selector = %#v, want empty string", selector)
	}
}

// TestParseKubernetesDeploymentCapturesPodTemplateLabels proves the parser
// captures spec.template.metadata.labels for pod-template-bearing kinds
// (Deployment, StatefulSet, DaemonSet, ReplicaSet), normalized the same way
// as selector, so the matcher can check selector-subset-of-pod-template-labels.
func TestParseKubernetesDeploymentCapturesPodTemplateLabels(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "deployment.yaml")
	source := []byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: frontend-deploy
  namespace: prod
spec:
  template:
    metadata:
      labels:
        app: frontend
        tier: web
    spec:
      containers:
        - name: web
          image: ghcr.io/eshu-hq/web:1.0.0
`)
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	resources := got["k8s_resources"].([]map[string]any)
	if len(resources) != 1 {
		t.Fatalf("k8s_resources length = %d, want 1", len(resources))
	}
	if gotLabels, want := resources[0]["pod_template_labels"], "app=frontend,tier=web"; gotLabels != want {
		t.Fatalf("pod_template_labels = %#v, want %#v", gotLabels, want)
	}
}

// TestParseKubernetesPodTemplateKindsAllCapturePodTemplateLabels proves the
// parser captures pod_template_labels for every pod-template-bearing kind,
// not just Deployment -- the matcher stays Deployment-only in v1, but the
// parser captures for all four so a future matcher widening does not need a
// parser change.
func TestParseKubernetesPodTemplateKindsAllCapturePodTemplateLabels(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"Deployment", "StatefulSet", "DaemonSet", "ReplicaSet"} {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "workload.yaml")
			source := []byte(`
apiVersion: apps/v1
kind: ` + kind + `
metadata:
  name: demo
  namespace: prod
spec:
  template:
    metadata:
      labels:
        app: demo
`)
			if err := os.WriteFile(path, source, 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			got, err := Parse(path, false, Options{})
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			resources := got["k8s_resources"].([]map[string]any)
			if len(resources) != 1 {
				t.Fatalf("k8s_resources length = %d, want 1", len(resources))
			}
			if gotLabels, want := resources[0]["pod_template_labels"], "app=demo"; gotLabels != want {
				t.Fatalf("pod_template_labels = %#v, want %#v", gotLabels, want)
			}
		})
	}
}

// TestParseKubernetesServiceHasNoPodTemplateLabelsKey proves kinds without a
// pod template (Service, ConfigMap, ...) never emit pod_template_labels, so
// its absence is a meaningful "not a workload" signal, not ambiguous with an
// empty capture.
func TestParseKubernetesServiceHasNoPodTemplateLabelsKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "service.yaml")
	source := []byte(`
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: prod
spec:
  selector:
    app: web
`)
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	resources := got["k8s_resources"].([]map[string]any)
	if len(resources) != 1 {
		t.Fatalf("k8s_resources length = %d, want 1", len(resources))
	}
	if _, ok := resources[0]["pod_template_labels"]; ok {
		t.Fatalf("pod_template_labels present for Service, want absent")
	}
}
