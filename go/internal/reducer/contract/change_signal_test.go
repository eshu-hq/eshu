// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"strings"
	"testing"
)

// TestAWSRuntimeDriftDiffAddedChangedRemoved pins the AWS runtime-drift
// change signal (#10878 decision 2): the delta snapshot is the
// added/changed/removed ARN split of the generation diff, and removals are
// reported (never silently dropped) so the handler retracts findings.
func TestAWSRuntimeDriftDiffAddedChangedRemoved(t *testing.T) {
	oldARNs := map[string]string{"arn:a": "h1", "arn:b": "h2"}
	newARNs := map[string]string{"arn:b": "h2x", "arn:c": "h3"}
	diff := DiffARNRecords(oldARNs, newARNs)
	if len(diff.Added) != 1 || diff.Added[0] != "arn:c" {
		t.Fatalf("added = %v, want [arn:c]", diff.Added)
	}
	if len(diff.Changed) != 1 || diff.Changed[0] != "arn:b" {
		t.Fatalf("changed = %v, want [arn:b]", diff.Changed)
	}
	if len(diff.Removed) != 1 || diff.Removed[0] != "arn:a" {
		t.Fatalf("removed = %v, want [arn:a]", diff.Removed)
	}
	if !diff.HasChanges() {
		t.Fatal("HasChanges = false, want true")
	}
	if got := len(diff.ChangedKeys()); got != 3 {
		t.Fatalf("ChangedKeys = %d, want 3", got)
	}
}

// TestAWSRuntimeDriftDiffNoChange verifies the no-change case: identical
// payload hashes produce an empty delta so the run can skip.
func TestAWSRuntimeDriftDiffNoChange(t *testing.T) {
	records := map[string]string{"arn:a": "h1", "arn:b": "h2"}
	diff := DiffARNRecords(records, map[string]string{"arn:a": "h1", "arn:b": "h2"})
	if diff.HasChanges() {
		t.Fatalf("HasChanges = true on identical sets: %+v", diff)
	}
	if len(diff.ChangedKeys()) != 0 {
		t.Fatalf("ChangedKeys = %v, want empty", diff.ChangedKeys())
	}
}

// TestAWSRuntimeDriftDiffScalesWithChanged is the cheapest theory shim for
// the #10878 pre-build gate: diff work tracks changed ARNs, not total ARNs.
// A 1000-record generation with ~1% change must report exactly that change
// set, so a delta load bounded by the diff stays far below a full load.
func TestAWSRuntimeDriftDiffScalesWithChanged(t *testing.T) {
	oldARNs := make(map[string]string, 1000)
	newARNs := make(map[string]string, 1000)
	for i := 0; i < 1000; i++ {
		arn := strings.Repeat("a", 8) + string(rune('0'+i%10)) + string(rune('0'+(i/10)%10)) + string(rune('0'+(i/100)%10))
		oldARNs[arn] = "payload-v1"
		newARNs[arn] = "payload-v1"
	}
	for i := 0; i < 10; i++ {
		arn := strings.Repeat("a", 8) + string(rune('0'+i%10)) + string(rune('0'+(i/10)%10)) + string(rune('0'+(i/100)%10))
		newARNs[arn] = "payload-v2"
	}
	diff := DiffARNRecords(oldARNs, newARNs)
	if len(diff.Changed) != 10 {
		t.Fatalf("changed = %d, want 10", len(diff.Changed))
	}
	if len(diff.Added) != 0 || len(diff.Removed) != 0 {
		t.Fatalf("added/removed = %v/%v, want empty", diff.Added, diff.Removed)
	}
}

// TestSupplyChainImpactDirtySignal verifies the supply-chain marked key
// (#10878 decision 2): the dirty (CVE, package, PURL) set derives the
// scoped stamp, and an empty dirty set means no genuine re-derivation.
func TestSupplyChainImpactDirtySignal(t *testing.T) {
	scoped := SupplyChainImpactChangeSignal{
		DirtyCVEIDs:     []string{"CVE-2026-0001"},
		DirtyPackageIDs: []string{"pkg:npm/left-pad@1.3.0"},
	}
	if !scoped.NeedsRederivation() {
		t.Fatal("NeedsRederivation = false with dirty keys, want true")
	}
	keys := scoped.DirtyKeys()
	if len(keys) != 2 {
		t.Fatalf("DirtyKeys = %v, want 2 keys", keys)
	}
	empty := SupplyChainImpactChangeSignal{}
	if empty.NeedsRederivation() {
		t.Fatal("NeedsRederivation = true with no dirty keys, want false")
	}
}

