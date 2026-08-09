// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRemoteValidationArtifactBody(t *testing.T, repoRoot, ref, body string) {
	t.Helper()
	writeRemoteValidationTestSnapshot(t, repoRoot, "example_query")
	dir := filepath.Join(repoRoot, RemoteValidationArtifactDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ref+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write remote-validation artifact: %v", err)
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
		{name: "placeholder", body: "# evidence\n"},
		{
			name: "go test only",
			body: `# production validation

Validation-Slug: prod-example
Validation-Tier: local_test
Validation-Date: 2026-08-08
Evidence-Kind: go_test
Evidence-Source: go/internal/query/example_test.go
Validation-Command: cd go && go test ./internal/query; echo $?
Validation-Exit-Code: 0
Capability-Assertion: cap.example returns the expected result.
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repoRoot := t.TempDir()
			writeRemoteValidationArtifactBody(t, repoRoot, "prod-example", test.body)
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
	source := filepath.Join(repoRoot, "scripts", "run-remote-e2e-example.sh")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(source, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}
	writeRemoteValidationArtifactBody(t, repoRoot, "prod-example", `# prod-example production validation

Validation-Slug: prod-example
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/run-remote-e2e-example.sh
Validation-Command: bash scripts/run-remote-e2e-example.sh; echo $?
Validation-Exit-Code: 0
Capability-Assertion: cap.example returns a non-empty exact result through the deployed API.
B12-Assertion: cap.example -> mcp:example_query
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
		b12Line    string
		wantDetail string
	}{
		{name: "missing", wantDetail: "missing B12-Assertion"},
		{name: "unknown query shape", b12Line: "B12-Assertion: cap.example -> mcp:not_a_committed_shape\n", wantDetail: "not found in the committed B-12 snapshot"},
		{name: "capability prefix is not exact", b12Line: "B12-Assertion: cap.example.extra -> mcp:example_query\n", wantDetail: "unexpected capability"},
		{name: "duplicate capability", b12Line: "B12-Assertion: cap.example -> mcp:example_query\nB12-Assertion: cap.example -> mcp:example_query\n", wantDetail: "duplicate B12-Assertion"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repoRoot := t.TempDir()
			writeRemoteValidationTestSource(t, repoRoot, "scripts/run-remote-e2e-example.sh")
			writeRemoteValidationTestSnapshot(t, repoRoot, "example_query")
			writeRemoteValidationArtifactBody(t, repoRoot, "prod-example", `# production validation

Validation-Slug: prod-example
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/run-remote-e2e-example.sh
Validation-Command: bash scripts/run-remote-e2e-example.sh; echo $?
Validation-Exit-Code: 0
Capability-Assertion: cap.example returns a non-empty exact result.
`+test.b12Line)
			matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "cap.example", ref: "prod-example"})

			findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil)
			if len(findings) != 1 || !strings.Contains(findings[0].Reason, test.wantDetail) {
				t.Fatalf("findings = %+v, want one containing %q", findings, test.wantDetail)
			}
		})
	}
}

func TestCheckRemoteValidationArtifactsRequiresEverySharedCapabilityAssertion(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	source := filepath.Join(repoRoot, "scripts", "run-remote-e2e-shared.sh")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(source, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}
	writeRemoteValidationArtifactBody(t, repoRoot, "prod-shared", `# shared production validation

Validation-Slug: prod-shared
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/run-remote-e2e-shared.sh
Validation-Command: bash scripts/run-remote-e2e-shared.sh; echo $?
Validation-Exit-Code: 0
Capability-Assertion: cap.one returns a deployed result.
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
	source := filepath.Join(repoRoot, "scripts", "run-remote-e2e-example.sh")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(source, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}
	writeRemoteValidationArtifactBody(t, repoRoot, "prod-example", `# production validation

Validation-Slug: prod-example
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/run-remote-e2e-example.sh
Validation-Command: true; echo $?
Validation-Exit-Code: 0
Capability-Assertion: cap.example returns a non-empty deployed result.
B12-Assertion: cap.example -> mcp:example_query
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
			source := filepath.Join(repoRoot, "scripts", "run-remote-e2e-example.sh")
			if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
				t.Fatalf("mkdir source dir: %v", err)
			}
			if err := os.WriteFile(source, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
				t.Fatalf("write source: %v", err)
			}
			writeRemoteValidationArtifactBody(t, repoRoot, "prod-example", `# production validation

Validation-Slug: prod-example
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/run-remote-e2e-example.sh
Validation-Command: `+test.command+`
Validation-Exit-Code: 0
Capability-Assertion: cap.example returns a non-empty deployed result.
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
	source := filepath.Join(repoRoot, "scripts", "verify-golden-corpus-gate.sh")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(source, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}
	writeRemoteValidationArtifactBody(t, repoRoot, "prod-example", `# production validation

Validation-Slug: prod-example
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: ESHU_QUERY_PROFILE=local_authoritative GATE_COMPOSE_PROJECT=proof bash scripts/verify-golden-corpus-gate.sh --verbose >/tmp/proof.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: cap.example returns a non-empty deployed result.
B12-Assertion: cap.example -> mcp:example_query
`)
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "cap.example", ref: "prod-example"})

	if findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil); len(findings) != 0 {
		t.Fatalf("findings = %+v, want env-prefixed redirected driver command accepted", findings)
	}
}

func TestCheckRemoteValidationArtifactsRequiresDirectExitCaptureForLiveEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	source := filepath.Join(repoRoot, "docs", "internal", "evidence", "live-proof.md")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(source, []byte("# live proof\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	writeRemoteValidationArtifactBody(t, repoRoot, "prod-example", `# production validation

Validation-Slug: prod-example
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: live_backend
Evidence-Source: docs/internal/evidence/live-proof.md
Validation-Command: curl --fail http://127.0.0.1:8080/proof
Validation-Exit-Code: 0
Capability-Assertion: cap.example returns a non-empty deployed result.
`)
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "cap.example", ref: "prod-example"})

	if findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil); len(findings) != 1 {
		t.Fatalf("findings = %+v, want missing direct exit capture rejected", findings)
	}
}

func TestCheckRemoteValidationArtifactsAcceptsDeployedKubernetesDriver(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	source := filepath.Join(repoRoot, "scripts", "run-k8s-governance-proof.sh")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(source, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}
	writeRemoteValidationArtifactBody(t, repoRoot, "prod-governance", `# deployed validation

Validation-Slug: prod-governance
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: deployed_e2e
Evidence-Source: scripts/run-k8s-governance-proof.sh
Validation-Command: bash scripts/run-k8s-governance-proof.sh; echo $?
Validation-Exit-Code: 0
Capability-Assertion: governance.status returns isolated deployed results.
B12-Assertion: governance.status -> mcp:example_query
`)
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "governance.status", ref: "prod-governance"})

	if findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil); len(findings) != 0 {
		t.Fatalf("findings = %+v, want deployed Kubernetes evidence to pass", findings)
	}
}
