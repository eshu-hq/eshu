// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMissingMetadataNameAndNamespaceNeverFabricateNil(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing-metadata.yaml")
	source := []byte(`
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  generateName: generated-app-
spec:
  project: default
---
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  generateName: generated-appset-
spec:
  generators: []
  template:
    spec:
      project: default
---
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  generateName: generated-xrd-
spec:
  group: example.org
  names:
    kind: Example
    plural: examples
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  generateName: generated-composition-
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: Example
---
apiVersion: apps/v1
kind: Deployment
metadata:
  generateName: generated-deployment-
spec: {}
---
apiVersion: grafana.integreatly.org/v1beta1
kind: GrafanaDashboard
metadata:
  generateName: generated-dashboard-
spec:
  json: '{"uid":"generated-dashboard","title":"Generated Dashboard","panels":[]}'
`)
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	payload, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, bucket := range []string{
		"argocd_applications",
		"argocd_applicationsets",
		"crossplane_xrds",
		"crossplane_compositions",
	} {
		row := onlyMissingMetadataRow(t, payload, bucket)
		assertMissingMetadataIdentity(t, bucket, row)
	}

	resources := payload["k8s_resources"].([]map[string]any)
	for _, kind := range []string{"Deployment", "GrafanaDashboard"} {
		row := missingMetadataRowByField(t, resources, "kind", kind)
		assertMissingMetadataIdentity(t, "k8s_resources "+kind, row)
		if got, want := row["qualified_name"], kind; got != want {
			t.Errorf("k8s_resources %s qualified_name = %#v, want %#v", kind, got, want)
		}
	}

	dashboards := payload[observabilityDashboardBucket].([]map[string]any)
	dashboard := missingMetadataRowByField(t, dashboards, "dashboard_uid", "generated-dashboard")
	if value, ok := dashboard["resource_name"]; ok {
		t.Errorf("observability resource_name = %#v, want key omitted", value)
	}
	if value, ok := dashboard["namespace"]; ok {
		t.Errorf("observability namespace = %#v, want key omitted", value)
	}
}

func onlyMissingMetadataRow(t *testing.T, payload map[string]any, bucket string) map[string]any {
	t.Helper()

	rows := payload[bucket].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("%s length = %d, want 1: %#v", bucket, len(rows), rows)
	}
	return rows[0]
}

func missingMetadataRowByField(t *testing.T, rows []map[string]any, key string, value any) map[string]any {
	t.Helper()

	for _, row := range rows {
		if row[key] == value {
			return row
		}
	}
	t.Fatalf("missing row with %s=%#v in %#v", key, value, rows)
	return nil
}

func assertMissingMetadataIdentity(t *testing.T, bucket string, row map[string]any) {
	t.Helper()

	if got := row["name"]; got != "" {
		t.Errorf("%s name = %#v, want empty string", bucket, got)
	}
	if value, ok := row["namespace"]; ok {
		t.Errorf("%s namespace = %#v, want key omitted", bucket, value)
	}
}
