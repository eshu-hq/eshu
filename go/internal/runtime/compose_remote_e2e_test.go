// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"fmt"
	"strings"
	"testing"
)

func TestRemoteE2EComposeDefinesCorpusPreflight(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	preflight := requireComposeService(t, doc, "remote-e2e-corpus-preflight")

	assertComposeEnv(t, preflight, "ESHU_REMOTE_E2E_CORPUS_MODE", "${ESHU_REMOTE_E2E_CORPUS_MODE:-smoke}")
	assertComposeEnv(t, preflight, "ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT", "${ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT:-0}")
	assertComposeEnv(t, preflight, "ESHU_REMOTE_E2E_EXPECTED_REPOSITORY_COUNT", "${ESHU_REMOTE_E2E_EXPECTED_REPOSITORY_COUNT:-}")
	assertComposeEnv(t, preflight, "ESHU_FILESYSTEM_HOST_ROOT", "${ESHU_FILESYSTEM_HOST_ROOT:-./tests/fixtures/ecosystems}")
	assertComposeVolumeContains(t, preflight, "${ESHU_FILESYSTEM_HOST_ROOT:-./tests/fixtures/ecosystems}:/fixtures:ro")
	assertComposeVolumeContains(t, preflight, "./scripts/remote-e2e-corpus-preflight.sh:/usr/local/bin/remote-e2e-corpus-preflight.sh:ro")
	assertComposeScriptContains(t, preflight, "remote-e2e-corpus-preflight.sh")

	for _, serviceName := range []string{"bootstrap-index", "workflow-coordinator"} {
		service := requireComposeService(t, doc, serviceName)
		assertComposeDependency(t, service, "remote-e2e-corpus-preflight")
	}
}

func TestRemoteE2EExampleEnvDefaultsToSmokeCorpusPreflight(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, deployCheckoutRoot(t), ".env.remote-e2e.example")
	for _, want := range []string{
		"ESHU_REMOTE_E2E_CORPUS_MODE=smoke",
		"ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT=0",
		"ESHU_FILESYSTEM_HOST_ROOT=./tests/fixtures/ecosystems",
		"ESHU_CANONICAL_WRITE_TIMEOUT=120s",
		"ESHU_API_KEY=",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf(".env.remote-e2e.example missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"ESHU_REMOTE_E2E_CORPUS_MODE=full",
		"ESHU_FILESYSTEM_HOST_ROOT=/absolute/path/to/full-corpus",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf(".env.remote-e2e.example should not default to full corpus value %q", forbidden)
		}
	}
}

