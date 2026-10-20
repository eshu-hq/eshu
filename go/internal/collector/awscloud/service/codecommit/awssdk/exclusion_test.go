// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsContentReadsAndMutation is the metadata-only
// acceptance gate for the CodeCommit adapter. The real CodeCommit SDK client
// exposes commit, ref, blob, file-content, pull-request, and comment readers
// plus a full mutation surface, so the danger is that a future edit widens the
// adapter-local apiClient interface to reach repository contents. This test
// reflects over apiClient and fails the build if any forbidden content reader
// or mutation method becomes reachable, proving the scanner can only see
// repository metadata.
func TestAdapterInterfaceForbidsContentReadsAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// Commit, ref, blob, and file-content readers — repository contents.
		"GetFile", "GetFolder", "GetBlob", "GetCommit", "BatchGetCommits",
		"GetDifferences", "GetBranch", "ListBranches", "GetMergeCommit",
		"GetMergeConflicts", "GetMergeOptions", "GetObjectIdentifier",
		// Pull-request, approval, and comment bodies.
		"PullRequest", "Comment", "Approval", "Reaction",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Post", "Merge",
		"Associate", "Disassociate", "Override", "Tag", "Untag",
		"Test", "Evaluate", "BatchDescribe",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatal("apiClient has no methods; expected the CodeCommit metadata read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden content/mutation method %q; the CodeCommit adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the CodeCommit adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreBoundedMetadataReads asserts every apiClient method is a
// List, Get, or Batch metadata read so the read surface stays explicit and
// auditable. BatchGetRepositories is the only Batch read allowed, and it
// returns repository metadata, never commit or file content.
func TestAdapterMethodsAreBoundedMetadataReads(t *testing.T) {
	allowed := map[string]struct{}{
		"ListRepositories":      {},
		"BatchGetRepositories":  {},
		"GetRepositoryTriggers": {},
		"ListTagsForResource":   {},
	}
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if _, ok := allowed[name]; !ok {
			t.Fatalf("apiClient method %q is not in the bounded metadata read allowlist", name)
		}
	}
}
