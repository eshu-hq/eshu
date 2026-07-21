// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestParsePinnedRefsJSONEmptyFeatureOff(t *testing.T) {
	t.Parallel()

	got, err := parsePinnedRefsJSON("")
	if err != nil {
		t.Fatalf("parsePinnedRefsJSON(empty) error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("parsePinnedRefsJSON(empty) = %v, want nil", got)
	}
}

func TestParsePinnedRefsJSONValid(t *testing.T) {
	t.Parallel()

	raw := `{"eshu-hq/service": ["feature-x", "bugfix-y"], "eshu-hq/lib": ["v2"]}`
	got, err := parsePinnedRefsJSON(raw)
	if err != nil {
		t.Fatalf("parsePinnedRefsJSON error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(parsed) = %d, want 2", len(got))
	}
	refs := got["eshu-hq/service"]
	if len(refs) != 2 || refs[0] != "feature-x" || refs[1] != "bugfix-y" {
		t.Fatalf("eshu-hq/service refs = %v, want [feature-x bugfix-y]", refs)
	}
	if got["eshu-hq/lib"][0] != "v2" {
		t.Fatalf("eshu-hq/lib refs = %v, want [v2]", got["eshu-hq/lib"])
	}
}

func TestParsePinnedRefsJSONInvalidRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{"backslash", `{"eshu-hq/service": ["bad\\ref"]}`},
		{"spaces", `{"eshu-hq/service": ["bad ref"]}`},
		{"double-dot", `{"eshu-hq/service": ["bad..ref"]}`},
		{"HEAD", `{"eshu-hq/service": ["HEAD"]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parsePinnedRefsJSON(tc.raw)
			if err == nil {
				t.Fatalf("parsePinnedRefsJSON(%q) error = nil, want error", tc.raw)
			}
		})
	}
}

func TestParsePinnedRefsJSONDedupsWithinRepoArray(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "trailing-duplicates",
			raw:  `{"owner/repo": ["main", "main", "dev", "dev"]}`,
			want: []string{"main", "dev"},
		},
		{
			name: "mixed-position-duplicate",
			raw:  `{"owner/repo": ["a", "b", "a"]}`,
			want: []string{"a", "b"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parsePinnedRefsJSON(tc.raw)
			if err != nil {
				t.Fatalf("parsePinnedRefsJSON(%q) error = %v, want nil", tc.raw, err)
			}
			refs := got["owner/repo"]
			if len(refs) != len(tc.want) {
				t.Fatalf("owner/repo refs = %v, want %v", refs, tc.want)
			}
			for i, ref := range refs {
				if ref != tc.want[i] {
					t.Fatalf("owner/repo refs = %v, want %v", refs, tc.want)
				}
			}
		})
	}
}

func TestLoadRepoSyncConfigPinnedRefsEnv(t *testing.T) {
	t.Parallel()

	env := func(key string) string {
		switch key {
		case "ESHU_PINNED_REFS_JSON":
			return `{"eshu-hq/repo": ["feat-a", "feat-b"]}`
		case "ESHU_REPO_SOURCE_MODE":
			return "explicit"
		default:
			return ""
		}
	}
	config, err := LoadRepoSyncConfig("collector-git", env)
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig error = %v, want nil", err)
	}
	refs := config.PinnedRefsByRepoID["eshu-hq/repo"]
	if len(refs) != 2 || refs[0] != "feat-a" || refs[1] != "feat-b" {
		t.Fatalf("PinnedRefsByRepoID[eshu-hq/repo] = %v, want [feat-a feat-b]", refs)
	}
	if config.PinnedRefPerRepoCap != 3 {
		t.Fatalf("PinnedRefPerRepoCap = %d, want 3 (default)", config.PinnedRefPerRepoCap)
	}
}

func TestPinnedRefPerRepoCapDefault(t *testing.T) {
	t.Parallel()

	cap := pinnedRefPerRepoCap(func(string) string { return "" })
	if cap != 3 {
		t.Fatalf("pinnedRefPerRepoCap(default) = %d, want 3", cap)
	}
}

func TestBuildScopeRefScoped(t *testing.T) {
	t.Parallel()

	repo, err := repositoryidentity.MetadataFor(
		"example-org/example-repo",
		"/repos/example-repo",
		"https://github.com/example-org/example-repo.git",
	)
	if err != nil {
		t.Fatalf("MetadataFor error = %v", err)
	}
	got := buildScope(repo, "feature-x")

	// Ref-scoped scope uses KindRepositoryRef, not KindRepository.
	if got.ScopeKind != scope.KindRepositoryRef {
		t.Fatalf("ScopeKind = %q, want %q", got.ScopeKind, scope.KindRepositoryRef)
	}
	// ScopeID carries ref suffix.
	if !strings.Contains(got.ScopeID, "@feature-x") {
		t.Fatalf("ScopeID = %q, want to contain @feature-x", got.ScopeID)
	}
	// Metadata carries ref.
	if got.Metadata["ref"] != "feature-x" {
		t.Fatalf("Metadata[ref] = %q, want feature-x", got.Metadata["ref"])
	}
	// PartitionKey stays bare repo ID.
	if got.PartitionKey != repo.ID {
		t.Fatalf("PartitionKey = %q, want %q (bare repo ID)", got.PartitionKey, repo.ID)
	}
}

func TestBuildScopeDefaultBranch(t *testing.T) {
	t.Parallel()

	repo, err := repositoryidentity.MetadataFor(
		"example-org/example-repo",
		"/repos/example-repo",
		"https://github.com/example-org/example-repo.git",
	)
	if err != nil {
		t.Fatalf("MetadataFor error = %v", err)
	}
	got := buildScope(repo, "")

	// Default branch uses KindRepository.
	if got.ScopeKind != scope.KindRepository {
		t.Fatalf("ScopeKind = %q, want %q", got.ScopeKind, scope.KindRepository)
	}
	// No ref in metadata.
	if _, ok := got.Metadata["ref"]; ok {
		t.Fatal("Metadata should not have ref key when ref is empty")
	}
}

// TestFactsCarryRefOnRefScoped proves ref-scoped facts carry a "ref" payload
// field and default-branch facts do not.
func TestFactsCarryRefOnRefScoped(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	repo, err := repositoryidentity.MetadataFor(
		"test-repo",
		repoDir,
		"https://github.com/test-org/test-repo.git",
	)
	if err != nil {
		t.Fatalf("MetadataFor error = %v", err)
	}

	snapshot := RepositorySnapshot{
		RepoPath:  repoDir,
		FileCount: 1,
		FileData: []map[string]any{
			{"path": filepath.Join(repoDir, "main.go"), "language": "go", "lang": "go"},
		},
		HeadCommitSHA: "abc123",
	}
	observedAt := time.Date(2026, time.July, 20, 1, 0, 0, 0, time.UTC)

	// Ref-scoped generation
	refCollected := buildStreamingGeneration(repoDir, repo, "run-ref", observedAt, snapshot, false, "feature-x")
	refFacts := drainFactChannel(refCollected.Facts)

	// Default-branch generation
	defCollected := buildStreamingGeneration(repoDir, repo, "run-default", observedAt, snapshot, false, "")
	defFacts := drainFactChannel(defCollected.Facts)

	// Find the repository fact in each.
	refRepoFact := findFactByKind(t, refFacts, "repository")
	defRepoFact := findFactByKind(t, defFacts, "repository")

	// Ref-scoped repository fact must carry ref.
	if ref, ok := refRepoFact.Payload["ref"]; !ok || ref != "feature-x" {
		t.Fatalf("ref-scoped repository fact Payload[ref] = %v, want feature-x", ref)
	}

	// Default-branch repository fact must NOT carry ref.
	if ref, ok := defRepoFact.Payload["ref"]; ok {
		t.Fatalf("default-branch repository fact Payload[ref] = %v, want absent", ref)
	}

	// Find a file fact in each.
	refFileFact := findFactByKind(t, refFacts, "file")
	defFileFact := findFactByKind(t, defFacts, "file")

	if ref, ok := refFileFact.Payload["ref"]; !ok || ref != "feature-x" {
		t.Fatalf("ref-scoped file fact Payload[ref] = %v, want feature-x", ref)
	}
	if ref, ok := defFileFact.Payload["ref"]; ok {
		t.Fatalf("default-branch file fact Payload[ref] = %v, want absent", ref)
	}

	// Verify ref-scoped scope identity.
	if refCollected.Scope.ScopeKind != scope.KindRepositoryRef {
		t.Fatalf("ref-scoped ScopeKind = %q, want %q", refCollected.Scope.ScopeKind, scope.KindRepositoryRef)
	}
	if defCollected.Scope.ScopeKind != scope.KindRepository {
		t.Fatalf("default-branch ScopeKind = %q, want %q", defCollected.Scope.ScopeKind, scope.KindRepository)
	}
}

func findFactByKind(t *testing.T, envelopes []facts.Envelope, kind string) facts.Envelope {
	t.Helper()
	for _, env := range envelopes {
		if env.FactKind == kind {
			return env
		}
	}
	t.Fatalf("no fact with kind %q in %d envelopes", kind, len(envelopes))
	return facts.Envelope{}
}

// TestBuildStreamingGenerationDefaultBranchByteIdentical proves that when the
// feature is unset (empty ref), built scope and identity are the same as
// before — byte-identical KindRepository, no ref in metadata.
func TestBuildStreamingGenerationDefaultBranchByteIdentical(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	writeSelectionTestFile(t, filepath.Join(repoDir, "main.go"), "package main\n")

	repo, err := repositoryidentity.MetadataFor(
		"test-repo-default",
		repoDir,
		"https://github.com/test-org/test-repo.git",
	)
	if err != nil {
		t.Fatalf("MetadataFor error = %v", err)
	}

	snapshot := RepositorySnapshot{
		RepoPath:      repoDir,
		FileCount:     1,
		HeadCommitSHA: "abc123",
	}
	observedAt := time.Date(2026, time.July, 20, 1, 0, 0, 0, time.UTC)

	collected := buildStreamingGeneration(repoDir, repo, "run-def", observedAt, snapshot, false, "")

	if collected.Scope.ScopeKind != scope.KindRepository {
		t.Fatalf("default-branch ScopeKind = %q, want %q (KindRepository)", collected.Scope.ScopeKind, scope.KindRepository)
	}
	if _, ok := collected.Scope.Metadata["ref"]; ok {
		t.Fatal("default-branch scope metadata must not contain ref key")
	}
}
