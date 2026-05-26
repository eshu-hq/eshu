package runtime

import (
	"strings"
	"testing"
)

func TestRemoteE2EComposeWiresRepresentativeCorpusBounds(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	preflight := requireComposeService(t, doc, "remote-e2e-corpus-preflight")

	assertComposeEnv(t, preflight, "ESHU_REMOTE_E2E_MAX_REPOSITORY_COUNT", "${ESHU_REMOTE_E2E_MAX_REPOSITORY_COUNT:-}")

	exampleEnv := readRepositoryFile(t, "../../..", ".env.remote-e2e.example")
	for _, want := range []string{
		"ESHU_REMOTE_E2E_CORPUS_MODE=smoke",
		"ESHU_REMOTE_E2E_MAX_REPOSITORY_COUNT=",
		"ESHU_REMOTE_E2E_MIN_PACKAGE_COUNT=",
		"ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT=",
		"ESHU_REMOTE_E2E_MIN_SECURITY_ALERT_RECONCILIATION_COUNT=",
	} {
		if !strings.Contains(exampleEnv, want) {
			t.Fatalf(".env.remote-e2e.example missing %q", want)
		}
	}
}

func TestRemoteE2EPreflightScriptDefinesRepresentativeMode(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "scripts/remote-e2e-corpus-preflight.sh")
	for _, want := range []string{
		"smoke | representative | full",
		"effective_min_count=20",
		"effective_max_count=50",
		"representative-corpus mode requires at least one Git repository root",
		"representative-corpus mode allows at most",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("remote E2E preflight script missing %q", want)
		}
	}
}
