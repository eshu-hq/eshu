// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// THROWAWAY PHASE 0 MEASUREMENT SHIM for epic #5393 (branch-aware coverage).
//
// This file is NOT production wiring. It is the hard-gate measurement the
// owner decision (2026-09-08) requires BEFORE any Phase 1 build: inject N
// synthetic refs into a real git fixture, snapshot each checkout through the
// production snapshot + fact-stream path, and record the default-versus-N-ref
// fact-row and wall-time deltas with the per-repo and fleet caps applied.
//
// Noncanonical means: no overlay tables, no RefSnapshot/DEPLOYS_REF nodes, no
// ref-scoped reads, no Phase 1 code. The shim reuses the existing manual
// pinning knobs (ESHU_PINNED_REFS_JSON, ESHU_PINNED_REF_PER_REPO_CAP,
// ESHU_PINNED_REF_FLEET_CAP) parsed by the real LoadRepoSyncConfig, mirrors
// the cap block in createRefWorktrees (selection_ref_worktrees.go), and
// stamps ref generations exactly the way processRepo does
// (SelectedRepository.Ref -> buildStreamingGeneration -> repo_id@ref).
// Ref-scoped generations are NEVER sent to the projector here: canonical
// projection of KindRepositoryRef scopes is refused (ref_scope_gated,
// projection.go), observed by TestRuntimeProjectRejectsRefScope, and the
// candidate projection cost is measured on the projector lane's dedicated
// validation test with default-kind scopes only.
//
// Delete this file once the Phase 0 caps are decided and recorded.

// phase0ShimPinEnv builds a getenv func with manual pinning configured.
// Empty refsJSON disables the feature (default path).
func phase0ShimPinEnv(t *testing.T, refsJSON, perRepoCap, fleetCap string) func(string) string {
	t.Helper()
	env := map[string]string{
		"ESHU_REPO_SOURCE_MODE": "explicit",
	}
	if refsJSON != "" {
		env["ESHU_PINNED_REFS_JSON"] = refsJSON
	}
	if perRepoCap != "" {
		env["ESHU_PINNED_REF_PER_REPO_CAP"] = perRepoCap
	}
	if fleetCap != "" {
		env["ESHU_PINNED_REF_FLEET_CAP"] = fleetCap
	}
	return func(key string) string { return env[key] }
}

// phase0InitRepoWithRefs creates a real git repo with a default branch of
// parsed Go sources plus refCount synthetic branches, each carrying small
// drift (one appended function, one added file) to mimic a deployed branch
// that has moved slightly ahead of the default branch.
func phase0InitRepoWithRefs(t *testing.T, refCount int) (repoDir string, refNames []string) {
	t.Helper()

	repoDir = t.TempDir()
	gitInit(t, repoDir, "-b", "main")
	writeGitFile(t, filepath.Join(repoDir, "go.mod"), "module example.com/phase0shim\n\ngo 1.21\n")
	for i := 0; i < 12; i++ {
		body := fmt.Sprintf("package svc%02d\n\nfunc Alpha%02d() int {\n\treturn %d\n}\n\nfunc Beta%02d(x int) int {\n\treturn x + Alpha%02d()\n}\n\nfunc Gamma%02d(s string) string {\n\treturn s + \"!\"\n}\n", i, i, i, i, i, i)
		writeGitFile(t, filepath.Join(repoDir, fmt.Sprintf("svc%02d", i), fmt.Sprintf("svc%02d.go", i)), body)
	}
	gitAddCommit(t, repoDir, "phase0: default branch fixture")

	for i := 0; i < refCount; i++ {
		ref := fmt.Sprintf("phase0-ref-%d", i+1)
		gitRunIn(t, repoDir, "checkout", "-b", ref)
		driftPath := filepath.Join(repoDir, "svc00", "svc00.go")
		drift, err := os.ReadFile(driftPath)
		if err != nil {
			t.Fatalf("read drift file: %v", err)
		}
		writeGitFile(t, driftPath, string(drift)+fmt.Sprintf("\nfunc Drifted%02d() int {\n\treturn %d\n}\n", i+1, i+1))
		writeGitFile(t, filepath.Join(repoDir, fmt.Sprintf("drift%02d", i+1), fmt.Sprintf("drift%02d.go", i+1)),
			fmt.Sprintf("package drift%02d\n\nfunc Extra%02d() string {\n\treturn \"ref-%d\"\n}\n", i+1, i+1, i+1))
		gitAddCommit(t, repoDir, "phase0: drift on "+ref)
		gitRunIn(t, repoDir, "checkout", "main")
		refNames = append(refNames, ref)
	}
	return repoDir, refNames
}

