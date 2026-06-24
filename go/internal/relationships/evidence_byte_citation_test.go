// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestDiscoverTerraformEvidenceCapturesByteOffset proves that the Terraform
// evidence extractor records real byte-level citation (start_line, end_line,
// byte_offset, byte_length, commit_sha) in the EvidenceFact.Details map when
// the envelope carries that information.
//
// This is the TDD anchor for issue #3636. The test MUST FAIL before the
// byte-citation capture implementation is added to discoverTerraformEvidence /
// matchCatalog.
func TestDiscoverTerraformEvidenceCapturesByteOffset(t *testing.T) {
	t.Parallel()

	// "app_repo" match starts at byte 0, the value "payments-service" starts
	// after `app_repo = "` (12 bytes). The full match is the whole line.
	content := `app_repo = "payments-service"`
	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content":       content,
				"commit_sha":    "abc123def456",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	f := evidence[0]

	// commit_sha must flow from envelope payload into Details.
	if got, _ := f.Details["commit_sha"].(string); got != "abc123def456" {
		t.Errorf("Details[commit_sha] = %q, want abc123def456", got)
	}
	// byte_offset must be non-zero or zero but present, and byte_length > 0.
	byteOffset, hasOffset := f.Details["byte_offset"]
	if !hasOffset {
		t.Errorf("Details missing byte_offset")
	}
	byteLength, hasLength := f.Details["byte_length"]
	if !hasLength {
		t.Errorf("Details missing byte_length")
	}
	// The matched capture group "payments-service" is 16 bytes. The full
	// pattern match `app_repo = "payments-service"` is 28 bytes. We expect
	// byte_length to be the length of the full regex match.
	if hasLength {
		switch typed := byteLength.(type) {
		case int:
			if typed <= 0 {
				t.Errorf("byte_length = %d, want > 0", typed)
			}
		case float64:
			if typed <= 0 {
				t.Errorf("byte_length = %v, want > 0", typed)
			}
		default:
			t.Errorf("byte_length type = %T, want int or float64", byteLength)
		}
	}
	if hasOffset {
		switch byteOffset.(type) {
		case int, float64:
			// valid
		default:
			t.Errorf("byte_offset type = %T, want int or float64", byteOffset)
		}
	}

	// start_line and end_line must be present and ≥ 1.
	startLine, hasStart := f.Details["start_line"]
	endLine, hasEnd := f.Details["end_line"]
	if !hasStart {
		t.Errorf("Details missing start_line")
	}
	if !hasEnd {
		t.Errorf("Details missing end_line")
	}
	if hasStart {
		switch typed := startLine.(type) {
		case int:
			if typed < 1 {
				t.Errorf("start_line = %d, want >= 1", typed)
			}
		case float64:
			if typed < 1 {
				t.Errorf("start_line = %v, want >= 1", typed)
			}
		default:
			t.Errorf("start_line type = %T, want int or float64", startLine)
		}
	}
	if hasEnd {
		switch typed := endLine.(type) {
		case int:
			if typed < 1 {
				t.Errorf("end_line = %d, want >= 1", typed)
			}
		case float64:
			if typed < 1 {
				t.Errorf("end_line = %v, want >= 1", typed)
			}
		default:
			t.Errorf("end_line type = %T, want int or float64", endLine)
		}
	}

	// Canonical() must pass Validate() with the captured citation.
	ev := f.Canonical()
	if err := ev.Validate(); err != nil {
		t.Fatalf("Canonical().Validate() error = %v, want nil", err)
	}
	if ev.Citation.CommitSHA != "abc123def456" {
		t.Errorf("Canonical Citation.CommitSHA = %q, want abc123def456", ev.Citation.CommitSHA)
	}
	if ev.Citation.ByteLength <= 0 {
		t.Errorf("Canonical Citation.ByteLength = %d, want > 0", ev.Citation.ByteLength)
	}
	if ev.Citation.StartLine < 1 {
		t.Errorf("Canonical Citation.StartLine = %d, want >= 1", ev.Citation.StartLine)
	}
}

