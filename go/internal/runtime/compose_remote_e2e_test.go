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

	content := readRepositoryFile(t, "../../..", ".env.remote-e2e.example")
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

	content := readRepositoryFile(t, "../../..", "docker-compose.remote-e2e.yaml")
	for _, want := range []string{
		`"backend_filters"`,
		`"target_scope_id": "aws-e2e"`,
		`"backend_kind": "s3"`,
		`"bucket": "${ESHU_TFSTATE_S3_BUCKET:?set ESHU_TFSTATE_S3_BUCKET}"`,
		`"region": "${ESHU_TFSTATE_S3_REGION:?set ESHU_TFSTATE_S3_REGION}"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("docker-compose.remote-e2e.yaml missing Terraform-state backend filter term %q", want)
		}
	}
}

func TestRemoteE2EComposeUsesStableWorkflowReconcileWindow(t *testing.T) {
	t.Parallel()

	compose := readRepositoryFile(t, "../../..", "docker-compose.remote-e2e.yaml")
	if !strings.Contains(compose, "ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL:-1h") {
		t.Fatal("remote E2E Compose should default workflow reconcile to 1h so verification can finish before the next continuous wave")
	}

	exampleEnv := readRepositoryFile(t, "../../..", ".env.remote-e2e.example")
	if !strings.Contains(exampleEnv, "ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL=1h") {
		t.Fatal(".env.remote-e2e.example should document the stable reconcile window")
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
		"resolution-engine",
		"workflow-coordinator",
		"collector-terraform-state",
		"collector-oci-registry",
		"collector-package-registry",
		"collector-aws-cloud",
	} {
		service := requireComposeService(t, doc, serviceName)
		assertComposeEnv(t, service, "ESHU_CANONICAL_WRITE_TIMEOUT", "${ESHU_CANONICAL_WRITE_TIMEOUT:-120s}")
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
		"resolution-engine",
		"workflow-coordinator",
		"webhook-listener",
		"collector-terraform-state",
		"collector-oci-registry",
		"collector-package-registry",
		"collector-aws-cloud",
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
		"bootstrap-index":            "19660",
		"ingester":                   "19661",
		"resolution-engine":          "19662",
		"workflow-coordinator":       "19663",
		"collector-terraform-state":  "19664",
		"collector-oci-registry":     "19665",
		"collector-package-registry": "19666",
		"collector-aws-cloud":        "19667",
		"collector-confluence":       "19668",
	} {
		service := requireComposeService(t, doc, serviceName)
		assertComposeEnv(t, service, "ESHU_PPROF_ADDR", "0.0.0.0:6060")
		assertComposePortContains(t, service, "127.0.0.1:"+hostPort+":6060")
	}
}

func TestHostedWorkerCommandsStartPprofServer(t *testing.T) {
	t.Parallel()

	for _, sourcePath := range []string{
		"go/cmd/collector-aws-cloud/main.go",
		"go/cmd/collector-confluence/main.go",
		"go/cmd/collector-git/main.go",
		"go/cmd/collector-oci-registry/main.go",
		"go/cmd/collector-package-registry/main.go",
		"go/cmd/collector-terraform-state/main.go",
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

func TestNornicDBGraphOnlySearchStartupDocsTrackSupportedKnobs(t *testing.T) {
	t.Parallel()

	docs := readRepositoryFile(t, "../../..", "docs/docs/run-locally/docker-compose.md")
	for _, want := range []string{
		"NORNICDB_EMBEDDING_ENABLED=false",
		"NORNICDB_PERSIST_SEARCH_INDEXES=true",
		"NornicDB does not currently document a supported switch that disables search/BM25 services entirely",
		"orneryd/NornicDB#",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("docker compose docs missing NornicDB search startup note %q", want)
		}
	}

	compose := readRepositoryFile(t, "../../..", "docker-compose.yaml")
	if strings.Contains(compose, "NORNICDB_SEARCH_ENABLED") {
		t.Fatal("docker-compose.yaml must not advertise unsupported NORNICDB_SEARCH_ENABLED")
	}
}

func assertComposeVolumeContains(t *testing.T, service composeService, want string) {
	t.Helper()

	for _, volume := range service.Volumes {
		if fmt.Sprint(volume) == want {
			return
		}
	}
	t.Fatalf("compose volume %q missing from %#v", want, service.Volumes)
}

func assertComposeScriptContains(t *testing.T, service composeService, want string) {
	t.Helper()

	body := fmt.Sprintf("%#v %#v", service.Entrypoint, service.Command)
	if !strings.Contains(body, want) {
		t.Fatalf("compose script missing %q in %s", want, body)
	}
}

func remoteE2EWorkerPprofServices() []string {
	return []string{
		"bootstrap-index",
		"ingester",
		"resolution-engine",
		"workflow-coordinator",
		"collector-terraform-state",
		"collector-oci-registry",
		"collector-package-registry",
		"collector-aws-cloud",
		"collector-confluence",
	}
}

func assertComposePortContains(t *testing.T, service composeService, want string) {
	t.Helper()

	for _, port := range service.Ports {
		if fmt.Sprint(port) == want {
			return
		}
	}
	t.Fatalf("compose port %q missing from %#v", want, service.Ports)
}

func assertComposePortMissing(t *testing.T, service composeService, forbidden string) {
	t.Helper()

	for _, port := range service.Ports {
		if strings.Contains(fmt.Sprint(port), forbidden) {
			t.Fatalf("compose port %q unexpectedly present in %#v", forbidden, service.Ports)
		}
	}
}