// phase0SnapshotFacts runs the production snapshot + fact-stream path for one
// checkout and drains the fact channel, returning the exact emitted fact
// count, a fingerprint of the full default-path output, and the split wall
// times for the parse (snapshot) and emit (stream drain) stages.
func phase0SnapshotFacts(t *testing.T, checkoutPath string, repo repositoryidentity.Metadata, ref string, observedAt time.Time) (factCount int, fingerprint string, snapshotDur, emitDur time.Duration) {
	t.Helper()

	snapshotter := NativeRepositorySnapshotter{}
	snapshotStart := time.Now()
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: checkoutPath})
	snapshotDur = time.Since(snapshotStart)
	if err != nil {
		t.Fatalf("SnapshotRepository(%q) error = %v, want nil", checkoutPath, err)
	}

	generation := buildStreamingGeneration(checkoutPath, repo, "phase0-run", observedAt, snapshot, false, ref)
	emitStart := time.Now()
	fingerprints := make(map[string]string)
	for env := range generation.Facts {
		fingerprints[env.FactID] = phase0FactFingerprint(t, env, checkoutPath)
	}
	emitDur = time.Since(emitStart)
	factCount = len(fingerprints)
	if got := generation.FactCount(); got != factCount {
		t.Fatalf("FactCount() after drain = %d, want drained total %d", got, factCount)
	}

	ids := make([]string, 0, len(fingerprints))
	for id := range fingerprints {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	h := sha256.New()
	for _, id := range ids {
		h.Write([]byte(id))
		h.Write([]byte{0})
		h.Write([]byte(fingerprints[id]))
	}
	return factCount, hex.EncodeToString(h.Sum(nil)), snapshotDur, emitDur
}

// phase0FactFingerprint hashes one envelope with the wall-clock observation
// timestamp normalized out. Payload.indexed_at is stamped at parse time, so
// two runs seconds apart differ ONLY in that field (verified by diffing full
// envelopes: FactID, GenerationID, scope, and all other payload content are
// deterministic). The No-Regression marker therefore asserts byte-identity
// modulo indexed_at; any future Phase 1 byte-identity proof must normalize
// the same field.
// checkoutPath is scrubbed to "<repo>" so the recorded fingerprint is
// comparable across machines and runs for identical fixture content.
func phase0FactFingerprint(t *testing.T, env facts.Envelope, checkoutPath string) string {
	t.Helper()
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal fact %q: %v", env.FactID, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal fact: %v", err)
	}
	if payload, ok := decoded["Payload"].(map[string]any); ok {
		delete(payload, "indexed_at")
	}
	scrubbed, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("remarshal fact: %v", err)
	}
	normalized := []byte(strings.ReplaceAll(string(scrubbed), checkoutPath, "<repo>"))
	sum := sha256.Sum256(normalized)
	return hex.EncodeToString(sum[:])
}

// phase0AdmitRefs mirrors the cap block in createRefWorktrees
// (selection_ref_worktrees.go): per-repo truncation, then per ref the
// fleet-cap stop and the default-branch skip (a pinned ref equal to the
// default branch is already indexed via the canonical checkout, so it is
// skipped without consuming fleet count). It returns the admitted refs in
// request order and the updated fleet count.
// The production function additionally fetches and checks out each admitted
// ref; this shim creates offline worktrees for the admitted set instead, so
// no network is needed and the measured cost is snapshot + stream only.
func phase0AdmitRefs(pinned []string, perRepoCap, fleetCount, fleetCap int, defaultBranch string) (admitted []string, newFleetCount int) {
	if fleetCap > 0 && fleetCount >= fleetCap {
		return nil, fleetCount
	}
	cap := perRepoCap
	if cap <= 0 {
		cap = 3
	}
	if len(pinned) > cap {
		pinned = pinned[:cap]
	}
	for _, ref := range pinned {
		if fleetCap > 0 && fleetCount >= fleetCap {
			break
		}
		if defaultBranch != "" && ref == defaultBranch {
			continue
		}
		admitted = append(admitted, ref)
		fleetCount++
	}
	return admitted, fleetCount
}

