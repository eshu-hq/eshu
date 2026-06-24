// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
		"ESHU_REMOTE_E2E_ADVISORY_EVIDENCE_CVE_ID=",
		"ESHU_REMOTE_E2E_DERIVED_TARGET_LIMIT=100",
		"ESHU_REMOTE_E2E_MIN_PACKAGE_COUNT=",
		"ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT=",
		"ESHU_REMOTE_E2E_MIN_SECURITY_ALERT_RECONCILIATION_COUNT=",
	} {
		if !strings.Contains(exampleEnv, want) {
			t.Fatalf(".env.remote-e2e.example missing %q", want)
		}
	}
}

func TestRemoteE2EComposeUsesBoundedDerivedTargetBudget(t *testing.T) {
	t.Parallel()

	compose := readRepositoryFile(t, "../../..", "docker-compose.remote-e2e.runtime.yaml")
	if got := strings.Count(compose, `\"target_limit\": ${ESHU_REMOTE_E2E_DERIVED_TARGET_LIMIT:-100}`); got != 2 {
		t.Fatalf("derived target budget interpolation count = %d, want 2", got)
	}
	if strings.Contains(compose, `\"target_limit\": 5000`) {
		t.Fatal("remote E2E Compose must not hard-code full-corpus derived target fanout")
	}
}

func TestRemoteE2EDocsDocumentAdvisoryEvidenceSelector(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"docs/public/reference/environment-compose-tests.md",
		"docs/public/reference/local-testing/remote-representative-acceptance.md",
	} {
		content := readRepositoryFile(t, "../../..", path)
		if !strings.Contains(content, "ESHU_REMOTE_E2E_ADVISORY_EVIDENCE_CVE_ID") {
			t.Fatalf("%s missing ESHU_REMOTE_E2E_ADVISORY_EVIDENCE_CVE_ID", path)
		}
		if !strings.Contains(content, "ESHU_REMOTE_E2E_DERIVED_TARGET_LIMIT") {
			t.Fatalf("%s missing ESHU_REMOTE_E2E_DERIVED_TARGET_LIMIT", path)
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
