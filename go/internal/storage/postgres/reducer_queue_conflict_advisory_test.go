// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestAdvisoryGroupConflictPartition pins the #10411 claim partition: the
// same advisory group partitions to one conflict key across scopes (so two
// scopes never reconcile the same group concurrently), while disjoint
// groups partition apart (so they drain in parallel).
func TestAdvisoryGroupConflictPartition(t *testing.T) {
	t.Parallel()

	intent := func(scopeID, entityKey string) runtime.ReducerIntent {
		return runtime.ReducerIntent{
			ScopeID:   scopeID,
			Domain:    reducer.DomainAdvisoryGroupProjection,
			EntityKey: entityKey,
		}
	}

	domainA, keyA := reducerConflictDomainKey(intent("scope-a", "advisory_group:CVE-2026-0001"))
	domainB, keyB := reducerConflictDomainKey(intent("scope-b", "advisory_group:CVE-2026-0001"))
	_, keyOther := reducerConflictDomainKey(intent("scope-a", "advisory_group:CVE-2026-0002"))
	if domainA != reducerConflictDomainAdvisoryGroup || domainB != reducerConflictDomainAdvisoryGroup {
		t.Fatalf("domains = %q/%q, want %q", domainA, domainB, reducerConflictDomainAdvisoryGroup)
	}
	if keyA != keyB {
		t.Fatal("same group from two scopes partitioned apart: concurrent same-group reconcile is possible")
	}
	if keyA == keyOther {
		t.Fatal("disjoint groups partitioned together: parallel drain serializes on one key")
	}
}