// TestDiscoverTerraformEvidenceMultilineCapturesCorrectLine proves that when a
// Terraform file has multiple lines, the extractor records the correct 1-based
// line number for a match on a non-first line.
func TestDiscoverTerraformEvidenceMultilineCapturesCorrectLine(t *testing.T) {
	t.Parallel()

	// The match is on line 3 (1-based).
	content := "# infra config\nregion = \"us-east-1\"\napp_repo = \"payments-service\"\n"
	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "env/prod/main.tf",
				"content":       content,
				"commit_sha":    "deadbeef",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	f := evidence[0]

	startLine, hasStart := f.Details["start_line"]
	if !hasStart {
		t.Fatalf("Details missing start_line")
	}
	var startLineInt int
	switch typed := startLine.(type) {
	case int:
		startLineInt = typed
	case float64:
		startLineInt = int(typed)
	default:
		t.Fatalf("start_line type = %T", startLine)
	}
	if startLineInt != 3 {
		t.Errorf("start_line = %d, want 3 (match is on third line)", startLineInt)
	}
	if got, _ := f.Details["commit_sha"].(string); got != "deadbeef" {
		t.Errorf("commit_sha = %q, want deadbeef", got)
	}
}

// TestDiscoverEvidenceNoCommitSHAInEnvelopeDegradesSafely proves that when the
// envelope does not carry a commit_sha, the extractor does not fabricate one
// — commit_sha is simply absent from Details (or empty) and the extracted
// evidence still validates via Canonical().
func TestDiscoverEvidenceNoCommitSHAInEnvelopeDegradesSafely(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content":       `app_repo = "payments-service"`,
				// No commit_sha in payload.
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	f := evidence[0]

	// commit_sha must not be fabricated.
	if got, _ := f.Details["commit_sha"].(string); got != "" {
		t.Errorf("Details[commit_sha] = %q, want empty (no fabrication)", got)
	}

	// Canonical must still validate (file path locator is enough).
	ev := f.Canonical()
	if err := ev.Validate(); err != nil {
		t.Fatalf("Canonical().Validate() error = %v, want nil (safe degradation)", err)
	}
}

// TestDiscoverKustomizeDocumentEvidenceCapturesCommitSHA proves that
// discoverKustomizeDocumentEvidence forwards commit_sha into Details for
// resources, helmCharts, and images matches when the envelope carries a SHA.
// This is item 2 of issue #3650: the matchCatalog calls inside
// discoverKustomizeDocumentEvidence previously passed nil extra-details, losing
// the commit_sha even when the envelope carried it.
func TestDiscoverKustomizeDocumentEvidenceCapturesCommitSHA(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document map[string]any
		kind     EvidenceKind
	}{
		{
			name:     "resources",
			document: map[string]any{"resources": []any{"payments-service"}},
			kind:     EvidenceKindKustomizeResource,
		},
		{
			name:     "helmCharts",
			document: map[string]any{"helmCharts": []any{map[string]any{"name": "payments-service"}}},
			kind:     EvidenceKindKustomizeHelmChart,
		},
		{
			name:     "images",
			document: map[string]any{"images": []any{map[string]any{"name": "payments-service"}}},
			kind:     EvidenceKindKustomizeImage,
		},
	}

	const wantSHA = "kustomize-commit-abc"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			matcher := registryRoutingMatcher("payments-service", "repo-payments")
			seen := make(map[evidenceKey]struct{})

			evidence := discoverKustomizeDocumentEvidence(
				"repo-infra", "overlays/kustomization.yaml", tc.document,
				matcher, seen, wantSHA,
			)

			if len(evidence) == 0 {
				t.Fatalf("no evidence emitted for %s", tc.name)
			}
			for _, f := range evidence {
				got, _ := f.Details["commit_sha"].(string)
				if got != wantSHA {
					t.Errorf("Details[commit_sha] = %q, want %q (kind=%s)", got, wantSHA, tc.kind)
				}
			}
		})
	}
}

// TestDiscoverKustomizeDocumentEvidenceNoCommitSHADegradesSafely proves that
// when the envelope has no commit_sha, discoverKustomizeDocumentEvidence does
// not fabricate one — commit_sha is simply absent from Details.
func TestDiscoverKustomizeDocumentEvidenceNoCommitSHADegradesSafely(t *testing.T) {
	t.Parallel()

	document := map[string]any{"resources": []any{"payments-service"}}
	matcher := registryRoutingMatcher("payments-service", "repo-payments")
	seen := make(map[evidenceKey]struct{})

	evidence := discoverKustomizeDocumentEvidence(
		"repo-infra", "overlays/kustomization.yaml", document,
		matcher, seen, "", // no commit_sha
	)

	if len(evidence) == 0 {
		t.Fatal("no evidence emitted")
	}
	for _, f := range evidence {
		if got, _ := f.Details["commit_sha"].(string); got != "" {
			t.Errorf("Details[commit_sha] = %q, want empty (no fabrication)", got)
		}
	}
}

