package yaml

import (
	"os"
	"path/filepath"
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
