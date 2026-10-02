// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// followupKeyPrefixes maps every git-collector shared_followup reducer_domain to
// the entity-key prefix its envelope builder uses. It is the closed list the
// #7316 contract covers: a follow-up added to the collector without an entry
// here fails the count assertions below instead of skipping the shared key
// helper unnoticed.
var followupKeyPrefixes = map[string]string{
	"workload_identity":                "workload:",
	"deployable_unit_correlation":      "repo:",
	"code_call_materialization":        "repo:",
	"rationale_materialization":        "rationale:",
	"platform_infra_materialization":   "repo:",
	"workload_materialization":         "workload:",
	"deployment_mapping":               "deployment:",
	"sql_relationship_materialization": "sql:",
	"shell_exec_materialization":       "shell:",
	"inheritance_materialization":      "inheritance:",
	"code_import_repo_edge":            "repo:",
	"codeowners_ownership":             "codeowners:",
	"submodule_pin":                    "submodule:",
}

// deltaFollowupDomains are the follow-ups streamFacts emits before the delta
// early return (fact_builder.go), so a delta generation still carries them.
var deltaFollowupDomains = []string{
	"rationale_materialization",
	"codeowners_ownership",
	"submodule_pin",
	"shell_exec_materialization",
}

// followupKeyGeneration streams one generation for a repository whose fact name
// is repoName and whose checkout directory is dirName, and returns the
// repository fact's name plus each shared_followup entity_key by reducer_domain.
func followupKeyGeneration(t *testing.T, repoName, dirName string, delta bool) (string, map[string]string, int) {
	t.Helper()

	repoPath := filepath.Join(t.TempDir(), dirName)
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", repoPath, err)
	}
	repo := repositoryidentity.Metadata{
		ID:        "repository:r_7316abcd",
		Name:      repoName,
		LocalPath: repoPath,
	}
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-followup-key")
	snapshot.Delta = delta

	collected := buildStreamingGeneration(repoPath, repo, "run-followup-key", time.Now().UTC(), snapshot, false, "")
	var factName string
	keys := map[string]string{}
	followups := 0
	for _, envelope := range drainFactChannel(collected.Facts) {
		switch envelope.FactKind {
		case "repository":
			factName, _ = envelope.Payload["name"].(string)
		case "shared_followup":
			domain, _ := envelope.Payload["reducer_domain"].(string)
			key, _ := envelope.Payload["entity_key"].(string)
			keys[domain] = key
			followups++
		}
	}
	return factName, keys, followups
}

// TestFollowupEntityKeysUseRepositoryID pins #7384: every follow-up
// entity_key is derived from the repository ID, never from the repository
// fact name or the checkout directory basename. Names are not unique across
// a run (dependency-mode ESHU_BOOTSTRAP_PACKAGE_NAME overrides) and can end
// in a colon (which the alias normalizer cannot match), so name-keyed
// selection is wrong even when the name is available.
func TestFollowupEntityKeysUseRepositoryID(t *testing.T) {
	t.Parallel()

	const repoID = "repository:r_7316abcd"
	const repoName = "pkg-display"
	const dirName = "checkout-dir"

	t.Run("full", func(t *testing.T) {
		t.Parallel()
		factName, keys, followups := followupKeyGeneration(t, repoName, dirName, false)
		if factName != repoName {
			t.Fatalf("repository fact name = %q, want %q", factName, repoName)
		}
		if followups != len(followupKeyPrefixes) {
			t.Fatalf("shared_followup envelopes = %d, want %d (one per reducer_domain)", followups, len(followupKeyPrefixes))
		}
		for domain, prefix := range followupKeyPrefixes {
			got, ok := keys[domain]
			if !ok {
				t.Errorf("full generation missing shared_followup for reducer_domain %q", domain)
				continue
			}
			if want := prefix + repoID; got != want {
				t.Errorf("%s entity_key = %q, want %q (repository ID, not the fact name)", domain, got, want)
			}
		}
	})

	t.Run("delta", func(t *testing.T) {
		t.Parallel()
		_, keys, followups := followupKeyGeneration(t, repoName, dirName, true)
		if followups != len(deltaFollowupDomains) {
			t.Fatalf("delta shared_followup envelopes = %d, want %d", followups, len(deltaFollowupDomains))
		}
		for _, domain := range deltaFollowupDomains {
			got, ok := keys[domain]
			if !ok {
				t.Errorf("delta generation missing shared_followup for reducer_domain %q", domain)
				continue
			}
			if want := followupKeyPrefixes[domain] + repoID; got != want {
				t.Errorf("delta %s entity_key = %q, want %q", domain, got, want)
			}
		}
	})
}

// TestFollowupEntityKeysIgnoreDisplayName pins #7384: two generations for the
// same repository ID under different display names emit byte-identical keys,
// so selection cannot depend on the name. (This replaces the #7316
// no-churn test, whose byte-identical rationale ended with name-keyed
// emission; cassettes and golden rows regenerate under the new spelling.)
func TestFollowupEntityKeysIgnoreDisplayName(t *testing.T) {
	t.Parallel()

	_, namedKeys, _ := followupKeyGeneration(t, "pkg-display", "checkout-dir", false)
	_, renamedKeys, renamedFollowups := followupKeyGeneration(t, "other-name:", "checkout-dir", false)
	if renamedFollowups != len(followupKeyPrefixes) {
		t.Fatalf("shared_followup envelopes = %d, want %d", renamedFollowups, len(followupKeyPrefixes))
	}
	for domain, prefix := range followupKeyPrefixes {
		want := prefix + "repository:r_7316abcd"
		if got := renamedKeys[domain]; got != want {
			t.Errorf("%s entity_key = %q, want %q", domain, got, want)
		}
		if got := namedKeys[domain]; got != want {
			t.Errorf("%s entity_key under first name = %q, want %q (name-independent)", domain, got, want)
		}
	}
}