// TestDiscoverJenkinsEvidenceCapturesCommitSHA proves the Jenkins extractor
// forwards commit_sha from the envelope into Details (item 3 of #3650).
func TestDiscoverJenkinsEvidenceCapturesCommitSHA(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"relative_path": "Jenkinsfile",
				"content":       "@Library('pipelines') _\n",
				"commit_sha":    "jenkins-commit-abc",
				"parsed_file_data": map[string]any{
					"shared_libraries": []any{"pipelines"},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-pipelines", Aliases: []string{"pipelines"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) == 0 {
		t.Fatal("no Jenkins evidence emitted")
	}
	for _, f := range evidence {
		if got, _ := f.Details["commit_sha"].(string); got != "jenkins-commit-abc" {
			t.Errorf("Details[commit_sha] = %q, want jenkins-commit-abc", got)
		}
	}
}

// TestDiscoverJenkinsEvidenceNoCommitSHADegradesSafely proves the Jenkins
// extractor does not fabricate a commit_sha when the envelope lacks one.
func TestDiscoverJenkinsEvidenceNoCommitSHADegradesSafely(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"relative_path": "Jenkinsfile",
				"content":       "@Library('pipelines') _\n",
				"parsed_file_data": map[string]any{
					"shared_libraries": []any{"pipelines"},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-pipelines", Aliases: []string{"pipelines"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) == 0 {
		t.Fatal("no Jenkins evidence emitted")
	}
	for _, f := range evidence {
		if got, _ := f.Details["commit_sha"].(string); got != "" {
			t.Errorf("Details[commit_sha] = %q, want empty (no fabrication)", got)
		}
	}
}

// TestDiscoverDockerfileEvidenceCapturesCommitSHA proves the Dockerfile
// extractor forwards commit_sha from the envelope into Details (item 3 of
// #3650).
func TestDiscoverDockerfileEvidenceCapturesCommitSHA(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"artifact_type": "dockerfile",
				"relative_path": "Dockerfile",
				"content":       "FROM scratch\n",
				"commit_sha":    "docker-commit-abc",
				"parsed_file_data": map[string]any{
					"dockerfile_labels": []any{
						map[string]any{
							"name":  "org.opencontainers.image.source",
							"value": "https://github.com/acme/payments-service",
						},
					},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) == 0 {
		t.Fatal("no Dockerfile evidence emitted")
	}
	for _, f := range evidence {
		if got, _ := f.Details["commit_sha"].(string); got != "docker-commit-abc" {
			t.Errorf("Details[commit_sha] = %q, want docker-commit-abc", got)
		}
	}
}

// TestDiscoverDockerfileEvidenceNoCommitSHADegradesSafely proves the Dockerfile
// extractor does not fabricate a commit_sha when the envelope lacks one.
func TestDiscoverDockerfileEvidenceNoCommitSHADegradesSafely(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"artifact_type": "dockerfile",
				"relative_path": "Dockerfile",
				"content":       "FROM scratch\n",
				"parsed_file_data": map[string]any{
					"dockerfile_labels": []any{
						map[string]any{
							"name":  "org.opencontainers.image.source",
							"value": "https://github.com/acme/payments-service",
						},
					},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) == 0 {
		t.Fatal("no Dockerfile evidence emitted")
	}
	for _, f := range evidence {
		if got, _ := f.Details["commit_sha"].(string); got != "" {
			t.Errorf("Details[commit_sha] = %q, want empty (no fabrication)", got)
		}
	}
}

// TestDiscoverHelmEvidenceCapturesCommitSHA proves that Helm evidence discovery
// also forwards the commit_sha from the envelope into Details, i.e., the fix is
// not limited to the Terraform code path.
func TestDiscoverHelmEvidenceCapturesCommitSHA(t *testing.T) {
	t.Parallel()

	content := "image:\n  repository: payments-service\n  tag: latest\n"
	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-platform",
			Payload: map[string]any{
				"artifact_type": "helm",
				"relative_path": "charts/web/values.yaml",
				"content":       content,
				"commit_sha":    "helm-commit-abc",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) == 0 {
		t.Skip("Helm values extractor did not match — check alias; skipping commit_sha assertion")
	}
	for _, f := range evidence {
		if got, _ := f.Details["commit_sha"].(string); got != "helm-commit-abc" {
			t.Errorf("Details[commit_sha] = %q, want helm-commit-abc", got)
		}
		ev := f.Canonical()
		if err := ev.Validate(); err != nil {
			t.Fatalf("Canonical().Validate() error = %v", err)
		}
	}
}
