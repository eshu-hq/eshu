// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/parser/hcl"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestEvaluateBackendConfigLocalBackendThroughRealParser closes a gap the
// hand-built-row tests in backend_config_local_test.go cannot see: those
// tests construct a `backend` row map directly, matching the SHAPE the real
// HCL parser is documented to emit, but never exercise the real parser
// itself. backendConfigLocalCandidate's correctness depends on
// go/internal/parser/hcl/terraform_backend.go's row["path"] genuinely being
// an absolute path (backendFileDirRelativeToRepo rejects anything else) --
// this test proves that end to end: a real .tf file on a real temp
// directory, parsed by hcl.Parse, fed straight into EvaluateBackendConfig, no
// hand-built row in between (issue #5594 live-gate round-trip: FAIL 1
// investigation ruled out a parser/candidate-derivation defect via this
// reproduction before the fix in scripts/lib/golden-corpus-local-backend.sh
// landed).
func TestEvaluateBackendConfigLocalBackendThroughRealParser(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tfPath := filepath.Join(dir, "main.tf")
	content := `terraform {
  required_version = ">= 1.5.0"
  backend "local" {}
}

resource "aws_instance" "local_backend_demo" {
  ami = "ami-0localbackend000001"
}
`
	if err := os.WriteFile(tfPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture .tf: %v", err)
	}

	payload, err := hcl.Parse(tfPath, false, shared.Options{})
	if err != nil {
		t.Fatalf("hcl.Parse: %v", err)
	}
	backends, _ := payload["terraform_backends"].([]map[string]any)
	if len(backends) != 1 {
		t.Fatalf("terraform_backends rows = %d, want 1: %#v", len(backends), payload)
	}
	if !filepath.IsAbs(backends[0]["path"].(string)) {
		t.Fatalf(`row["path"] = %q, want an absolute path`, backends[0]["path"])
	}
	if _, present := backends[0]["state_path"]; present {
		t.Fatalf(`row["state_path"] present for a bare backend block: %#v`, backends[0])
	}

	repoLocalPath, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}

	result := terraformstate.EvaluateBackendConfig("terraform_local_backend_demo", terraformstate.BackendConfigContext{
		Backends:      backends,
		RepoLocalPath: repoLocalPath,
	})
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want none", result.Warnings)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("Candidates = %d, want 1", len(result.Candidates))
	}

	got := result.Candidates[0]
	wantLocator := filepath.Join(repoLocalPath, "terraform.tfstate")
	if got.State.Locator != wantLocator {
		t.Fatalf("Locator = %q, want %q", got.State.Locator, wantLocator)
	}
	if got.State.BackendKind != terraformstate.BackendLocal {
		t.Fatalf("BackendKind = %q, want %q", got.State.BackendKind, terraformstate.BackendLocal)
	}
	if !got.LocatorDefaulted {
		t.Fatal("LocatorDefaulted = false, want true for a bare `backend \"local\" {}` block")
	}

	// This locator is exactly what golden-corpus-gate's own
	// -print-local-backend-scope-id mode (go/cmd/golden-corpus-gate/local_backend_scope_id.go)
	// must reproduce for the live gate's config-side/state-side join to
	// agree; TestComputeLocalBackendScopeIDMatchesScopeLocatorHash cross-checks
	// that duplicated formula against terraformstate.ScopeLocatorHash
	// directly, so this test does not repeat that check.
}
