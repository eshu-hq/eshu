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

// TestFollowupEntityKeysUseRepositoryFactName pins #7316: every follow-up
// entity_key is derived from the same repository name the repository fact
// publishes, never from the checkout directory basename. In dependency mode
// ESHU_BOOTSTRAP_PACKAGE_NAME sets that name, so the two differ; a key built
// from the path then selects no candidate in the reducer and the domain neither
// writes nor retracts.
func TestFollowupEntityKeysUseRepositoryFactName(t *testing.T) {
	t.Parallel()

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
			if want := prefix + factName; got != want {
				t.Errorf("%s entity_key = %q, want %q (repository fact name, not the checkout basename)", domain, got, want)
			}
		}
	})

	t.Run("delta", func(t *testing.T) {
		t.Parallel()
		factName, keys, followups := followupKeyGeneration(t, repoName, dirName, true)
		if followups != len(deltaFollowupDomains) {
			t.Fatalf("delta shared_followup envelopes = %d, want %d", followups, len(deltaFollowupDomains))
		}
		for _, domain := range deltaFollowupDomains {
			got, ok := keys[domain]
			if !ok {
				t.Errorf("delta generation missing shared_followup for reducer_domain %q", domain)
				continue
			}
			if want := followupKeyPrefixes[domain] + factName; got != want {
				t.Errorf("delta %s entity_key = %q, want %q", domain, got, want)
			}
		}
	})
}

// TestFollowupEntityKeysUnchangedWithoutDisplayName is the no-churn proof for
// #7316: with no display name the repository name IS the checkout basename, so
// every key stays byte-identical to the pre-change spelling. That is why the
// Ifá cassettes, the golden snapshot, queued work item ids and phase rows need
// no regeneration for repositories outside dependency mode.
func TestFollowupEntityKeysUnchangedWithoutDisplayName(t *testing.T) {
	t.Parallel()

	_, keys, followups := followupKeyGeneration(t, "checkout-dir", "checkout-dir", false)
	want := map[string]string{
		"workload_identity":                "workload:checkout-dir",
		"deployable_unit_correlation":      "repo:checkout-dir",
		"code_call_materialization":        "repo:checkout-dir",
		"rationale_materialization":        "rationale:checkout-dir",
		"platform_infra_materialization":   "repo:checkout-dir",
		"workload_materialization":         "workload:checkout-dir",
		"deployment_mapping":               "deployment:checkout-dir",
		"sql_relationship_materialization": "sql:checkout-dir",
		"shell_exec_materialization":       "shell:checkout-dir",
		"inheritance_materialization":      "inheritance:checkout-dir",
		"code_import_repo_edge":            "repo:checkout-dir",
		"codeowners_ownership":             "codeowners:checkout-dir",
		"submodule_pin":                    "submodule:checkout-dir",
	}
	if followups != len(want) {
		t.Fatalf("shared_followup envelopes = %d, want %d", followups, len(want))
	}
	for domain, wantKey := range want {
		if got := keys[domain]; got != wantKey {
			t.Errorf("%s entity_key = %q, want %q", domain, got, wantKey)
		}
	}
}
