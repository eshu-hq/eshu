// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"strings"
	"testing"
)

// TestScannerWorkerRunsFromSatelliteNotInTreeBinaries proves the scanner
// worker no longer ships as an in-tree binary: collection runs from the
// extracted satellite (eshu-hq/eshu-scanner-worker) since #8878, so no
// in-tree Dockerfile build, installer line, cmd entry, or core runtime row
// may remain. The coordinator still plans scanner_worker claims and the
// component-extension worker still hosts the satellite-seeded components.
func TestScannerWorkerRunsFromSatelliteNotInTreeBinaries(t *testing.T) {
	t.Parallel()

	for file, forbidden := range map[string]string{
		"Dockerfile":                        "./cmd/scanner-worker",
		"scripts/install-local-binaries.sh": "./cmd/scanner-worker",
		"go/cmd/README.md":                  "`eshu-scanner-worker`",
	} {
		content := readRepositoryFile(t, "../../..", file)
		if strings.Contains(content, forbidden) {
			t.Fatalf("%s still references in-tree %q, want satellite-only execution", file, forbidden)
		}
	}
	for _, binary := range []string{
		"eshu-scanner-component-image",
		"eshu-scanner-component-sbom",
		"eshu-scanner-component-osruntime",
	} {
		for _, file := range []string{"Dockerfile", "scripts/install-local-binaries.sh"} {
			content := readRepositoryFile(t, "../../..", file)
			if strings.Contains(content, binary) {
				t.Fatalf("%s still builds in-tree %q, want satellite-only execution", file, binary)
			}
		}
	}
	// The service-runtimes lockstep entry stays read: the runtime table
	// still carries the scanner worker as a satellite runtime.
	runtimes := readRepositoryFile(t, "../../..", "docs/public/deployment/service-runtimes.md")
	if !strings.Contains(runtimes, "Scanner Worker (satellite)") {
		t.Fatal("service-runtimes.md lost the Scanner Worker (satellite) row")
	}
}

func TestDockerfileCopiesCollectorSDKBeforeModuleDownload(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "Dockerfile")
	sdkCopy := strings.Index(content, "COPY sdk/go/collector/go.mod ./sdk/go/collector/")
	sdkSourceCopy := strings.Index(content, "COPY sdk/go/collector/ ./sdk/go/collector/")
	download := strings.Index(content, "go mod download")
	build := strings.Index(content, "xx-go build")
	if sdkCopy < 0 {
		t.Fatal("Dockerfile does not copy sdk/go/collector/go.mod before module download")
	}
	if sdkSourceCopy < 0 {
		t.Fatal("Dockerfile does not copy sdk/go/collector source before building binaries")
	}
	if download < 0 {
		t.Fatal("Dockerfile does not run go mod download")
	}
	if build < 0 {
		t.Fatal("Dockerfile does not build Go binaries")
	}
	if sdkCopy > download {
		t.Fatalf("Dockerfile copies sdk/go/collector/go.mod after go mod download; sdk copy index=%d download index=%d", sdkCopy, download)
	}
	if sdkSourceCopy > build {
		t.Fatalf("Dockerfile copies sdk/go/collector source after building binaries; sdk copy index=%d build index=%d", sdkSourceCopy, build)
	}
}

func TestSBOMAttestationCollectorBinaryIsBuiltInstalledAndDocumented(t *testing.T) {
	t.Parallel()

	for file, want := range map[string]string{
		"Dockerfile":                        "-o /go-bin/eshu-collector-sbom-attestation ./cmd/collector-sbom-attestation",
		"scripts/install-local-binaries.sh": "go build -trimpath -ldflags=\"$LDFLAGS\" -o \"$INSTALL_DIR/eshu-collector-sbom-attestation\" ./cmd/collector-sbom-attestation",
		"go/cmd/README.md":                  "`eshu-collector-sbom-attestation`",
		"docs/public/deployment/service-runtimes-collectors.md":        "SBOM Attestation Collector",
		"docs/public/reference/environment-collectors-supply-chain.md": "ESHU_SBOM_ATTESTATION_COLLECTOR_INSTANCE_ID",
	} {
		content := readRepositoryFile(t, "../../..", file)
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", file, want)
		}
	}
	for file, want := range map[string]string{
		"helm/eshu/templates/deployment-sbom-attestation-collector.yaml": "/usr/local/bin/eshu-collector-sbom-attestation",
	} {
		content := readRepositoryFile(t, deployCheckoutRoot(t), file)
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", file, want)
		}
	}
}

