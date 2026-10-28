// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/imageanalyzer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// directImageOutputFor runs the in-tree image analyzer with the admission
// claim identity so the host-admitted facts compare against in-tree truth.
func directImageOutputFor(t *testing.T, claim sdkcollector.Claim, acceptance, root string) []string {
	t.Helper()

	analyzer, err := imageanalyzer.NewAnalyzer(imageanalyzer.AnalyzerConfig{
		CollectorInstanceID: claim.InstanceID,
		Targets: []imageanalyzer.TargetConfig{{
			ScopeID:        claim.Scope.ID,
			RootFSPath:     root,
			SourceURI:      "oci://registry.example/admission@sha256:deadbeef",
			SourceRecordID: "sha256:deadbeef",
			ImageReference: "registry.example/admission:1.0",
			ImageDigest:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
		Now: time.Now().UTC,
	})
	if err != nil {
		t.Fatalf("NewAnalyzer() error = %v", err)
	}
	input := testClaimInput(t, claim, scannerworker.AnalyzerImageUnpacking, acceptance)
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	return wantIdentities(result.Output)
}

// buildComponentBinary compiles one scanner component binary by module
// package path so the test never depends on the working directory.
func buildComponentBinary(t *testing.T, pkg, name string) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, pkg)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s error = %v\n%s", pkg, err, out)
	}
	return bin
}

// admissionItem builds a claimed work item the host Source accepts, mirroring
// the parity harness: claimed status, matching instance, live lease.
func admissionItem(instanceID, scopeID, acceptanceUnitID string) workflow.WorkItem {
	now := time.Now().UTC()
	return workflow.WorkItem{
		WorkItemID:          "scanner-worker:admission:work-1",
		RunID:               "scanner-worker:admission-run-1",
		CollectorKind:       scope.CollectorScannerWorker,
		CollectorInstanceID: instanceID,
		SourceSystem:        string(scope.CollectorScannerWorker),
		ScopeID:             scopeID,
		AcceptanceUnitID:    acceptanceUnitID,
		SourceRunID:         "scanner-worker:generation-admission",
		GenerationID:        "scanner-worker:generation-admission",
		FairnessKey:         "scanner_worker:admission:image",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentClaimID:      "claim-admission",
		CurrentFencingToken: 13,
		CurrentOwnerID:      "scanner-worker-1",
		LeaseExpiresAt:      now.Add(5 * time.Minute),
		VisibleAt:           now.Add(-time.Minute),
		LastClaimedAt:       now,
		CreatedAt:           now.Add(-time.Hour),
		UpdatedAt:           now,
	}
}

// admitThroughHost runs one real component binary behind a real host Source
// and returns the admitted fact identities.
func admitThroughHost(t *testing.T, kind scannerworker.AnalyzerKind, bin string, item workflow.WorkItem, config map[string]any) []string {
	t.Helper()

	manifest, err := ManifestFor(kind)
	if err != nil {
		t.Fatalf("ManifestFor(%q) error = %v", kind, err)
	}
	source, err := extensionhost.NewSource(extensionhost.Config{
		Manifest:            manifest,
		CollectorInstanceID: item.CollectorInstanceID,
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-admission",
		Config:              config,
		Runner:              extensionhost.ProcessRunner{Command: bin},
		Grants:              RequiredGrants(manifest),
	})
	if err != nil {
		t.Fatalf("NewSource(%q) error = %v", kind, err)
	}
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed(%q) ok=%t error=%v, want enforced admission", kind, ok, err)
	}
	var admitted []string
	for envelope := range collected.Facts {
		admitted = append(admitted, envelope.FactKind+"\x00"+envelope.StableFactKey+
			"\x00"+envelope.ScopeID+"\x00"+envelope.GenerationID)
	}
	if collected.FactStreamErr != nil {
		if err := collected.FactStreamErr(); err != nil {
			t.Fatalf("NextClaimed(%q) FactStreamErr() = %v", kind, err)
		}
	}
	return admitted
}

func wantIdentities(output scannerworker.FactOutput) []string {
	var want []string
	for _, envelope := range output.Facts {
		want = append(want, envelope.FactKind+"\x00"+envelope.StableFactKey+
			"\x00"+envelope.ScopeID+"\x00"+envelope.GenerationID)
	}
	return want
}

