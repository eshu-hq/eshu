// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

// TestInfraSearchKeyFieldsMirrorFreeTextPredicate keeps the key's field list
// equal to the property set the infra free-text predicate reads. The key is
// only a candidate filter, so it must cover every property the predicate can
// match (a missing field drops rows once the reader cuts over).
func TestInfraSearchKeyFieldsMirrorFreeTextPredicate(t *testing.T) {
	t.Parallel()

	refs := regexp.MustCompile(`\bn\.(\w+)\b`).FindAllStringSubmatch(deployment.InfraResourceFreeTextPredicate, -1)
	var predicate []string
	for _, m := range refs {
		if !slices.Contains(predicate, m[1]) {
			predicate = append(predicate, m[1])
		}
	}
	got := search.Fields()
	slices.Sort(predicate)
	slices.Sort(got)
	if !slices.Equal(got, predicate) {
		t.Fatalf("search.Fields() = %v, want the predicate's properties %v", got, predicate)
	}
	if len(got) != 12 {
		t.Fatalf("search.Fields() has %d fields, want 12", len(got))
	}
	// The predicate matches the resource type through
	// coalesce(n.resource_type, n.data_type, ''): data_type counts only when
	// resource_type is null. The clause must coalesce in the same order.
	if !strings.Contains(deployment.InfraResourceFreeTextPredicate, "coalesce(n.resource_type, n.data_type, '')") {
		t.Fatal("predicate no longer coalesces resource_type before data_type; re-derive the key clause")
	}
	if !strings.Contains(search.Clause("n"), "coalesce(n.resource_type, n.data_type)") {
		t.Fatal("clause does not coalesce resource_type before data_type")
	}
}
