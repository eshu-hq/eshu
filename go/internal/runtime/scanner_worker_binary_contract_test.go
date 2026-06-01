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