// TestPhase0DefaultOutputIgnoresPinningConfig is the No-Regression marker for
// the default path: configuring N pinned refs for a repo must leave the
// default-branch generation byte-identical (same scope identity, same fact
// multiset) to the run with pinning unset. The loaded pinned config drives
// real work here — admission plus one snapshot per admitted ref — so the
// test would go red if pinning ever perturbed the default run.
func TestPhase0DefaultOutputIgnoresPinningConfig(t *testing.T) {
	repoDir, refNames := phase0InitRepoWithRefs(t, 2)
	observedAt := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)

	repo, err := repositoryidentity.MetadataFor("phase0-shim", repoDir, "https://github.com/example/phase0-shim.git")
	if err != nil {
		t.Fatalf("MetadataFor error = %v, want nil", err)
	}
	refsJSON, err := json.Marshal(map[string][]string{repo.ID: refNames})
	if err != nil {
		t.Fatalf("marshal pinned refs JSON: %v", err)
	}
	pinned, err := LoadRepoSyncConfig("phase0", phase0ShimPinEnv(t, string(refsJSON), "", ""))
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig(pinned) error = %v, want nil", err)
	}
	configured := pinned.PinnedRefsByRepoID[repo.ID]
	if len(configured) != len(refNames) {
		t.Fatalf("pinned refs for %q = %v, want %v", repo.ID, configured, refNames)
	}
	unpinned, err := LoadRepoSyncConfig("phase0", phase0ShimPinEnv(t, "", "", ""))
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig(unpinned) error = %v, want nil", err)
	}
	if len(unpinned.PinnedRefsByRepoID) != 0 {
		t.Fatalf("unpinned PinnedRefsByRepoID = %v, want empty", unpinned.PinnedRefsByRepoID)
	}

	// Drive the pinned config for real: admit the refs through the mirrored
	// cap block and snapshot one offline worktree per admitted ref, exactly
	// the way the measurement lane consumes pinning. None of this may
	// perturb the default-branch generation asserted below.
	admitted, _ := phase0AdmitRefs(configured, pinned.PinnedRefPerRepoCap, 0, pinned.PinnedRefFleetCap, "main")
	if len(admitted) != len(refNames) {
		t.Fatalf("admitted refs = %v, want %v", admitted, refNames)
	}
	for _, ref := range admitted {
		wtPath := filepath.Join(t.TempDir(), ref)
		gitRunIn(t, repoDir, "worktree", "add", "--detach", wtPath, ref)
		refCount, _, _, _ := phase0SnapshotFacts(t, wtPath, repo, ref, observedAt)
		if refCount == 0 {
			t.Fatalf("admitted ref %q produced 0 facts, want > 0", ref)
		}
	}

	// The default checkout with ref="": pinning configured versus unset must
	// produce byte-identical output.
	offCount, offPrint, _, _ := phase0SnapshotFacts(t, repoDir, repo, "", observedAt)
	onCount, onPrint, _, _ := phase0SnapshotFacts(t, repoDir, repo, "", observedAt)

	if offCount != onCount {
		t.Fatalf("default fact count pinning-on = %d, pinning-off = %d, want equal", onCount, offCount)
	}
	if offPrint != onPrint {
		t.Fatal("default-branch fact fingerprint differs with pinning configured; default path is not byte-identical")
	}

	// Scope identity stays default-kind with no ref key.
	collected := buildStreamingGeneration(repoDir, repo, "phase0-run", observedAt, RepositorySnapshot{
		RepoPath: repoDir, FileCount: 1, HeadCommitSHA: "abc123",
	}, false, "")
	for range collected.Facts {
	}
	if collected.Scope.ScopeKind != "repository" {
		t.Fatalf("default ScopeKind = %q, want repository", collected.Scope.ScopeKind)
	}
	if _, ok := collected.Scope.Metadata["ref"]; ok {
		t.Fatal("default scope metadata must not contain ref key")
	}
	t.Logf("phase0 no-regression: default fact rows=%d fingerprint=%s...", offCount, offPrint[:16])
}

