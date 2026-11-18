// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"path/filepath"
	"strings"
	"testing"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

// TestRelationshipRetentionLockClassDiffersFromTreeKeys runs the module-wide
// advisory class scan for the #10823 relationship retention sweep class: its
// own sites must resolve to relationshipretention.LockClass, and no other
// two-integer site in the module may evaluate to it. The sweep never takes a
// session or waiting advisory lock, only the per-batch try lock, so every site
// in its directory must be that one class.
func TestRelationshipRetentionLockClassDiffersFromTreeKeys(t *testing.T) {
	l := openLedgerDB(t)
	root, self := goModuleRoot(t)
	classes, unresolved := scanAdvisoryClasses(t, root, self)
	if len(unresolved) > 0 {
		t.Fatalf("advisory lock sites the scan cannot resolve:\n%s", strings.Join(unresolved, "\n"))
	}
	leaf := filepath.Join(root, "internal", "storage", "postgres", "retention", "relationships") + string(filepath.Separator)
	var others []lockClass
	own := 0
	for _, c := range classes {
		if strings.HasPrefix(c.site, leaf) {
			if c.integer == nil || *c.integer != relationshipretention.LockClass {
				t.Fatalf("relationship retention site %s does not take class %d", c.site, relationshipretention.LockClass)
			}
			own++
			continue
		}
		others = append(others, c)
	}
	if own < 1 {
		t.Fatalf("scan found %d relationship retention lock sites, want the batch try-lock", own)
	}
	if hits := l.collisions(t, append(others, lockClass{site: "links slot class", integer: ptrInt64(linksfreshnessstore.SlotLockClass)}), relationshipretention.LockClass); len(hits) > 0 {
		t.Fatalf("relationship retention lock class %d collides with %v", relationshipretention.LockClass, hits)
	}
}