func TestPagerDutyCollectorBinaryIsBuiltInstalledAndDocumented(t *testing.T) {
	t.Parallel()

	for file, want := range map[string]string{
		"Dockerfile":                        "-o /go-bin/eshu-collector-pagerduty ./cmd/collector-pagerduty",
		"scripts/install-local-binaries.sh": "go build -trimpath -ldflags=\"$LDFLAGS\" -o \"$INSTALL_DIR/eshu-collector-pagerduty\" ./cmd/collector-pagerduty",
		"go/cmd/README.md":                  "`eshu-collector-pagerduty`",
		"docs/public/deployment/service-runtimes-collectors.md": "https://github.com/eshu-hq/eshu-deploy/blob/main/helm/eshu/templates/deployment-pagerduty-collector.yaml",
		"docs/public/reference/environment-collectors.md":       "ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID",
	} {
		content := readRepositoryFile(t, "../../..", file)
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", file, want)
		}
	}
	for file, want := range map[string]string{
		"helm/eshu/templates/deployment-pagerduty-collector.yaml": "/usr/local/bin/eshu-collector-pagerduty",
	} {
		content := readRepositoryFile(t, deployCheckoutRoot(t), file)
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", file, want)
		}
	}
}

func TestObservabilityCollectorBinariesAreBuiltInstalledAndDocumented(t *testing.T) {
	t.Parallel()

	for _, collector := range []struct {
		binary       string
		commandPath  string
		displayName  string
		envVar       string
		templatePath string
	}{
		{
			binary:       "eshu-collector-grafana",
			commandPath:  "./cmd/collector-grafana",
			displayName:  "Grafana Collector",
			envVar:       "ESHU_GRAFANA_COLLECTOR_INSTANCE_ID",
			templatePath: "helm/eshu/templates/deployment-grafana-collector.yaml",
		},
		{
			binary:       "eshu-collector-prometheus-mimir",
			commandPath:  "./cmd/collector-prometheus-mimir",
			displayName:  "Prometheus/Mimir Collector",
			envVar:       "ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID",
			templatePath: "helm/eshu/templates/deployment-prometheus-mimir-collector.yaml",
		},
		// Tempo collects from its extracted satellite
		// (eshu-hq/eshu-collector-tempo) since #9172: no in-tree binary,
		// command path, env-var row, or monolith-image template remains.
	} {
		for file, want := range map[string]string{
			"Dockerfile":                        "-o /go-bin/" + collector.binary + " " + collector.commandPath,
			"scripts/install-local-binaries.sh": "go build -trimpath -ldflags=\"$LDFLAGS\" -o \"$INSTALL_DIR/" + collector.binary + "\" " + collector.commandPath,
			"go/cmd/README.md":                  "`" + collector.binary + "`",
			"docs/public/deployment/service-runtimes-collectors.md": collector.displayName,
			"docs/public/reference/environment-collectors.md":       collector.envVar,
		} {
			content := readRepositoryFile(t, "../../..", file)
			if !strings.Contains(content, want) {
				t.Fatalf("%s missing %q", file, want)
			}
		}
		deployTemplate := strings.TrimPrefix(collector.templatePath, "deploy/")
		content := readRepositoryFile(t, deployCheckoutRoot(t), deployTemplate)
		if want := "/usr/local/bin/" + collector.binary; !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", deployTemplate, want)
		}
	}
}

func TestCICDRunCollectorBinaryIsBuiltInstalledAndDocumented(t *testing.T) {
	t.Parallel()

	for file, want := range map[string]string{
		"Dockerfile":                        "-o /go-bin/eshu-collector-cicd-run ./cmd/collector-cicd-run",
		"scripts/install-local-binaries.sh": "go build -trimpath -ldflags=\"$LDFLAGS\" -o \"$INSTALL_DIR/eshu-collector-cicd-run\" ./cmd/collector-cicd-run",
		"go/cmd/README.md":                  "`eshu-collector-cicd-run`",
		"docs/public/deployment/service-runtimes-collectors.md":        "CI/CD Run Collector",
		"docs/public/reference/environment-collectors-supply-chain.md": "ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID",
	} {
		content := readRepositoryFile(t, "../../..", file)
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", file, want)
		}
	}
	for file, want := range map[string]string{
		"helm/eshu/templates/deployment-cicd-run-collector.yaml": "/usr/local/bin/eshu-collector-cicd-run",
	} {
		content := readRepositoryFile(t, deployCheckoutRoot(t), file)
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", file, want)
		}
	}
}
