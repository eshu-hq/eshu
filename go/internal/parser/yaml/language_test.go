// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseKubernetesResourceDirectly(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "deployment.yaml")
	source := []byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: prod
spec:
  template:
    spec:
      containers:
        - name: api
          image: ghcr.io/eshu-hq/api:1.0.0
`)
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := Parse(path, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	resources := got["k8s_resources"].([]map[string]any)
	if len(resources) != 1 {
		t.Fatalf("k8s_resources length = %d, want 1", len(resources))
	}
	row := resources[0]
	if row["qualified_name"] != "prod/Deployment/api" {
		t.Fatalf("qualified_name = %#v, want prod/Deployment/api", row["qualified_name"])
	}
	if row["container_images"] != "ghcr.io/eshu-hq/api:1.0.0" {
		t.Fatalf("container_images = %#v, want image ref", row["container_images"])
	}
	if got["source"] != string(source) {
		t.Fatalf("source was not preserved under IndexSource")
	}
}

func TestParseCloudFormationIntrinsicDirectly(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "stack.yaml")
	source := []byte(`
AWSTemplateFormatVersion: '2010-09-09'
Parameters:
  BucketName:
    Type: String
Resources:
  Logs:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: !Ref BucketName
Outputs:
  LogsName:
    Value: !Ref Logs
`)
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	resources := got["cloudformation_resources"].([]map[string]any)
	if len(resources) != 1 {
		t.Fatalf("cloudformation_resources length = %d, want 1", len(resources))
	}
	if resources[0]["name"] != "Logs" {
		t.Fatalf("resource name = %#v, want Logs", resources[0]["name"])
	}
	outputs := got["cloudformation_outputs"].([]map[string]any)
	if len(outputs) != 1 {
		t.Fatalf("cloudformation_outputs length = %d, want 1", len(outputs))
	}
}

func TestParseMultiDocumentYAMLWithAnchorsAndMergeKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "resources.yaml")
	source := []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared
  namespace: prod
  labels: &commonLabels
    app: api
    tier: backend
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: prod
  labels:
    <<: *commonLabels
    tier: web
spec:
  template:
    spec:
      containers:
        - name: api
          image: ghcr.io/eshu-hq/api:1.0.0
`)
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	resources := got["k8s_resources"].([]map[string]any)
	if len(resources) != 2 {
		t.Fatalf("k8s_resources length = %d, want 2: %#v", len(resources), resources)
	}
	deployment := yamlRowByName(t, resources, "api")
	if gotLabels, want := deployment["labels"], "app=api,tier=web"; gotLabels != want {
		t.Fatalf("deployment labels = %#v, want %#v", gotLabels, want)
	}
	if gotImage, want := deployment["container_images"], "ghcr.io/eshu-hq/api:1.0.0"; gotImage != want {
		t.Fatalf("deployment container_images = %#v, want %#v", gotImage, want)
	}
}

func TestDecodeDocumentsPreservesQuotedMergeKey(t *testing.T) {
	t.Parallel()

	documents, err := DecodeDocuments(`
"<<": literal
merged:
  <<: &defaults
    app: api
    tier: backend
  tier: web
`)
	if err != nil {
		t.Fatalf("DecodeDocuments() error = %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("DecodeDocuments() length = %d, want 1", len(documents))
	}
	document := documents[0].(map[string]any)
	if got, want := document["<<"], "literal"; got != want {
		t.Fatalf("quoted merge key = %#v, want %#v", got, want)
	}
	merged := document["merged"].(map[string]any)
	if got, want := merged["app"], "api"; got != want {
		t.Fatalf("merged app = %#v, want %#v", got, want)
	}
	if got, want := merged["tier"], "web"; got != want {
		t.Fatalf("merged tier = %#v, want %#v", got, want)
	}
}

func TestDecodeDocumentsRejectsInvalidMergeValue(t *testing.T) {
	t.Parallel()

	_, err := DecodeDocuments(`
metadata:
  labels:
    <<: not-a-map
    app: api
`)
	if err == nil {
		t.Fatal("DecodeDocuments() error = nil, want invalid merge error")
	}
	if !strings.Contains(err.Error(), "merge") {
		t.Fatalf("DecodeDocuments() error = %v, want merge context", err)
	}
}

func TestDecodeDocumentsRejectsRecursiveAlias(t *testing.T) {
	t.Parallel()

	_, err := DecodeDocuments(`
metadata: &metadata
  labels:
    self: *metadata
`)
	if err == nil {
		t.Fatal("DecodeDocuments() error = nil, want recursive alias error")
	}
	if !strings.Contains(err.Error(), "alias cycle") {
		t.Fatalf("DecodeDocuments() error = %v, want alias cycle context", err)
	}
}

func yamlRowByName(t *testing.T, rows []map[string]any, name string) map[string]any {
	t.Helper()

	for _, row := range rows {
		if row["name"] == name {
			return row
		}
	}
	t.Fatalf("missing row %q in %#v", name, rows)
	return nil
}
