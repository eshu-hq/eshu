package component

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifestAcceptsValidComponentPackage(t *testing.T) {
	t.Parallel()

	path := writeManifest(t, validManifestYAML())

	manifest, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v, want nil", err)
	}
	if got, want := manifest.Metadata.ID, "dev.eshu.collector.aws"; got != want {
		t.Fatalf("Metadata.ID = %q, want %q", got, want)
	}
	if got, want := manifest.Spec.ComponentType, ComponentTypeCollector; got != want {
		t.Fatalf("Spec.ComponentType = %q, want %q", got, want)
	}
}

func TestManifestValidateRejectsMissingRequiredIdentity(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Metadata.ID = ""

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want missing ID error")
	}
	if !strings.Contains(err.Error(), "metadata.id is required") {
		t.Fatalf("Validate() error = %v, want metadata.id error", err)
	}
}

func TestManifestValidateRejectsVersionPathTraversal(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Metadata.Version = "../../evil"

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want version validation error")
	}
	if !strings.Contains(err.Error(), "metadata.version") {
		t.Fatalf("Validate() error = %v, want metadata.version error", err)
	}
}

func TestManifestValidateRejectsInvalidSemverVersion(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Metadata.Version = "not-semver"

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want semver validation error")
	}
	if !strings.Contains(err.Error(), "metadata.version") {
		t.Fatalf("Validate() error = %v, want metadata.version error", err)
	}
}

func TestManifestValidateRejectsMalformedArtifactDigest(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.Artifacts[0].Image = "ghcr.io/eshu-hq/components/aws-collector@sha256:abc123"

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want malformed digest error")
	}
	if !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("Validate() error = %v, want sha256 error", err)
	}
}

func TestManifestValidateRejectsInvalidCollectorKind(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.CollectorKinds = []string{"AWS!"}

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want invalid collector kind error")
	}
	if !strings.Contains(err.Error(), "collector kind") {
		t.Fatalf("Validate() error = %v, want collector kind error", err)
	}
}

func TestManifestValidateRequiresDigestPinnedArtifacts(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.Artifacts[0].Image = "ghcr.io/eshu-hq/components/aws-collector:latest"

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want digest-pinned artifact error")
	}
	if !strings.Contains(err.Error(), "digest pinned") {
		t.Fatalf("Validate() error = %v, want digest-pinned error", err)
	}
}

func writeManifest(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "component.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v, want nil", err)
	}
	return path
}

func validManifest() Manifest {
	return Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: Metadata{
			ID:        "dev.eshu.collector.aws",
			Name:      "AWS cloud scanner",
			Publisher: "eshu-hq",
			Version:   "0.1.0",
		},
		Spec: Spec{
			CompatibleCore: ">=0.0.5 <0.1.0",
			ComponentType:  ComponentTypeCollector,
			CollectorKinds: []string{"aws"},
			Artifacts: []Artifact{
				{
					Platform: "linux/amd64",
					Image:    "ghcr.io/eshu-hq/components/aws-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				},
			},
			EmittedFacts: []FactFamily{
				{
					Kind:           "dev.eshu.aws.cloud_resource",
					SchemaVersions: []string{"1.0.0"},
				},
			},
			ConsumerContracts: ConsumerContracts{
				Reducer: ReducerContract{
					Phases: []string{"cloud_resource_uid:canonical_nodes_committed"},
				},
			},
			Telemetry: Telemetry{
				MetricsPrefix: "eshu_dp_aws_",
			},
		},
	}
}

func validManifestYAML() string {
	return `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.eshu.collector.aws
  name: AWS cloud scanner
  publisher: eshu-hq
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.1.0"
  componentType: collector
  collectorKinds:
    - aws
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/components/aws-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: dev.eshu.aws.cloud_resource
      schemaVersions:
        - 1.0.0
  consumerContracts:
    reducer:
      phases:
        - cloud_resource_uid:canonical_nodes_committed
  telemetry:
    metricsPrefix: eshu_dp_aws_
`
}
