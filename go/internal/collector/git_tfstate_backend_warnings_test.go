// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildStreamingGenerationEmitsUnresolvedTerraformBackendExpressionWarnings(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoPath)
	observedAt := time.Date(2026, time.June, 13, 15, 30, 0, 0, time.UTC)
	relativePath := "env/backend.tf"
	snapshot := RepositorySnapshot{
		FileCount: 1,
		FileData: []map[string]any{
			{
				"lang": "hcl",
				"path": filepath.Join(repoPath, filepath.FromSlash(relativePath)),
				"terraform_backends": []map[string]any{
					{
						"name":               "s3",
						"backend_kind":       "s3",
						"path":               relativePath,
						"line_number":        2,
						"bucket":             "var.state_bucket",
						"bucket_is_literal":  false,
						"bucket_line_number": 3,
						"key":                "services/api/terraform.tfstate",
						"key_is_literal":     true,
						"key_line_number":    4,
						"region":             "us-east-1",
						"region_is_literal":  true,
						"region_line_number": 5,
					},
				},
			},
		},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-backend-warning", observedAt, snapshot, false)
	envelopes := drainFactChannel(collected.Facts)
	if got, want := len(envelopes), collected.FactCount; got != want {
		t.Fatalf("streamed facts = %d, FactCount = %d", got, want)
	}
	if got := len(factsByKind(envelopes, facts.TerraformStateCandidateFactKind)); got != 0 {
		t.Fatalf("terraform_state_candidate fact count = %d, want 0", got)
	}
	warnings := factsByKind(envelopes, facts.TerraformStateWarningFactKind)
	if got, want := len(warnings), 1; got != want {
		t.Fatalf("terraform_state_warning fact count = %d, want %d", got, want)
	}

	warning := warnings[0]
	if got, want := warning.CollectorKind, "git"; got != want {
		t.Fatalf("warning CollectorKind = %q, want %q", got, want)
	}
	if got, want := warning.SourceConfidence, facts.SourceConfidenceObserved; got != want {
		t.Fatalf("warning SourceConfidence = %q, want %q", got, want)
	}
	if got, want := warning.SourceRef.SourceSystem, "git"; got != want {
		t.Fatalf("warning SourceRef.SourceSystem = %q, want %q", got, want)
	}
	if got, want := warning.SourceRef.SourceURI, "git://"+repo.ID+"/"+relativePath; got != want {
		t.Fatalf("warning SourceRef.SourceURI = %q, want %q", got, want)
	}
	payload := warning.Payload
	if got, want := payload["warning_kind"], "unresolved_backend_expression"; got != want {
		t.Fatalf("warning_kind = %#v, want %#v", got, want)
	}
	if got, want := payload["reason"], "missing_variable_default"; got != want {
		t.Fatalf("reason = %#v, want %#v", got, want)
	}
	if got, want := payload["source"], "terraform_backend"; got != want {
		t.Fatalf("source = %#v, want %#v", got, want)
	}
	if got, want := payload["severity"], "blocking"; got != want {
		t.Fatalf("severity = %#v, want %#v", got, want)
	}
	if got, want := payload["actionability"], "blocking_evidence"; got != want {
		t.Fatalf("actionability = %#v, want %#v", got, want)
	}
	if got, want := payload["repo_id"], repo.ID; got != want {
		t.Fatalf("repo_id = %#v, want %#v", got, want)
	}
	if got, want := payload["backend_kind"], "s3"; got != want {
		t.Fatalf("backend_kind = %#v, want %#v", got, want)
	}
	if got, want := payload["attribute_name"], "bucket"; got != want {
		t.Fatalf("attribute_name = %#v, want %#v", got, want)
	}
	if got, want := payload["expression_kind"], "var_reference"; got != want {
		t.Fatalf("expression_kind = %#v, want %#v", got, want)
	}
	if got, want := payload["confidence_tier"], "name_only"; got != want {
		t.Fatalf("confidence_tier = %#v, want %#v", got, want)
	}
	if got, want := payload["not_candidate_reason"], "backend attribute did not resolve to an exact locator"; got != want {
		t.Fatalf("not_candidate_reason = %#v, want %#v", got, want)
	}
	if got, want := payload["source_path"], relativePath; got != want {
		t.Fatalf("source_path = %#v, want %#v", got, want)
	}
	if got, want := payload["line_number"], 3; got != want {
		t.Fatalf("line_number = %#v, want %#v", got, want)
	}
	expressionHash, ok := payload["expression_hash"].(string)
	if !ok || expressionHash == "" {
		t.Fatalf("expression_hash = %#v, want non-empty string", payload["expression_hash"])
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal(payload) error = %v, want nil", err)
	}
	if strings.Contains(string(payloadJSON), "var.state_bucket") {
		t.Fatalf("warning payload includes raw backend expression: %s", payloadJSON)
	}
}

func TestBuildStreamingGenerationKeysBackendExpressionWarningsByLine(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoPath)
	relativePath := "backend.tf"
	snapshot := RepositorySnapshot{
		FileCount: 1,
		FileData: []map[string]any{
			{
				"lang": "hcl",
				"path": filepath.Join(repoPath, relativePath),
				"terraform_backends": []map[string]any{
					backendExpressionWarningTestRow(relativePath, 3),
					backendExpressionWarningTestRow(relativePath, 9),
				},
			},
		},
	}

	collected := buildStreamingGeneration(
		repoPath,
		repo,
		"run-backend-warning-lines",
		time.Date(2026, time.June, 13, 16, 0, 0, 0, time.UTC),
		snapshot,
		false,
	)
	warnings := factsByKind(drainFactChannel(collected.Facts), facts.TerraformStateWarningFactKind)
	if got, want := len(warnings), 2; got != want {
		t.Fatalf("terraform_state_warning fact count = %d, want %d", got, want)
	}
	seenKeys := map[string]struct{}{}
	for _, warning := range warnings {
		if _, exists := seenKeys[warning.StableFactKey]; exists {
			t.Fatalf("duplicate StableFactKey for repeated backend warning: %q", warning.StableFactKey)
		}
		seenKeys[warning.StableFactKey] = struct{}{}
	}
}

func backendExpressionWarningTestRow(relativePath string, lineNumber int) map[string]any {
	return map[string]any{
		"name":               "s3",
		"backend_kind":       "s3",
		"path":               relativePath,
		"line_number":        lineNumber - 1,
		"bucket":             "var.state_bucket",
		"bucket_is_literal":  false,
		"bucket_line_number": lineNumber,
		"key":                "services/api/terraform.tfstate",
		"key_is_literal":     true,
		"key_line_number":    lineNumber + 1,
		"region":             "us-east-1",
		"region_is_literal":  true,
		"region_line_number": lineNumber + 2,
	}
}
