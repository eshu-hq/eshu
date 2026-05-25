package runtime

import (
	"strings"
	"testing"
)

func TestScannerWorkerBinaryIsBuiltInstalledAndDocumented(t *testing.T) {
	t.Parallel()

	for file, want := range map[string]string{
		"Dockerfile":                                 "-o /go-bin/eshu-scanner-worker ./cmd/scanner-worker",
		"scripts/install-local-binaries.sh":          "go build -trimpath -ldflags=\"$LDFLAGS\" -o \"$INSTALL_DIR/eshu-scanner-worker\" ./cmd/scanner-worker",
		"go/cmd/README.md":                           "`eshu-scanner-worker`",
		"docs/public/deployment/service-runtimes.md": "Scanner Worker",
	} {
		content := readRepositoryFile(t, "../../..", file)
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", file, want)
		}
	}
}

func TestSBOMAttestationCollectorBinaryIsBuiltInstalledAndDocumented(t *testing.T) {
	t.Parallel()

	for file, want := range map[string]string{
		"Dockerfile":                        "-o /go-bin/eshu-collector-sbom-attestation ./cmd/collector-sbom-attestation",
		"scripts/install-local-binaries.sh": "go build -trimpath -ldflags=\"$LDFLAGS\" -o \"$INSTALL_DIR/eshu-collector-sbom-attestation\" ./cmd/collector-sbom-attestation",
		"go/cmd/README.md":                  "`eshu-collector-sbom-attestation`",
		"docs/public/deployment/service-runtimes-collectors.md":                 "SBOM Attestation Collector",
		"docs/public/reference/environment-collectors.md":                       "ESHU_SBOM_ATTESTATION_COLLECTOR_INSTANCE_ID",
		"deploy/helm/eshu/templates/deployment-sbom-attestation-collector.yaml": "/usr/local/bin/eshu-collector-sbom-attestation",
	} {
		content := readRepositoryFile(t, "../../..", file)
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", file, want)
		}
	}
}
