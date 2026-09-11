// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestLanguageResultRepositoryMatchKeySeparatesRepositories is the unit-level
// statement of the #5167 code-family batch 2a review round 2, finding 2 rule
// (see language_query_metadata_repository_key_test.go): a content-metadata
// merge key must include the repository, so a future reader can see the key
// contract without running a route.
//
// This is a family-owned white-box test (#6642): it calls
// languageResultRepositoryMatchKey directly, an unexported free function
// belonging to the language family, rather than driving it through the
// mounted route.
func TestLanguageResultRepositoryMatchKeySeparatesRepositories(t *testing.T) {
	t.Parallel()

	left := languageResultRepositoryMatchKey(codeGrantGrantedRepo, languageMetadataSharedPath, "Function", languageMetadataSharedName, languageMetadataSharedStart)
	right := languageResultRepositoryMatchKey(codeGrantOtherRepo, languageMetadataSharedPath, "Function", languageMetadataSharedName, languageMetadataSharedStart)
	if left == right {
		t.Fatalf("two repositories sharing path/label/name/start line produced the same key %q", left)
	}
	unattributedLeft := languageResultRepositoryMatchKey("", languageMetadataSharedPath, "Function", languageMetadataSharedName, languageMetadataSharedStart)
	unattributedRight := languageResultRepositoryMatchKey("", languageMetadataSharedPath, "Function", languageMetadataSharedName, languageMetadataSharedStart)
	if unattributedLeft != unattributedRight {
		t.Fatal("two rows that both carry no repository must share a key")
	}
	if unattributedLeft == left {
		t.Fatalf("a row with no repository shares key %q with one inside a repository", left)
	}
}