// TestHostAdmitsRealImageBinary proves the full production path for the
// image component: the real host Source launches the real binary as a
// subprocess, reassembles its pages, and admits the identical fact set the
// in-tree analyzer produces on the same rootfs.
func TestHostAdmitsRealImageBinary(t *testing.T) {
	bin := buildComponentBinary(t, "github.com/eshu-hq/eshu/go/cmd/scanner-component-image", "scanner-component-image")

	scopeID := "image://registry.example/admission@sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	root := writeRootFS(t, 20)
	acceptance := "image:sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	item := admissionItem("scanner-admission", scopeID, acceptance)
	config := map[string]any{
		"targets": []any{map[string]any{
			"scope_id":         scopeID,
			"rootfs_path":      root,
			"source_uri":       "oci://registry.example/admission@sha256:deadbeef",
			"source_record_id": "sha256:deadbeef",
			"image_reference":  "registry.example/admission:1.0",
			"image_digest":     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
		"acceptance_unit_id": acceptance,
	}
	admitted := admitThroughHost(t, scannerworker.AnalyzerImageUnpacking, bin, item, config)

	claim := testClaim(IDImage, scopeID, item.GenerationID)
	claim.InstanceID = item.CollectorInstanceID
	claim.FencingToken = strconv.FormatInt(item.CurrentFencingToken, 10)
	claim.Attempt = item.AttemptCount
	claim.Deadline = item.LeaseExpiresAt
	want := directImageOutputFor(t, claim, acceptance, root)
	if len(admitted) != len(want) {
		t.Fatalf("admitted %d facts, want %d", len(admitted), len(want))
	}
	for i := range want {
		if admitted[i] != want[i] {
			t.Fatalf("admitted facts[%d]:\n got %q\nwant %q", i, admitted[i], want[i])
		}
	}
	t.Logf("image: host-admitted %d facts identical to in-tree output", len(admitted))
}

// terminalFailure reports whether the host ended the claim terminal (no
// retry) rather than with a retryable launch failure.
func terminalFailure(t *testing.T, err error) bool {
	t.Helper()

	var terminal interface{ TerminalFailure() bool }
	if !errors.As(err, &terminal) {
		t.Fatalf("host error %v carries no terminal verdict, want a terminal failure", err)
	}
	return terminal.TerminalFailure()
}

// TestHostAdmitsMultiPageBinaryLiveClock proves the production path for a
// multi-page component result with a real wall clock: the real host Source
// launches the real image binary (no frozen clock) at a small page bound,
// reassembles every page, and admits the identical fact set the in-tree
// analyzer produces. A per-invocation Generation.ObservedAt fails this with
// a terminal identity mismatch on page two.
func TestHostAdmitsMultiPageBinaryLiveClock(t *testing.T) {
	bin := buildComponentBinary(t, "github.com/eshu-hq/eshu/go/cmd/scanner-component-image", "scanner-component-image")

	scopeID := "image://registry.example/admission@sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	root := writeRootFS(t, 60)
	acceptance := "image:sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	target := map[string]any{
		"scope_id":         scopeID,
		"rootfs_path":      root,
		"source_uri":       "oci://registry.example/admission@sha256:deadbeef",
		"source_record_id": "sha256:deadbeef",
		"image_reference":  "registry.example/admission:1.0",
		"image_digest":     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	config := map[string]any{
		"targets":            []any{target},
		"acceptance_unit_id": acceptance,
		"page_bound_bytes":   8 * 1024,
	}

	claim := testClaim(IDImage, scopeID, "scanner-worker:generation-admission")
	claim.InstanceID = "scanner-admission"
	claim.FencingToken = "13"
	claim.Attempt = 1
	if _, pages := drivePages(t, IDImage, claim, config); pages < 2 {
		t.Fatalf("pages = %d, want multi-page output at the small test bound", pages)
	}

	item := admissionItem("scanner-admission", scopeID, acceptance)
	admitted := admitThroughHost(t, scannerworker.AnalyzerImageUnpacking, bin, item, config)

	wantClaim := testClaim(IDImage, scopeID, item.GenerationID)
	wantClaim.InstanceID = item.CollectorInstanceID
	wantClaim.FencingToken = strconv.FormatInt(item.CurrentFencingToken, 10)
	wantClaim.Attempt = item.AttemptCount
	wantClaim.Deadline = item.LeaseExpiresAt
	want := directImageOutputFor(t, wantClaim, acceptance, root)
	if len(admitted) != len(want) {
		t.Fatalf("admitted %d facts, want %d", len(admitted), len(want))
	}
	for i := range want {
		if admitted[i] != want[i] {
			t.Fatalf("admitted facts[%d]:\n got %q\nwant %q", i, admitted[i], want[i])
		}
	}
	t.Logf("image: host-admitted %d multi-page facts identical to in-tree output with a live clock", len(admitted))
}

// TestHostEndsBadConfigTerminal proves a deterministic operator config
// defect ends the claim terminal through the real host instead of retrying
// a launch failure until the lease expires.
func TestHostEndsBadConfigTerminal(t *testing.T) {
	bin := buildComponentBinary(t, "github.com/eshu-hq/eshu/go/cmd/scanner-component-image", "scanner-component-image")

	scopeID := "image://registry.example/admission@sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	root := writeRootFS(t, 1)
	acceptance := "image:sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	item := admissionItem("scanner-admission", scopeID, acceptance)
	config := map[string]any{
		"targets": []any{map[string]any{
			"scope_id":         scopeID,
			"rootfs_path":      root,
			"source_uri":       "oci://registry.example/admission@sha256:deadbeef",
			"source_record_id": "sha256:deadbeef",
			"image_reference":  "registry.example/admission:1.0",
			"image_digest":     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
		"acceptance_unit_id": acceptance,
		"page_bound_bytes":   -1,
	}

	manifest, err := ManifestFor(scannerworker.AnalyzerImageUnpacking)
	if err != nil {
		t.Fatalf("ManifestFor() error = %v", err)
	}
	source, err := extensionhost.NewSource(extensionhost.Config{
		Manifest:            manifest,
		CollectorInstanceID: item.CollectorInstanceID,
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-admission",
		Config:              config,
		Runner:              extensionhost.ProcessRunner{Command: bin},
		Grants:              RequiredGrants(manifest),
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	_, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed(bad config) error = nil, want a terminal failure")
	}
	if ok {
		t.Fatal("NextClaimed(bad config) ok = true, want no admission")
	}
	if !terminalFailure(t, err) {
		t.Fatalf("NextClaimed(bad config) error = %v, want terminal, not a retryable launch failure", err)
	}
	if !strings.Contains(err.Error(), "component_bad_config") {
		t.Fatalf("NextClaimed(bad config) error = %v, want the bounded component class", err)
	}
}

// TestHostAdmitsRealOSRuntimeBinary proves the same production path for the
// osruntime component.
func TestHostAdmitsRealOSRuntimeBinary(t *testing.T) {
	bin := buildComponentBinary(t, "github.com/eshu-hq/eshu/go/cmd/scanner-component-osruntime", "scanner-component-osruntime")

	scopeID := "image://registry.example/admission@sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	root := writeRootFS(t, 20)
	acceptance := "image:sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	item := admissionItem("scanner-admission", scopeID, acceptance)
	config := map[string]any{
		"targets": []any{map[string]any{
			"scope_id":         scopeID,
			"rootfs_path":      root,
			"source_uri":       "oci://registry.example/admission@sha256:deadbeef",
			"source_record_id": "sha256:deadbeef",
		}},
		"acceptance_unit_id": acceptance,
	}
	admitted := admitThroughHost(t, scannerworker.AnalyzerOSPackageExtraction, bin, item, config)

	claim := testClaim(IDOSRuntime, scopeID, item.GenerationID)
	claim.InstanceID = item.CollectorInstanceID
	claim.FencingToken = strconv.FormatInt(item.CurrentFencingToken, 10)
	claim.Attempt = item.AttemptCount
	claim.Deadline = item.LeaseExpiresAt
	want := wantIdentities(directOSRuntimeOutput(t, claim, root))
	if len(admitted) != len(want) {
		t.Fatalf("admitted %d facts, want %d", len(admitted), len(want))
	}
	for i := range want {
		if admitted[i] != want[i] {
			t.Fatalf("admitted facts[%d]:\n got %q\nwant %q", i, admitted[i], want[i])
		}
	}
	t.Logf("osruntime: host-admitted %d facts identical to in-tree output", len(admitted))
}

// TestHostAdmitsRealSBOMBinary proves the same production path for the sbom
// component over repository lockfile evidence.
func TestHostAdmitsRealSBOMBinary(t *testing.T) {
	bin := buildComponentBinary(t, "github.com/eshu-hq/eshu/go/cmd/scanner-component-sbom", "scanner-component-sbom")

	scopeID := "scanner-worker://repository/admission"
	root := writeSBOMRepo(t)
	acceptance := "repository:admission"
	item := admissionItem("scanner-admission", scopeID, acceptance)
	config := map[string]any{
		"targets": []any{map[string]any{
			"scope_id":       scopeID,
			"root_path":      root,
			"subject_digest": "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
		}},
		"acceptance_unit_id": acceptance,
	}
	admitted := admitThroughHost(t, scannerworker.AnalyzerSBOMGeneration, bin, item, config)

	claim := testClaim(IDSBOM, scopeID, item.GenerationID)
	claim.InstanceID = item.CollectorInstanceID
	claim.FencingToken = strconv.FormatInt(item.CurrentFencingToken, 10)
	claim.Attempt = item.AttemptCount
	claim.Deadline = item.LeaseExpiresAt
	claim.Scope.Kind = "repository"
	want := wantIdentities(directSBOMOutput(t, claim, root))
	if len(admitted) != len(want) {
		t.Fatalf("admitted %d facts, want %d", len(admitted), len(want))
	}
	for i := range want {
		if admitted[i] != want[i] {
			t.Fatalf("admitted facts[%d]:\n got %q\nwant %q", i, admitted[i], want[i])
		}
	}
	t.Logf("sbom: host-admitted %d facts identical to in-tree output", len(admitted))
}
