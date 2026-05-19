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

func TestRemoteE2EExampleEnvRequestsFullCorpusPreflight(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", ".env.remote-e2e.example")
	for _, want := range []string{
		"ESHU_REMOTE_E2E_CORPUS_MODE=full",
		"ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT=100",
		"ESHU_FILESYSTEM_HOST_ROOT=/absolute/path/to/full-corpus",
	} {
		if !strings.Contains(content, want) {
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
