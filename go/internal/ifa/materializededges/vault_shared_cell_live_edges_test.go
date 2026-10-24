// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// sharedCellEdge is the identity triple every vault expected-edges file
// carries. The live assert compares exactly this triple with no scope, so a
// new vault family whose exact chains take the Vault hop spills its edges
// into the sibling asserts on the shared N-cell stack.
type sharedCellEdge struct {
	Relationship string `json:"relationship_type"`
	Source       string `json:"source_entity_id"`
	Target       string `json:"target_entity_id"`
}

// loadSharedCellEdges reads one expected-edges file's edge identities.
func loadSharedCellEdges(t *testing.T, path string) []sharedCellEdge {
	t.Helper()
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read expected-edges file %s: %v", path, err)
	}
	var body struct {
		Edges []sharedCellEdge `json:"edges"`
	}
	if err := json.Unmarshal(blob, &body); err != nil {
		t.Fatalf("parse expected-edges file %s: %v", path, err)
	}
	return body.Edges
}

// edgeSet indexes edge identities for exact-union comparison.
func edgeSet(edges []sharedCellEdge) map[sharedCellEdge]bool {
	set := make(map[sharedCellEdge]bool, len(edges))
	for _, e := range edges {
		set[e] = true
	}
	return set
}

// TestVaultSharedCellLiveEdgeSets pins the shared N-cell union the live
// determinism gate asserts. The authenticates and uses asserts run against a
// stack that also holds the grants scope, and assert-edges compares the
// (relationship, source, target) triple with no scope filter, so every exact
// grants chain with a Vault hop must appear in the sibling live files.
//
// The grants additions are cross-checked against two independent sources, not
// against the fixture under test: the four AUTHENTICATES identities are the
// verbatim EXTRA rows from determinism-matrix job 120887814049 on the red
// head, and the five USE_S identities follow the role-to-policy mapping in
// grants_secret_read_family_odu.go lanes A/B/7/8 (role 1 fans out to policies
// 1-2, role 2 to policy 3, role 3 to policy 4, role 4 to policy 5), which the
// PR review verified independently. The fault cells drive each family on a
// fresh per-family database, so the scope-local files below stay at their
// landed counts.
func TestVaultSharedCellLiveEdgeSets(t *testing.T) {
	root := filepath.Join("..", "testdata")
	authLocal := loadSharedCellEdges(t, filepath.Join(root, "authenticatesvaultrole", "ifa-authenticates-vault-role-family-expected-edges.json"))
	authLive := loadSharedCellEdges(t, filepath.Join(root, "authenticatesvaultrole", "ifa-authenticates-vault-role-family-expected-edges-live.json"))
	usesLocal := loadSharedCellEdges(t, filepath.Join(root, "usesvaultpolicy", "ifa-uses-vault-policy-family-expected-edges.json"))
	usesLive := loadSharedCellEdges(t, filepath.Join(root, "usesvaultpolicy", "ifa-uses-vault-policy-family-expected-edges-live.json"))

	if len(authLocal) != 2 {
		t.Fatalf("authenticates scope-local set = %d edges, want 2 (fault cells pin this file)", len(authLocal))
	}
	if len(usesLocal) != 3 {
		t.Fatalf("uses scope-local set = %d edges, want 3 (fault cells pin this file)", len(usesLocal))
	}

	const authRel = "SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE"
	// The two uses-scope edges predate the grants landing (row 21 comment,
	// four-edge live file): the uses fixture's two exact chains take the
	// same Vault hop.
	usesAuth := []sharedCellEdge{
		{authRel, "sha256:sa-uses-app-a", "sha256:uses-vault-role-1"},
		{authRel, "sha256:sa-uses-app-b", "sha256:uses-vault-role-2"},
	}
	grantsAuth := []sharedCellEdge{
		{authRel, "sha256:sa-grants-app-7", "sha256:grants-vault-role-3"},
		{authRel, "sha256:sa-grants-app-8", "sha256:grants-vault-role-4"},
		{authRel, "sha256:sa-grants-app-a", "sha256:grants-vault-role-1"},
		{authRel, "sha256:sa-grants-app-b", "sha256:grants-vault-role-2"},
	}
	wantAuthLive := edgeSet(authLocal)
	for _, e := range usesAuth {
		wantAuthLive[e] = true
	}
	for _, e := range grantsAuth {
		wantAuthLive[e] = true
	}
	if got := edgeSet(authLive); !equalEdgeSets(got, wantAuthLive) {
		t.Fatalf("authenticates live set = %d edges, want scope-local 2 + uses 2 + grants 4 = 8", len(authLive))
	}

	const usesRel = "SECRETS_IAM_USES_VAULT_POLICY"
	grantsUses := []sharedCellEdge{
		{usesRel, "sha256:grants-vault-role-1", "sha256:grants-vault-policy-1"},
		{usesRel, "sha256:grants-vault-role-1", "sha256:grants-vault-policy-2"},
		{usesRel, "sha256:grants-vault-role-2", "sha256:grants-vault-policy-3"},
		{usesRel, "sha256:grants-vault-role-3", "sha256:grants-vault-policy-4"},
		{usesRel, "sha256:grants-vault-role-4", "sha256:grants-vault-policy-5"},
	}
	wantUsesLive := edgeSet(usesLocal)
	for _, e := range grantsUses {
		wantUsesLive[e] = true
	}
	if got := edgeSet(usesLive); !equalEdgeSets(got, wantUsesLive) {
		t.Fatalf("uses live set = %d edges, want scope-local 3 + grants 5 = 8", len(usesLive))
	}
}

func equalEdgeSets(a, b map[sharedCellEdge]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for e := range a {
		if !b[e] {
			return false
		}
	}
	return true
}