func TestRemoteE2EComposeExercisesTerraformStateBackendFilterDiscovery(t *testing.T) {
	t.Parallel()

	content := readRemoteE2EComposeSource(t)
	for _, want := range []string{
		`"backend_filters"`,
		`"target_scope_id": "aws-e2e"`,
		`"backend_kind": "s3"`,
		`"bucket": "${ESHU_TFSTATE_S3_BUCKET:?set ESHU_TFSTATE_S3_BUCKET}"`,
		`"key": "${ESHU_TFSTATE_S3_KEY:?set ESHU_TFSTATE_S3_KEY}"`,
		`"region": "${ESHU_TFSTATE_S3_REGION:?set ESHU_TFSTATE_S3_REGION}"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("docker-compose.remote-e2e.yaml missing Terraform-state backend filter term %q", want)
		}
	}
}

func TestRemoteE2EComposeUsesResponsiveWorkflowReconcileWindow(t *testing.T) {
	t.Parallel()

	compose := readRemoteE2EComposeSource(t)
	if !strings.Contains(compose, "ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL:-30s") {
		t.Fatal("remote E2E Compose should default workflow reconcile to 30s so derived collector targets are planned after Git facts land")
	}

	exampleEnv := readRepositoryFile(t, deployCheckoutRoot(t), ".env.remote-e2e.example")
	if !strings.Contains(exampleEnv, "ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL=30s") {
		t.Fatal(".env.remote-e2e.example should document the responsive reconcile window")
	}
}

func TestRemoteE2EComposeUsesProductionCanonicalWriteTimeout(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	for _, serviceName := range []string{
		"bootstrap-index",
		"eshu",
		"mcp-server",
		"ingester",
		"projector",
		"resolution-engine",
		"workflow-coordinator",
		"collector-terraform-state",
		"collector-oci-registry",
		"collector-package-registry",
		"collector-security-alerts",
		"collector-vulnerability-intelligence",
		"collector-aws-cloud",
		"scanner-worker",
	} {
		service := requireComposeService(t, doc, serviceName)
		assertComposeEnv(t, service, "ESHU_CANONICAL_WRITE_TIMEOUT", "${ESHU_CANONICAL_WRITE_TIMEOUT:-120s}")
	}
}

func TestRemoteE2EComposeRunsProjectorForHostedCollectorSourceLocalWork(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	projector := requireComposeService(t, doc, "projector")

	if fmt.Sprint(projector.Command) != "[/usr/local/bin/eshu-projector]" {
		t.Fatalf("projector command = %#v, want eshu-projector", projector.Command)
	}
	assertComposeEnv(t, projector, "ESHU_PROJECTOR_WORKERS", "${ESHU_PROJECTOR_WORKERS:-}")
	assertComposeDependency(t, projector, "db-migrate")
	assertComposeDependency(t, projector, "bootstrap-index")
	assertComposePortContains(t, projector, "${ESHU_PROJECTOR_HTTP_PORT:-18084}:8080")
	assertComposePortContains(t, projector, "${ESHU_PROJECTOR_METRICS_PORT:-19475}:9464")

	for _, serviceName := range []string{
		"collector-terraform-state",
		"collector-oci-registry",
		"collector-package-registry",
		"collector-security-alerts",
		"collector-vulnerability-intelligence",
		"collector-aws-cloud",
		"scanner-worker",
	} {
		service := requireComposeService(t, doc, serviceName)
		assertComposeDependency(t, service, "projector")
	}
}

func TestRemoteE2EComposeSharesGeneratedAPIKeyState(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	for _, serviceName := range []string{"eshu", "mcp-server"} {
		service := requireComposeService(t, doc, serviceName)
		assertComposeEnv(t, service, "ESHU_HOME", "/data/.eshu")
		assertComposeEnv(t, service, "ESHU_API_KEY", "${ESHU_API_KEY:-}")
		assertComposeEnv(t, service, "ESHU_AUTO_GENERATE_API_KEY", "true")
	}
}

func TestRemoteE2EComposeRestartsRuntimeServicesAfterTransientStoreStartup(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	for _, serviceName := range []string{
		"db-migrate",
		"bootstrap-index",
		"eshu",
		"mcp-server",
		"ingester",
		"projector",
		"resolution-engine",
		"workflow-coordinator",
		"webhook-satellite",
		"webhook-intake",
		"collector-terraform-state",
		"collector-oci-registry",
		"collector-package-registry",
		"collector-vulnerability-intelligence",
		"collector-aws-cloud",
		"scanner-worker",
	} {
		service := requireComposeService(t, doc, serviceName)
		if service.Restart != "on-failure" {
			t.Fatalf("%s restart = %q, want on-failure", serviceName, service.Restart)
		}
	}
}

func TestRemoteE2EComposeKeepsWorkerPprofDisabledByDefault(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	for _, serviceName := range remoteE2EWorkerPprofServices() {
		service := requireComposeService(t, doc, serviceName)
		assertComposeEnvMissing(t, service, "ESHU_PPROF_ADDR")
		assertComposePortMissing(t, service, "6060")
	}
}

func TestRemoteE2EWorkerPprofOverlayBindsWorkersToHostLoopback(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.pprof.yaml")
	for serviceName, hostPort := range map[string]string{
		"bootstrap-index":                      "19660",
		"ingester":                             "19661",
		"resolution-engine":                    "19662",
		"workflow-coordinator":                 "19663",
		"collector-terraform-state":            "19664",
		"collector-oci-registry":               "19665",
		"collector-package-registry":           "19666",
		"collector-aws-cloud":                  "19667",
		"collector-confluence":                 "19668",
		"collector-vulnerability-intelligence": "19670",
		"projector":                            "19669",
		"scanner-worker":                       "19671",
		"collector-security-alerts":            "19673",
	} {
		service := requireComposeService(t, doc, serviceName)
		assertComposeEnv(t, service, "ESHU_PPROF_ADDR", "0.0.0.0:6060")
		assertComposePortContains(t, service, "127.0.0.1:"+hostPort+":6060")
	}
}

func TestHostedWorkerCommandsStartPprofServer(t *testing.T) {
	t.Parallel()

	// The scanner worker has no in-tree main since #9741 (its sources are being
	// ported to eshu-hq/eshu-scanner-worker, #8878 box 10), so no in-tree
	// pprof assertion. The vulnerability intelligence service runs the
	// component-extension host binary since #9999, so the host's main carries
	// its pprof assertion.
	for _, sourcePath := range []string{
		"go/cmd/collector-git/main.go",
		"go/cmd/collector-oci-registry/main.go",
		"go/cmd/collector-package-registry/main.go",
		"go/cmd/collector-security-alerts/main.go",
		"go/cmd/collector-component-extension/main.go",
		"go/cmd/projector/main.go",
		"go/cmd/workflow-coordinator/main.go",
	} {
		content := readRepositoryFile(t, "../../..", sourcePath)
		for _, want := range []string{
			"runtimecfg.NewPprofServer(os.Getenv)",
			"pprof server listening",
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("%s missing pprof startup term %q", sourcePath, want)
			}
		}
	}
}

func TestRemoteE2EComposeIncludesSecurityAlertCollector(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	coordinator := requireComposeService(t, doc, "workflow-coordinator")
	assertComposeDependency(t, coordinator, "collector-security-alerts-preflight")

	preflight := requireComposeService(t, doc, "collector-security-alerts-preflight")
	if fmt.Sprint(preflight.Command) != "[/usr/local/bin/eshu-collector-security-alerts --preflight-provider-access]" {
		t.Fatalf("preflight command = %#v, want eshu-collector-security-alerts provider preflight", preflight.Command)
	}
	assertComposeEnv(t, preflight, "ESHU_SECURITY_ALERT_COLLECTOR_INSTANCE_ID", "remote-e2e-security-alert")
	assertComposeEnv(t, preflight, "GITHUB_TOKEN", "${ESHU_SECURITY_ALERT_GITHUB_TOKEN:?set ESHU_SECURITY_ALERT_GITHUB_TOKEN}")

	service := requireComposeService(t, doc, "collector-security-alerts")
	if fmt.Sprint(service.Command) != "[/usr/local/bin/eshu-collector-security-alerts]" {
		t.Fatalf("collector command = %#v, want eshu-collector-security-alerts", service.Command)
	}
	assertComposeEnv(t, service, "ESHU_SECURITY_ALERT_COLLECTOR_INSTANCE_ID", "remote-e2e-security-alert")
	assertComposeEnv(t, service, "ESHU_SECURITY_ALERT_COLLECTOR_OWNER_ID", "remote-e2e-security-alert-worker")
	assertComposeEnv(t, service, "ESHU_SECURITY_ALERT_POLL_INTERVAL", "${ESHU_SECURITY_ALERT_POLL_INTERVAL:-2s}")
	assertComposeEnv(t, service, "GITHUB_TOKEN", "${ESHU_SECURITY_ALERT_GITHUB_TOKEN:?set ESHU_SECURITY_ALERT_GITHUB_TOKEN}")
	assertComposePortContains(t, service, "${ESHU_COLLECTOR_SECURITY_ALERTS_METRICS_PORT:-19479}:9464")
	assertComposeDependency(t, service, "projector")
	assertComposeDependency(t, service, "workflow-coordinator")

	compose := readRemoteE2EComposeSource(t)
	for _, want := range []string{
		`"instance_id": "remote-e2e-security-alert"`,
		`"collector_kind": "security_alert"`,
		`"provider": "github_dependabot"`,
		`"token_env": "GITHUB_TOKEN"`,
		`"allowed_repositories": ["${ESHU_SECURITY_ALERT_REPOSITORY:?set ESHU_SECURITY_ALERT_REPOSITORY}"]`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("docker-compose.remote-e2e.yaml missing security-alert collector term %q", want)
		}
	}

	exampleEnv := readRepositoryFile(t, deployCheckoutRoot(t), ".env.remote-e2e.example")
	for _, want := range []string{
		"ESHU_SECURITY_ALERT_REPOSITORY=owner/repository",
		"ESHU_SECURITY_ALERT_GITHUB_TOKEN=replace-with-read-only-token",
	} {
		if !strings.Contains(exampleEnv, want) {
			t.Fatalf(".env.remote-e2e.example missing %q", want)
		}
	}
}

func TestRemoteE2EComposeIncludesVulnerabilityIntelligenceCollector(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	service := requireComposeService(t, doc, "collector-vulnerability-intelligence")
	if fmt.Sprint(service.Command) != "[/usr/local/bin/eshu-collector-component-extension]" {
		t.Fatalf("collector command = %#v, want eshu-collector-component-extension", service.Command)
	}
	assertComposeEnv(t, service, "ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_INSTANCE_ID", "remote-e2e-vulnerability-intelligence")
	assertComposeEnv(t, service, "ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_OWNER_ID", "remote-e2e-vulnerability-worker")
	assertComposeEnv(t, service, "ESHU_VULNERABILITY_INTELLIGENCE_POLL_INTERVAL", "${ESHU_VULNERABILITY_INTELLIGENCE_POLL_INTERVAL:-2s}")
	assertComposeEnv(t, service, "NVD_API_KEY", "${ESHU_NVD_API_KEY:-}")
	assertComposePortContains(t, service, "${ESHU_COLLECTOR_VULNERABILITY_INTELLIGENCE_METRICS_PORT:-19476}:9464")
	assertComposeDependency(t, service, "projector")
	assertComposeDependency(t, service, "workflow-coordinator")

	compose := readRemoteE2EComposeSource(t)
	for _, want := range []string{
		`"instance_id": "remote-e2e-vulnerability-intelligence"`,
		`"collector_kind": "vulnerability_intelligence"`,
		`"source": "first_epss"`,
		`"source": "cisa_kev"`,
		`"source": "osv"`,
		`"source": "nvd"`,
		`"api_key_env": "NVD_API_KEY"`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("docker-compose.remote-e2e.yaml missing vulnerability collector term %q", want)
		}
	}

	exampleEnv := readRepositoryFile(t, deployCheckoutRoot(t), ".env.remote-e2e.example")
	for _, want := range []string{
		"ESHU_VULNERABILITY_E2E_CVE_ID=CVE-2021-44228",
		"ESHU_NVD_API_KEY=",
	} {
		if !strings.Contains(exampleEnv, want) {
			t.Fatalf(".env.remote-e2e.example missing %q", want)
		}
	}
}

func TestRemoteE2EComposeIncludesScannerWorker(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	service := requireComposeService(t, doc, "scanner-worker")
	if fmt.Sprint(service.Command) != "[/usr/local/bin/eshu-scanner-worker]" {
		t.Fatalf("scanner-worker command = %#v, want eshu-scanner-worker", service.Command)
	}
	assertComposeEnv(t, service, "ESHU_SCANNER_WORKER_INSTANCE_ID", "remote-e2e-scanner-worker-source")
	assertComposeEnv(t, service, "ESHU_SCANNER_WORKER_ANALYZER", "${ESHU_SCANNER_WORKER_ANALYZER:-sbom_generation}")
	assertComposeEnv(t, service, "ESHU_SCANNER_WORKER_POLL_INTERVAL", "${ESHU_SCANNER_WORKER_POLL_INTERVAL:-2s}")
	assertComposeEnv(t, service, "ESHU_SCANNER_WORKER_MEMORY_BYTES", "${ESHU_SCANNER_WORKER_MEMORY_BYTES:-4294967296}")
	assertComposePortContains(t, service, "${ESHU_SCANNER_WORKER_METRICS_PORT:-19477}:9464")
	assertComposeVolumeContains(
		t,
		service,
		"${ESHU_SCANNER_WORKER_SBOM_HOST_ROOT:-./tests/fixtures/ecosystems}:/scanner-fixtures:ro",
	)
	assertComposeDependency(t, service, "projector")
	assertComposeDependency(t, service, "workflow-coordinator")

	compose := readRemoteE2EComposeSource(t)
	for _, want := range []string{
		`"instance_id": "remote-e2e-scanner-worker-source"`,
		`"collector_kind": "scanner_worker"`,
		`"analyzer": "sbom_generation"`,
		`"scope_id": "scanner-worker://repository/remote-e2e-sbom-fixture"`,
		`"root_path": "/scanner-fixtures"`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("docker-compose.remote-e2e.yaml missing scanner-worker term %q", want)
		}
	}

	exampleEnv := readRepositoryFile(t, deployCheckoutRoot(t), ".env.remote-e2e.example")
	for _, want := range []string{
		"ESHU_SCANNER_WORKER_SBOM_HOST_ROOT=./tests/fixtures/ecosystems",
		"ESHU_SCANNER_WORKER_SBOM_SUBJECT_DIGEST=sha256:",
	} {
		if !strings.Contains(exampleEnv, want) {
			t.Fatalf(".env.remote-e2e.example missing %q", want)
		}
	}
}

func TestRemoteE2EPreflightScriptValidatesFullCorpusInputs(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "scripts/remote-e2e-corpus-preflight.sh")
	for _, want := range []string{
		"normalize_host_root",
		"git_repository_roots",
		"must be a non-negative integer",
		"*/tests/fixtures/ecosystems",
		"full-corpus mode requires at least one Git repository root",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("remote E2E preflight script missing %q", want)
		}
	}
}