// TestWorkloadMaterializationFingerprintChanged pins the workload
// fingerprint gate (#10878 decision 2): reopen only on fingerprint
// mismatch, missing recorded row, or logic-version move — the #8283
// contract generalized to workload materialization.
func TestWorkloadMaterializationFingerprintChanged(t *testing.T) {
	same := WorkloadMaterializationChangeSignal{
		RecordedValue: "abc123", RecordedVersion: WorkloadMaterializationLogicVersion,
		CurrentValue: "abc123", CurrentVersion: WorkloadMaterializationLogicVersion,
	}
	if same.InputChanged() {
		t.Fatal("InputChanged = true on identical fingerprint+version, want false")
	}
	moved := same
	moved.CurrentValue = "def456"
	if !moved.InputChanged() {
		t.Fatal("InputChanged = false on changed fingerprint, want true")
	}
	missing := WorkloadMaterializationChangeSignal{
		CurrentValue: "abc123", CurrentVersion: WorkloadMaterializationLogicVersion,
	}
	if !missing.InputChanged() {
		t.Fatal("InputChanged = false with no recorded row, want true")
	}
	bumped := same
	bumped.CurrentVersion = WorkloadMaterializationLogicVersion + 1
	if !bumped.InputChanged() {
		t.Fatal("InputChanged = false on logic-version move, want true")
	}
}

// TestContainerImageIdentityEpochChanged pins the container-identity
// fingerprint (#10878 decision 2): the identity epoch (count, max
// observed_at, ordered-set SHA-256 fingerprint) gates reopen, and the
// marked key is the changed (scope, digest) pair set for narrowing.
func TestContainerImageIdentityEpochChanged(t *testing.T) {
	before := ContainerImageIdentityChangeSignal{
		EpochCount: 10, EpochMaxObservedAt: "2026-11-19T00:00:00Z",
		EpochFingerprint: "fingerprint-v1",
	}
	after := before
	if after.EpochChanged(before) {
		t.Fatal("EpochChanged = true on identical epoch, want false")
	}
	after.EpochFingerprint = "fingerprint-v2"
	if !after.EpochChanged(before) {
		t.Fatal("EpochChanged = false on fingerprint move, want true")
	}
	pairs := DiffIdentityPairs(
		[]string{"scope-a:digest-1", "scope-b:digest-2"},
		[]string{"scope-b:digest-2", "scope-c:digest-3"},
	)
	if len(pairs.Added) != 1 || len(pairs.Removed) != 1 {
		t.Fatalf("pairs diff = %+v, want 1 added + 1 removed", pairs)
	}
}

// TestSearchDocumentsRepoFingerprint pins the search-documents signal
// (#10878 decision 2): a per-repository fingerprint (count, max indexed_at,
// ordered-set SHA-256 of document keys) skips unchanged repos, and dirty
// document keys captured at index-write time narrow the changed ones.
func TestSearchDocumentsRepoFingerprint(t *testing.T) {
	a := ComputeSearchRepoFingerprint(3, "2026-11-19T00:00:00Z", []string{"doc-a", "doc-b", "doc-c"})
	b := ComputeSearchRepoFingerprint(3, "2026-11-19T00:00:00Z", []string{"doc-c", "doc-b", "doc-a"})
	if a != b {
		t.Fatalf("fingerprint order-dependent: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("fingerprint length = %d, want 64 hex chars", len(a))
	}
	moved := ComputeSearchRepoFingerprint(4, "2026-11-19T00:00:00Z", []string{"doc-a", "doc-b", "doc-c", "doc-d"})
	if moved == a {
		t.Fatal("fingerprint unchanged after document add, want move")
	}
	empty := ComputeSearchRepoFingerprint(0, "", nil)
	if empty == "" {
		t.Fatal("empty-set fingerprint is empty, want stable non-empty value")
	}
	if empty != ComputeSearchRepoFingerprint(0, "", nil) {
		t.Fatal("empty-set fingerprint unstable across calls")
	}
	signal := SearchDocumentsChangeSignal{
		RepoFingerprint: a,
		DirtyDocKeys:    []string{"doc-d"},
	}
	if !signal.NeedsReindex() {
		t.Fatal("NeedsReindex = false with dirty keys, want true")
	}
	clean := SearchDocumentsChangeSignal{RepoFingerprint: a, RecordedFingerprint: a}
	if clean.NeedsReindex() {
		t.Fatal("NeedsReindex = true on matching fingerprint with no dirty keys, want false")
	}
}
