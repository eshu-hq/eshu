// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func TestPagerDutyCollectorBinaryIsBuiltInstalledAndDocumented(t *testing.T) {
	t.Parallel()

	for file, want := range map[string]string{
		"Dockerfile":                        "-o /go-bin/eshu-collector-pagerduty ./cmd/collector-pagerduty",
		"scripts/install-local-binaries.sh": "go build -trimpath -ldflags=\"$LDFLAGS\" -o \"$INSTALL_DIR/eshu-collector-pagerduty\" ./cmd/collector-pagerduty",
		"go/cmd/README.md":                  "`eshu-collector-pagerduty`",
		"docs/public/deployment/service-runtimes-collectors.md":          "deploy/helm/eshu/templates/deployment-pagerduty-collector.yaml",
		"docs/public/reference/environment-collectors.md":                "ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID",
		"deploy/helm/eshu/templates/deployment-pagerduty-collector.yaml": "/usr/local/bin/eshu-collector-pagerduty",
	} {
		content := readRepositoryFile(t, "../../..", file)
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
			templatePath: "deploy/helm/eshu/templates/deployment-grafana-collector.yaml",
		},
		{
			binary:       "eshu-collector-prometheus-mimir",
			commandPath:  "./cmd/collector-prometheus-mimir",
			displayName:  "Prometheus/Mimir Collector",
			envVar:       "ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID",
			templatePath: "deploy/helm/eshu/templates/deployment-prometheus-mimir-collector.yaml",
		},
		{
			binary:       "eshu-collector-loki",
			commandPath:  "./cmd/collector-loki",
			displayName:  "Loki Collector",
			envVar:       "ESHU_LOKI_COLLECTOR_INSTANCE_ID",
			templatePath: "deploy/helm/eshu/templates/deployment-loki-collector.yaml",
		},
		{
			binary:       "eshu-collector-tempo",
			commandPath:  "./cmd/collector-tempo",
			displayName:  "Tempo Collector",
			envVar:       "ESHU_TEMPO_COLLECTOR_INSTANCE_ID",
			templatePath: "deploy/helm/eshu/templates/deployment-tempo-collector.yaml",
		},
	} {
		for file, want := range map[string]string{
			"Dockerfile":                        "-o /go-bin/" + collector.binary + " " + collector.commandPath,
			"scripts/install-local-binaries.sh": "go build -trimpath -ldflags=\"$LDFLAGS\" -o \"$INSTALL_DIR/" + collector.binary + "\" " + collector.commandPath,
			"go/cmd/README.md":                  "`" + collector.binary + "`",
			"docs/public/deployment/service-runtimes-collectors.md": collector.displayName,
			"docs/public/reference/environment-collectors.md":       collector.envVar,
			collector.templatePath:                                  "/usr/local/bin/" + collector.binary,
		} {
			content := readRepositoryFile(t, "../../..", file)
			if !strings.Contains(content, want) {
				t.Fatalf("%s missing %q", file, want)
			}
		}
	}
}

func TestJiraCollectorBinaryIsBuiltInstalledAndDocumented(t *testing.T) {
	t.Parallel()

	for file, want := range map[string]string{
		"Dockerfile":                        "-o /go-bin/eshu-collector-jira ./cmd/collector-jira",
		"scripts/install-local-binaries.sh": "go build -trimpath -ldflags=\"$LDFLAGS\" -o \"$INSTALL_DIR/eshu-collector-jira\" ./cmd/collector-jira",
		"go/cmd/README.md":                  "`eshu-collector-jira`",
		"docs/public/deployment/service-runtimes-collectors.md":     "deploy/helm/eshu/templates/deployment-jira-collector.yaml",
		"docs/public/reference/environment-collectors.md":           "ESHU_JIRA_COLLECTOR_INSTANCE_ID",
		"deploy/helm/eshu/templates/deployment-jira-collector.yaml": "/usr/local/bin/eshu-collector-jira",
	} {
		content := readRepositoryFile(t, "../../..", file)
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", file, want)
		}
	}
}

func TestCICDRunCollectorBinaryIsBuiltInstalledAndDocumented(t *testing.T) {
	t.Parallel()

	for file, want := range map[string]string{
		"Dockerfile":                        "-o /go-bin/eshu-collector-cicd-run ./cmd/collector-cicd-run",
		"scripts/install-local-binaries.sh": "go build -trimpath -ldflags=\"$LDFLAGS\" -o \"$INSTALL_DIR/eshu-collector-cicd-run\" ./cmd/collector-cicd-run",
		"go/cmd/README.md":                  "`eshu-collector-cicd-run`",
		"docs/public/deployment/service-runtimes-collectors.md":         "CI/CD Run Collector",
		"docs/public/reference/environment-collectors.md":               "ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID",
		"deploy/helm/eshu/templates/deployment-cicd-run-collector.yaml": "/usr/local/bin/eshu-collector-cicd-run",
	} {
		content := readRepositoryFile(t, "../../..", file)
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", file, want)
		}
	}
}