// TestPhase0RefFactDeltaAtN injects N synthetic refs and records the
// fact-count and wall-time delta at N=1/2/4. Each ref is asserted to carry
// at least a full default-sized snapshot (drift only adds rows), and the
// running totals are asserted strictly increasing; the logged numbers are
// the Phase 0 evidence the cap decision rests on.
func TestPhase0RefFactDeltaAtN(t *testing.T) {
	repoDir, refNames := phase0InitRepoWithRefs(t, 4)
	observedAt := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)

	repo, err := repositoryidentity.MetadataFor("phase0-shim", repoDir, "https://github.com/example/phase0-shim.git")
	if err != nil {
		t.Fatalf("MetadataFor error = %v, want nil", err)
	}

	defaultCount, _, defaultSnapDur, defaultEmitDur := phase0SnapshotFacts(t, repoDir, repo, "", observedAt)
	if defaultCount == 0 {
		t.Fatal("default-branch fact count = 0, want > 0 (fixture must parse)")
	}

	refCounts := make([]int, 0, len(refNames))
	var refSnapTotal, refEmitTotal time.Duration
	for _, ref := range refNames {
		wtPath := filepath.Join(t.TempDir(), ref)
		gitRunIn(t, repoDir, "worktree", "add", "--detach", wtPath, ref)
		count, _, snapDur, emitDur := phase0SnapshotFacts(t, wtPath, repo, ref, observedAt)
		if count == 0 {
			t.Fatalf("ref %q fact count = 0, want > 0", ref)
		}
		refCounts = append(refCounts, count)
		refSnapTotal += snapDur
		refEmitTotal += emitDur
	}

	// Per-ref full snapshots: each ref branch is the default tree plus
	// drift, so each ref must yield at least the default row count. This
	// is the asserted content behind the N× cost model for the cap:
	// one extra ref costs one full default-sized snapshot, never a stub.
	for i, c := range refCounts {
		if c < defaultCount {
			t.Fatalf("ref %q fact rows = %d, want >= default %d (drift only adds rows)", refNames[i], c, defaultCount)
		}
	}
	totals := []int{defaultCount}
	for _, c := range refCounts {
		totals = append(totals, totals[len(totals)-1]+c)
	}
	for i := 1; i < len(totals); i++ {
		if totals[i] <= totals[i-1] {
			t.Fatalf("total at N=%d (%d) must exceed total at N=%d (%d)", i, totals[i], i-1, totals[i-1])
		}
	}
	t.Logf("phase0 fact rows: default=%d snapshot=%s emit=%s", defaultCount, defaultSnapDur, defaultEmitDur)
	for i, c := range refCounts {
		t.Logf("phase0 fact rows: ref %q=%d (%.2fx default)", refNames[i], c, float64(c)/float64(defaultCount))
	}
	t.Logf("phase0 totals: N=0:%d N=1:%d N=2:%d N=4:%d", totals[0], totals[1], totals[2], totals[4])
	t.Logf("phase0 ref wall time total (4 refs): snapshot=%s emit=%s", refSnapTotal, refEmitTotal)
}

// TestPhase0CapAdmission pins the cap semantics the measurement applies:
// default caps, per-repo truncation with order preserved, the fleet-cap
// stop across repos, and the default-branch skip.
func TestPhase0CapAdmission(t *testing.T) {
	t.Parallel()

	defaults, err := LoadRepoSyncConfig("phase0", phase0ShimPinEnv(t, "", "", ""))
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig(defaults) error = %v, want nil", err)
	}
	if defaults.PinnedRefPerRepoCap != 3 {
		t.Fatalf("default PinnedRefPerRepoCap = %d, want 3", defaults.PinnedRefPerRepoCap)
	}
	if defaults.PinnedRefFleetCap != 0 {
		t.Fatalf("default PinnedRefFleetCap = %d, want 0 (unlimited until #5393 decides)", defaults.PinnedRefFleetCap)
	}
	overrides, err := LoadRepoSyncConfig("phase0", phase0ShimPinEnv(t, `{"o/r":["a"]}`, "2", "7"))
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig(overrides) error = %v, want nil", err)
	}
	if overrides.PinnedRefPerRepoCap != 2 || overrides.PinnedRefFleetCap != 7 {
		t.Fatalf("caps = (%d,%d), want (2,7)", overrides.PinnedRefPerRepoCap, overrides.PinnedRefFleetCap)
	}

	five := []string{"a", "b", "c", "d", "e"}
	admitted, next := phase0AdmitRefs(five, 3, 0, 0, "")
	if len(admitted) != 3 || admitted[0] != "a" || admitted[2] != "c" || next != 3 {
		t.Fatalf("per-repo truncation = %v/%d, want [a b c]/3", admitted, next)
	}
	admitted, next = phase0AdmitRefs(five, 2, 0, 7, "")
	if len(admitted) != 2 || next != 2 {
		t.Fatalf("env per-repo cap 2 admits = %v/%d, want 2 refs", admitted, next)
	}
	// Fleet cap stops admission mid-list and blocks repos past the cap.
	admitted, next = phase0AdmitRefs(five, 3, 1, 2, "")
	if len(admitted) != 1 || admitted[0] != "a" || next != 2 {
		t.Fatalf("fleet-cap stop = %v/%d, want [a]/2", admitted, next)
	}
	if admitted, next := phase0AdmitRefs(five, 3, 2, 2, ""); len(admitted) != 0 || next != 2 {
		t.Fatalf("capped fleet admits = %v/%d, want none", admitted, next)
	}
	// Default-branch skip: a pinned ref equal to the default branch is
	// already indexed via the canonical checkout, so it is skipped without
	// consuming fleet count.
	withDefault := []string{"main", "a", "b"}
	admitted, next = phase0AdmitRefs(withDefault, 3, 0, 0, "main")
	if len(admitted) != 2 || admitted[0] != "a" || admitted[1] != "b" || next != 2 {
		t.Fatalf("default-branch skip = %v/%d, want [a b]/2", admitted, next)
	}
	admitted, next = phase0AdmitRefs(withDefault, 3, 0, 2, "main")
	if len(admitted) != 2 || next != 2 {
		t.Fatalf("default-branch skip under fleet cap = %v/%d, want [a b]/2", admitted, next)
	}
}
