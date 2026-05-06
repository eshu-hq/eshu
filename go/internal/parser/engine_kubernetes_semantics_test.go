package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathYAMLKubernetesQualifiedName(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "deployment.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
  namespace: prod
  labels:
    tier: backend
    app: demo
spec:
  template:
    spec:
      containers:
        - name: app
          image: ghcr.io/example/app:1.0.0
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertBucketContainsFieldValue(t, got, "k8s_resources", "qualified_name", "prod/Deployment/demo")
	assertBucketContainsFieldValue(t, got, "k8s_resources", "labels", "app=demo,tier=backend")
}
