// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRemoteValidationArtifactBody(t *testing.T, repoRoot, ref, body string) {
	t.Helper()
	dir := filepath.Join(repoRoot, RemoteValidationArtifactDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ref+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write remote-validation artifact: %v", err)
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
`)
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{
		capability: "cap.example",
		ref:        "prod-example",
	})

	if findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil); len(findings) != 0 {
		t.Fatalf("findings = %+v, want none for valid deployed evidence", findings)
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
`)
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "cap.example", ref: "prod-example"})

	if findings := CheckRemoteValidationArtifacts(matrix, repoRoot, nil); len(findings) != 1 {
		t.Fatalf("findings = %+v, want command/source mismatch", findings)
	}
}
