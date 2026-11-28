// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRemoteValidationProofFile(t *testing.T, repoRoot, body string) {
	t.Helper()
	writeRemoteValidationTestSnapshot(t, repoRoot, "example_query")
	path := filepath.Join(repoRoot, filepath.FromSlash(RemoteValidationProofsPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write remote-validation proofs: %v", err)
	}
}

func writeRemoteValidationTestSource(t *testing.T, repoRoot, source string) {
	t.Helper()
	path := filepath.Join(repoRoot, filepath.FromSlash(source))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}
}

func writeRemoteValidationTestSnapshot(t *testing.T, repoRoot, mcpKey string) {
	t.Helper()
	path := filepath.Join(repoRoot, "testdata", "golden", "e2e-20repo-snapshot.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	body := `{"query_shapes":{"mcp":{"` + mcpKey + `":{}},"http":{},"cli":{}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
}

func TestCheckRemoteValidationArtifactsRejectsPlaceholderAndLowerTierEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "placeholder",
			body: "schema_version: 1\nproofs:\n  - slug: prod-example\n",
		},
		{
			name: "missing exit code",
			body: `schema_version: 1
proofs:
  - slug: prod-example
    tier: deployed_services
    date: "2026-08-08"
    kind: compose_e2e
    source: scripts/run-remote-e2e-example.sh
    command: "bash scripts/run-remote-e2e-example.sh; echo $?"
    assertions:
      - "cap.example returns the expected result."
    b12_assertions:
      - "cap.example -> mcp:example_query"
`,
		},
		{
			name: "go test only",
			body: `schema_version: 1
proofs:
  - slug: prod-example
    tier: local_test
    date: "2026-08-08"
    kind: go_test
    source: go/internal/query/example_test.go
    command: "cd go && go test ./internal/query; echo $?"
    exit_code: 0
    assertions:
      - "cap.example returns the expected result."
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repoRoot := t.TempDir()
			writeRemoteValidationTestSource(t, repoRoot, "scripts/run-remote-e2e-example.sh")
			writeRemoteValidationProofFile(t, repoRoot, test.body)
			matrix := matrixWithRemoteValidationRefs(matrixRefSpec{
				capability: "cap.example",
				ref:        "prod-example",
			})

			findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil)
			if len(findings) != 1 {
				t.Fatalf("findings = %+v, want one invalid-artifact finding", findings)
			}
		})
	}
}

func TestCheckRemoteValidationArtifactsAcceptsDeployedEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRemoteValidationTestSource(t, repoRoot, "scripts/run-remote-e2e-example.sh")
	writeRemoteValidationProofFile(t, repoRoot, `schema_version: 1
proofs:
  - slug: prod-example
    tier: deployed_services
    date: "2026-08-08"
    kind: compose_e2e
    source: scripts/run-remote-e2e-example.sh
    command: "bash scripts/run-remote-e2e-example.sh; echo $?"
    exit_code: 0
    assertions:
      - "cap.example returns a non-empty exact result through the deployed API."
    b12_assertions:
      - "cap.example -> mcp:example_query"
`)
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{
		capability: "cap.example",
		ref:        "prod-example",
	})

	if findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil); len(findings) != 0 {
		t.Fatalf("findings = %+v, want none for valid deployed evidence", findings)
	}
}

func TestCheckRemoteValidationArtifactsRequiresResolvableB12Assertion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		b12Block   string
		wantDetail string
	}{
		{name: "missing", wantDetail: "missing B12-Assertion"},
		{name: "unknown query shape", b12Block: "    b12_assertions:\n      - \"cap.example -> mcp:not_a_committed_shape\"\n", wantDetail: "not found in the committed B-12 snapshot"},
		{name: "capability prefix is not exact", b12Block: "    b12_assertions:\n      - \"cap.example.extra -> mcp:example_query\"\n", wantDetail: "unexpected capability"},
		{name: "duplicate capability", b12Block: "    b12_assertions:\n      - \"cap.example -> mcp:example_query\"\n      - \"cap.example -> mcp:example_query\"\n", wantDetail: "duplicate B12-Assertion"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repoRoot := t.TempDir()
			writeRemoteValidationTestSource(t, repoRoot, "scripts/run-remote-e2e-example.sh")
			writeRemoteValidationProofFile(t, repoRoot, `schema_version: 1
proofs:
  - slug: prod-example
    tier: deployed_services
    date: "2026-08-08"
    kind: compose_e2e
    source: scripts/run-remote-e2e-example.sh
    command: "bash scripts/run-remote-e2e-example.sh; echo $?"
    exit_code: 0
    assertions:
      - "cap.example returns a non-empty exact result."
`+test.b12Block)
			matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "cap.example", ref: "prod-example"})

			findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil)
			if len(findings) != 1 || !strings.Contains(findings[0].Reason, test.wantDetail) {
				t.Fatalf("findings = %+v, want one containing %q", findings, test.wantDetail)
			}
		})
	}
}

func TestCheckRemoteValidationArtifactsRejectsRefusalOnlyB12Proof(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRemoteValidationTestSource(t, repoRoot, "scripts/verify-golden-corpus-gate.sh")
	writeRemoteValidationProofFile(t, repoRoot, `schema_version: 1
proofs:
  - slug: prod-example
    tier: deployed_services
    date: "2026-08-08"
    kind: compose_e2e
    source: scripts/verify-golden-corpus-gate.sh
    command: "bash scripts/verify-golden-corpus-gate.sh; echo $?"
    exit_code: 0
    assertions:
      - "cap.example returns a non-empty deployed result."
    b12_assertions:
      - "cap.example -> mcp:example_query"
`)
	snapshotPath := filepath.Join(repoRoot, "testdata", "golden", "e2e-20repo-snapshot.json")
	refusalOnly := `{"query_shapes":{"mcp":{"example_query":{"required_response_fields":[],"minimum_results":0,"expected_error_contains":"feature is not enabled"}},"http":{},"cli":{}}}`
	if err := os.WriteFile(snapshotPath, []byte(refusalOnly), 0o644); err != nil {
		t.Fatalf("write refusal-only snapshot: %v", err)
	}
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "cap.example", ref: "prod-example"})

	findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil)
	if len(findings) != 1 || !strings.Contains(findings[0].Reason, "refusal-only") {
		t.Fatalf("findings = %+v, want refusal-only B-12 proof rejected", findings)
	}
}

func TestCheckRemoteValidationArtifactsAllowsDedicatedDeployedProofWithDefaultStackRefusal(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRemoteValidationTestSource(t, repoRoot, "scripts/run-remote-e2e-example.sh")
	writeRemoteValidationProofFile(t, repoRoot, `schema_version: 1
proofs:
  - slug: prod-example
    tier: deployed_services
    date: "2026-08-08"
    kind: compose_e2e
    source: scripts/run-remote-e2e-example.sh
    command: "bash scripts/run-remote-e2e-example.sh; echo $?"
    exit_code: 0
    assertions:
      - "cap.example returned a non-empty response from its dedicated deployed stack."
    b12_assertions:
      - "cap.example -> mcp:example_query"
`)
	snapshotPath := filepath.Join(repoRoot, "testdata", "golden", "e2e-20repo-snapshot.json")
	defaultStackRefusal := `{"query_shapes":{"mcp":{"example_query":{"required_response_fields":[],"minimum_results":0,"expected_error_contains":"dedicated service is not configured"}},"http":{},"cli":{}}}`
	if err := os.WriteFile(snapshotPath, []byte(defaultStackRefusal), 0o644); err != nil {
		t.Fatalf("write default-stack snapshot: %v", err)
	}
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "cap.example", ref: "prod-example"})

	if findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil); len(findings) != 0 {
		t.Fatalf("findings = %+v, want dedicated deployed proof accepted", findings)
	}
}

func TestCheckRemoteValidationArtifactsRequiresEverySharedCapabilityAssertion(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRemoteValidationTestSource(t, repoRoot, "scripts/run-remote-e2e-shared.sh")
	writeRemoteValidationProofFile(t, repoRoot, `schema_version: 1
proofs:
  - slug: prod-shared
    tier: deployed_services
    date: "2026-08-08"
    kind: compose_e2e
    source: scripts/run-remote-e2e-shared.sh
    command: "bash scripts/run-remote-e2e-shared.sh; echo $?"
    exit_code: 0
    assertions:
      - "cap.one returns a deployed result."
`)
	matrix := matrixWithRemoteValidationRefs(
		matrixRefSpec{capability: "cap.one", ref: "prod-shared"},
		matrixRefSpec{capability: "cap.two", ref: "prod-shared"},
	)

	findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil)
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want missing cap.two assertion", findings)
	}
}

func TestCheckRemoteValidationArtifactsRejectsCommandThatDoesNotRunComposeSource(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRemoteValidationTestSource(t, repoRoot, "scripts/run-remote-e2e-example.sh")
	writeRemoteValidationProofFile(t, repoRoot, `schema_version: 1
proofs:
  - slug: prod-example
    tier: deployed_services
    date: "2026-08-08"
    kind: compose_e2e
    source: scripts/run-remote-e2e-example.sh
    command: "true; echo $?"
    exit_code: 0
    assertions:
      - "cap.example returns a non-empty deployed result."
    b12_assertions:
      - "cap.example -> mcp:example_query"
`)
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "cap.example", ref: "prod-example"})

	if findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil); len(findings) != 1 {
		t.Fatalf("findings = %+v, want command/source mismatch", findings)
	}
}

func TestCheckRemoteValidationArtifactsRejectsSpoofedOrMaskedDriverExit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
	}{
		{name: "echo only", command: "echo scripts/run-remote-e2e-example.sh; echo $?"},
		{name: "semicolon masking", command: "bash scripts/run-remote-e2e-example.sh; true; echo $?"},
		{name: "and masking", command: "bash scripts/run-remote-e2e-example.sh && true; echo $?"},
		{name: "or masking", command: "bash scripts/run-remote-e2e-example.sh || true; echo $?"},
		{name: "pipeline masking", command: "bash scripts/run-remote-e2e-example.sh | tee /tmp/proof.log; echo $?"},
		{name: "comment masks capture", command: "bash scripts/run-remote-e2e-example.sh # proof; echo $?"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repoRoot := t.TempDir()
			writeRemoteValidationTestSource(t, repoRoot, "scripts/run-remote-e2e-example.sh")
			writeRemoteValidationProofFile(t, repoRoot, `schema_version: 1
proofs:
  - slug: prod-example
    tier: deployed_services
    date: "2026-08-08"
    kind: compose_e2e
    source: scripts/run-remote-e2e-example.sh
    command: "`+test.command+`"
    exit_code: 0
    assertions:
      - "cap.example returns a non-empty deployed result."
`)
			matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "cap.example", ref: "prod-example"})

			if findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil); len(findings) != 1 {
				t.Fatalf("findings = %+v, want spoofed or masked driver command rejected", findings)
			}
		})
	}
}

func TestCheckRemoteValidationArtifactsAcceptsDirectDriverExitCapture(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRemoteValidationTestSource(t, repoRoot, "scripts/verify-golden-corpus-gate.sh")
	writeRemoteValidationProofFile(t, repoRoot, `schema_version: 1
proofs:
  - slug: prod-example
    tier: deployed_services
    date: "2026-08-08"
    kind: compose_e2e
    source: scripts/verify-golden-corpus-gate.sh
    command: "ESHU_QUERY_PROFILE=local_authoritative GATE_COMPOSE_PROJECT=proof bash scripts/verify-golden-corpus-gate.sh --verbose >/tmp/proof.log 2>&1; echo $?"
    exit_code: 0
    assertions:
      - "cap.example returns a non-empty deployed result."
    b12_assertions:
      - "cap.example -> mcp:example_query"
`)
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "cap.example", ref: "prod-example"})

	if findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil); len(findings) != 0 {
		t.Fatalf("findings = %+v, want env-prefixed redirected driver command accepted", findings)
	}
}

func TestCheckRemoteValidationArtifactsRejectsRetiredLiveBackendKind(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRemoteValidationProofFile(t, repoRoot, `schema_version: 1
proofs:
  - slug: prod-example
    tier: deployed_services
    date: "2026-08-08"
    kind: live_backend
    source: docs/internal/evidence/live-proof.md
    command: "curl --fail http://127.0.0.1:8080/proof; echo $?"
    exit_code: 0
    assertions:
      - "cap.example returns a non-empty deployed result."
    b12_assertions:
      - "cap.example -> mcp:example_query"
`)
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "cap.example", ref: "prod-example"})

	findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil)
	if len(findings) != 1 || !strings.Contains(findings[0].Reason, "compose_e2e or deployed_e2e") {
		t.Fatalf("findings = %+v, want retired live_backend kind rejected", findings)
	}
}

func TestCheckRemoteValidationArtifactsAcceptsDeployedKubernetesDriver(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRemoteValidationTestSource(t, repoRoot, "scripts/run-k8s-governance-proof.sh")
	writeRemoteValidationProofFile(t, repoRoot, `schema_version: 1
proofs:
  - slug: prod-governance
    tier: deployed_services
    date: "2026-08-08"
    kind: deployed_e2e
    source: scripts/run-k8s-governance-proof.sh
    command: "bash scripts/run-k8s-governance-proof.sh; echo $?"
    exit_code: 0
    assertions:
      - "governance.status returns isolated deployed results."
    b12_assertions:
      - "governance.status -> mcp:example_query"
`)
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "governance.status", ref: "prod-governance"})

	if findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil); len(findings) != 0 {
		t.Fatalf("findings = %+v, want deployed Kubernetes evidence to pass", findings)
	}
}

func TestCheckRemoteValidationArtifactsFailsClosedOnMalformedCatalog(t *testing.T) {
	t.Parallel()

	validEntry := `  - slug: prod-example
    tier: deployed_services
    date: "2026-08-08"
    kind: compose_e2e
    source: scripts/run-remote-e2e-example.sh
    command: "bash scripts/run-remote-e2e-example.sh; echo $?"
    exit_code: 0
    assertions:
      - "cap.example returns a deployed result."
    b12_assertions:
      - "cap.example -> mcp:example_query"
`
	tests := []struct {
		name       string
		body       string
		baseline   map[string]struct{}
		wantDetail string
	}{
		{
			name:       "unknown YAML key",
			body:       "schema_version: 1\nproofs:\n" + validEntry + "    unexpected_field: true\n",
			wantDetail: "unexpected_field",
		},
		{
			name:       "duplicate slug",
			body:       "schema_version: 1\nproofs:\n" + validEntry + validEntry,
			wantDetail: "duplicate proof entry",
		},
		{
			name:       "wrong schema_version",
			body:       "schema_version: 2\nproofs:\n" + validEntry,
			wantDetail: "schema_version is 2, want 1",
		},
		{
			name:       "missing slug",
			body:       "schema_version: 1\nproofs:\n  - tier: deployed_services\n",
			wantDetail: "missing slug",
		},
		{
			name:       "invalid slug pattern",
			body:       "schema_version: 1\nproofs:\n" + validEntry + "  - slug: Bad_Slug\n",
			wantDetail: "invalid slug",
		},
		{
			name:       "traversal slug pattern",
			body:       "schema_version: 1\nproofs:\n" + validEntry + "  - slug: ../evil\n",
			wantDetail: "invalid slug",
		},
		{
			name:       "trailing second YAML document",
			body:       "schema_version: 1\nproofs:\n" + validEntry + "---\nschema_version: 1\nproofs: []\n",
			wantDetail: "multiple YAML documents",
		},
		{
			name:       "non-zero exit_code",
			body:       strings.Replace(validEntry, "exit_code: 0", "exit_code: 1", 1),
			wantDetail: "exit_code is 1, want 0",
		},
		{
			name:       "corrupt catalog not excused by baseline",
			body:       "schema_version: 99\nproofs: []\n",
			baseline:   map[string]struct{}{"prod-example": {}},
			wantDetail: "schema_version is 99, want 1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repoRoot := t.TempDir()
			writeRemoteValidationTestSource(t, repoRoot, "scripts/run-remote-e2e-example.sh")
			body := test.body
			if !strings.HasPrefix(body, "schema_version:") {
				body = "schema_version: 1\nproofs:\n" + body
			}
			writeRemoteValidationProofFile(t, repoRoot, body)
			matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "cap.example", ref: "prod-example"})

			findings := CheckRemoteValidationArtifacts(matrix, repoRoot, test.baseline)
			if len(findings) != 1 || !strings.Contains(findings[0].Reason, test.wantDetail) {
				t.Fatalf("findings = %+v, want one containing %q", findings, test.wantDetail)
			}
		})
	}
}
