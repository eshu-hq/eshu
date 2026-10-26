// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	gitcontent "github.com/eshu-hq/eshu/go/internal/collector/git/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// differentialCatalogToken is every name-like token a tfvars value can hold.
// The catalog lists each one as a repository, so the matcher can resolve any
// evidence the raw body offers and a loss in the skeleton cannot hide behind a
// catalog miss.
var differentialCatalogToken = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9._-]{2,}`)

// TestTfvarsSkeletonEvidenceDifferential is the #8833 differential: over a
// corpus of real tfvars files, relationships.DiscoverEvidence must return the
// same evidence from the skeleton body as from the raw body, or the difference
// is listed. It runs only when ESHU_8833_DIFF_CORPUS names a directory of
// *.tfvars and *.tfvars.json files; ESHU_8833_DIFF_OUT, when set, receives the
// skeleton bodies for the SQL findings differential.
func TestTfvarsSkeletonEvidenceDifferential(t *testing.T) {
	dir := os.Getenv("ESHU_8833_DIFF_CORPUS")
	if dir == "" {
		t.Skip("set ESHU_8833_DIFF_CORPUS to a directory of tfvars files")
	}
	out := os.Getenv("ESHU_8833_DIFF_OUT")
	names, err := filepath.Glob(filepath.Join(dir, "*.tfvars*"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no tfvars files in %s: %v", dir, err)
	}
	sort.Strings(names)
	store := parser.ContentStore{Policy: parser.ContentStoreRedact, Redactor: parser.RedactorSkeleton}
	var raw, skeleton []facts.Envelope
	tokens := map[string]struct{}{}
	failed := 0
	for _, name := range names {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		result := gitcontent.ApplyContentStore(store, string(body))
		if result.Outcome == gitcontent.StoreOutcomeRedactorFailed {
			failed++
			continue
		}
		base := filepath.Base(name)
		raw = append(raw, differentialEnvelope(base, string(body)))
		skeleton = append(skeleton, differentialEnvelope(base, result.Body))
		for _, token := range differentialCatalogToken.FindAllString(string(body), -1) {
			tokens[strings.ToLower(token)] = struct{}{}
		}
		if out != "" {
			if err := os.WriteFile(filepath.Join(out, base), []byte(result.Body), 0o600); err != nil {
				t.Fatalf("write skeleton: %v", err)
			}
		}
	}
	catalog := make([]relationships.CatalogEntry, 0, len(tokens))
	for token := range tokens {
		catalog = append(catalog, relationships.CatalogEntry{RepoID: "repo://catalog/" + token, Aliases: []string{token}})
	}
	sort.Slice(catalog, func(i, j int) bool { return catalog[i].RepoID < catalog[j].RepoID })

	rawSet := evidenceSet(t, relationships.DiscoverEvidence(raw, catalog))
	skeletonSet := evidenceSet(t, relationships.DiscoverEvidence(skeleton, catalog))
	lost, gained := setDifference(rawSet, skeletonSet), setDifference(skeletonSet, rawSet)
	t.Logf("files=%d redactor_failed=%d catalog=%d raw_evidence=%d skeleton_evidence=%d lost=%d gained=%d",
		len(raw), failed, len(catalog), len(rawSet), len(skeletonSet), len(lost), len(gained))
	for _, key := range lost {
		t.Logf("lost: %s", key)
	}
	for _, key := range gained {
		t.Logf("gained: %s", key)
	}
}

func differentialEnvelope(path, body string) facts.Envelope {
	return facts.Envelope{FactKind: "content", Payload: map[string]any{
		"repo_id": "repo://diff/source", "content_path": path, "content_body": body, "artifact_type": "terraform_hcl",
	}}
}

// evidenceSet renders each evidence fact as its identity: kind, relationship,
// source, target, file, and start line. Byte offsets are left out on purpose:
// the skeleton rewrites the text before a match (a normalized URL, a marker),
// which moves the offset of the same evidence.
func evidenceSet(t *testing.T, evidence []relationships.EvidenceFact) map[string]struct{} {
	t.Helper()
	set := map[string]struct{}{}
	for _, fact := range evidence {
		key, err := json.Marshal([]any{
			fact.EvidenceKind, fact.RelationshipType, fact.SourceRepoID, fact.TargetRepoID,
			fact.Details["path"], fact.Details["start_line"],
		})
		if err != nil {
			t.Fatalf("marshal evidence key: %v", err)
		}
		set[string(key)] = struct{}{}
	}
	return set
}

func setDifference(a, b map[string]struct{}) []string {
	var out []string
	for key := range a {
		if _, ok := b[key]; !ok {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
