// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestLanguageResultRepositoryMatchKeySeparatesRepositories is the unit-level
// statement of the #5167 code-family batch 2a review round 2, finding 2 rule
// (see package query's language_query_metadata_repository_key_test.go): a
// content-metadata merge key must include the repository, so a future reader
// can see the key contract without running a route.
//
// This is a family-owned white-box test (#6642): it calls
// languageResultRepositoryMatchKey directly, an unexported free function
// belonging to the language family, rather than driving it through the
// mounted route.
func TestLanguageResultRepositoryMatchKeySeparatesRepositories(t *testing.T) {
	t.Parallel()

	left := languageResultRepositoryMatchKey(querytestutil.CodeGrantGrantedRepo, querytestutil.LanguageMetadataSharedPath, "Function", querytestutil.LanguageMetadataSharedName, querytestutil.LanguageMetadataSharedStart)
	right := languageResultRepositoryMatchKey(querytestutil.CodeGrantOtherRepo, querytestutil.LanguageMetadataSharedPath, "Function", querytestutil.LanguageMetadataSharedName, querytestutil.LanguageMetadataSharedStart)
	if left == right {
		t.Fatalf("two repositories sharing path/label/name/start line produced the same key %q", left)
	}
	unattributedLeft := languageResultRepositoryMatchKey("", querytestutil.LanguageMetadataSharedPath, "Function", querytestutil.LanguageMetadataSharedName, querytestutil.LanguageMetadataSharedStart)
	unattributedRight := languageResultRepositoryMatchKey("", querytestutil.LanguageMetadataSharedPath, "Function", querytestutil.LanguageMetadataSharedName, querytestutil.LanguageMetadataSharedStart)
	if unattributedLeft != unattributedRight {
		t.Fatal("two rows that both carry no repository must share a key")
	}
	if unattributedLeft == left {
		t.Fatalf("a row with no repository shares key %q with one inside a repository", left)
	}
}
