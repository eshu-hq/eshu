// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability/osruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/imageanalyzer"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/sbomgenerator"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testClock() time.Time {
	return time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
}

func testClaim(componentID, scopeID, generationID string) sdkcollector.Claim {
	return sdkcollector.Claim{
		ComponentID:   componentID,
		InstanceID:    "scanner-component-test",
		CollectorKind: "scanner_worker",
		SourceSystem:  "scanner_worker",
		Scope:         sdkcollector.Scope{ID: scopeID, Kind: "repository"},
		SourceRunID:   "scanner-worker:test-run",
		GenerationID:  generationID,
		WorkItemID:    "scanner-worker:test-work-1",
		FencingToken:  "13",
		Attempt:       1,
		Deadline:      testClock().Add(2 * time.Minute),
		ConfigHandle:  "cfg-test",
	}
}

func testRequest(t *testing.T, claim sdkcollector.Claim, config map[string]any) []byte {
	t.Helper()

	payload, err := json.Marshal(extensionhost.Request{
		ProtocolVersion: "collector-sdk/v1alpha3",
		Claim:           claim,
		Contract:        sdkcollector.Contract{ProtocolVersion: "collector-sdk/v1alpha3"},
		Config:          config,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return payload
}

// wirePage decodes one runner stdout document: the SDK result plus the
// v1alpha3 page envelope the pinned SDK type predates.
type wirePage struct {
	sdkcollector.Result
	Page *pageEnvelope `json:"page,omitempty"`
}

func decodePage(t *testing.T, raw []byte) wirePage {
	t.Helper()

	var page wirePage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("decode page trailer: %v, want exactly one JSON document", err)
	}
	return page
}

func writeRootFS(t *testing.T, packages int) string {
	t.Helper()

	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	write("etc/os-release", "ID=alpine\nVERSION_ID=3.19.1\n")
	write("etc/apk/repositories", "https://dl-cdn.alpinelinux.org/alpine/v3.19/main\n")
	var db strings.Builder
	for i := 0; i < packages; i++ {
		fmt.Fprintf(&db, "P:pkg-%04d\nV:3.1.4-r5\nA:x86_64\no:pkg-%04d\n\n", i, i)
	}
	write("lib/apk/db/installed", db.String())
	return root
}

// runRawWithClock invokes the runner for a single call with an explicit
// observation clock and returns the decoded page with the stderr record,
// without asserting the result state.
func runRawWithClock(t *testing.T, componentID string, stdin []byte, clock func() time.Time) (wirePage, string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	runner := Runner{
		ComponentID: componentID,
		Stdin:       bytes.NewReader(stdin),
		Stdout:      &stdout,
		Stderr:      &stderr,
		Clock:       clock,
	}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, stderr = %q", err, stderr.String())
	}
	if stderr.String() == "" {
		t.Fatal("Run() wrote no stderr log line, want at least a completion record")
	}
	return decodePage(t, stdout.Bytes()), stderr.String()
}

// runRaw invokes the runner for a single call and returns the decoded page
// with the stderr record, without asserting the result state.
func runRaw(t *testing.T, componentID string, stdin []byte) (wirePage, string) {
	t.Helper()

	return runRawWithClock(t, componentID, stdin, testClock)
}

// runTerminal invokes the runner for a single call that must end the claim
// terminal: a Go error here would become a retryable host launch failure.
func runTerminal(t *testing.T, componentID string, stdin []byte, class string) wirePage {
	t.Helper()

	page, _ := runRaw(t, componentID, stdin)
	if page.State != sdkcollector.ResultTerminal {
		t.Fatalf("state = %q, want terminal", page.State)
	}
	if len(page.Statuses) != 1 || page.Statuses[0].Class != sdkcollector.StatusFailure {
		t.Fatalf("statuses = %+v, want one failure status", page.Statuses)
	}
	if page.Statuses[0].FailureClass != class {
		t.Fatalf("failure_class = %q, want %q", page.Statuses[0].FailureClass, class)
	}
	if len(page.Facts) != 0 {
		t.Fatalf("facts = %d, want none on the terminal result", len(page.Facts))
	}
	return page
}

// runOne invokes the runner for a single call and returns the decoded page.
func runOne(t *testing.T, componentID string, stdin []byte) wirePage {
	t.Helper()

	page, _ := runRaw(t, componentID, stdin)
	return page
}

// TestRunnerRejectsForeignProtocol proves the binary only speaks v1alpha3:
// older hosts fail closed instead of receiving paged output they decode
// strictly.
func TestRunnerRejectsForeignProtocol(t *testing.T) {
	t.Parallel()

	claim := testClaim(IDImage, "image://registry.example/t@sha256:deadbeef", "gen-1")
	raw, err := json.Marshal(extensionhost.Request{
		ProtocolVersion: "collector-sdk/v1alpha1",
		Claim:           claim,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var stdout, stderr bytes.Buffer
	runner := Runner{ComponentID: IDImage, Stdin: bytes.NewReader(raw), Stdout: &stdout, Stderr: &stderr}
	if err := runner.Run(context.Background()); err == nil {
		t.Fatal("Run(v1alpha1) error = nil, want protocol rejection")
	} else if !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("Run(v1alpha1) error = %v, want protocol naming", err)
	}
}

// TestRunnerRejectsAnalyzerMismatch proves a binary never executes another
// analyzer: a claim naming a different component ends the claim terminal
// with a bounded class instead of a retryable launch failure.
func TestRunnerRejectsAnalyzerMismatch(t *testing.T) {
	t.Parallel()

	claim := testClaim(IDSBOM, "scanner-worker://repository/r", "gen-1")
	raw := testRequest(t, claim, map[string]any{"targets": []any{}})
	runTerminal(t, IDImage, raw, "component_claim_mismatch")
}

// TestRunnerForgedCursorEmitsTerminal proves a forged resume cursor ends the
// claim terminal with a bounded class instead of a retryable launch
// failure: the defect is deterministic, so retry cannot fix it.
func TestRunnerForgedCursorEmitsTerminal(t *testing.T) {
	t.Parallel()

	claim := testClaim(IDImage, "image://registry.example/t@sha256:deadbeef", "gen-1")
	raw, err := json.Marshal(struct {
		extensionhost.Request
		Continuation *continuation `json:"continuation,omitempty"`
	}{
		Request: extensionhost.Request{
			ProtocolVersion: "collector-sdk/v1alpha3",
			Claim:           claim,
			Config:          map[string]any{"targets": []any{}},
		},
		Continuation: &continuation{Cursor: "forged-cursor"},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	runTerminal(t, IDImage, raw, "component_bad_cursor")
}

// TestRunnerBadConfigEmitsTerminal proves a deterministic operator config
// defect ends the claim terminal with a bounded class: retrying a bad
// activation until the lease expires helps nobody.
func TestRunnerBadConfigEmitsTerminal(t *testing.T) {
	t.Parallel()

	claim := testClaim(IDImage, "image://registry.example/t@sha256:deadbeef", "gen-1")
	raw := testRequest(t, claim, map[string]any{
		"targets":            []any{map[string]any{}},
		"acceptance_unit_id": "image:sha256:deadbeef",
		"page_bound_bytes":   -1,
	})
	page := runTerminal(t, IDImage, raw, "component_bad_config")
	if page.Generation.ID != claim.GenerationID {
		t.Fatalf("generation = %q, want the claim generation echoed back", page.Generation.ID)
	}
}

// TestRunnerMalformedClaimEmitsTerminal proves a malformed claim identity
// ends the claim terminal instead of retrying a defect the host will hand
// back unchanged on every attempt.
func TestRunnerMalformedClaimEmitsTerminal(t *testing.T) {
	t.Parallel()

	claim := testClaim(IDImage, "image://registry.example/t@sha256:deadbeef", "gen-1")
	claim.WorkItemID = "  "
	raw := testRequest(t, claim, map[string]any{
		"targets":            []any{map[string]any{}},
		"acceptance_unit_id": "image:sha256:deadbeef",
	})
	runTerminal(t, IDImage, raw, "component_bad_claim")
}

// TestRunnerPagesShareClaimStableObservedAtLiveClock proves every page of
// one claim carries the same Generation.ObservedAt even when the wall clock
// advances between the stateless re-invocations: the host pins the full
// generation across pages and ends the claim terminal on drift.
func TestRunnerPagesShareClaimStableObservedAtLiveClock(t *testing.T) {
	t.Parallel()

	scopeID := "image://registry.example/t@sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	root := writeRootFS(t, 60)
	target := map[string]any{
		"scope_id":         scopeID,
		"rootfs_path":      root,
		"source_uri":       "oci://registry.example/t@sha256:deadbeef",
		"source_record_id": "sha256:deadbeef",
		"image_reference":  "registry.example/t:1.0",
		"image_digest":     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	claim := testClaim(IDImage, scopeID, "scanner-worker:generation-test")
	config := map[string]any{
		"targets":            []any{target},
		"acceptance_unit_id": "image:sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"page_bound_bytes":   8 * 1024,
	}

	tick := testClock()
	clock := func() time.Time {
		tick = tick.Add(2 * time.Second)
		return tick
	}
	var stamps []time.Time
	var cursor *continuation
	pages := 0
	for {
		raw := testRequestWithContinuation(t, claim, config, cursor)
		page, _ := runRawWithClock(t, IDImage, raw, clock)
		if page.State != sdkcollector.ResultComplete {
			t.Fatalf("page state = %q, want complete", page.State)
		}
		if page.Generation.ObservedAt.IsZero() {
			t.Fatalf("page %d has zero ObservedAt, want the claim observation time", pages)
		}
		stamps = append(stamps, page.Generation.ObservedAt)
		pages++
		if !page.Page.More {
			break
		}
		cursor = &continuation{Cursor: page.Page.Cursor}
		if pages > 64 {
			t.Fatal("paging did not terminate within 64 pages")
		}
	}
	if pages < 2 {
		t.Fatalf("pages = %d, want multi-page output at the small test bound", pages)
	}
	for i, stamp := range stamps {
		if !stamp.Equal(stamps[0]) {
			t.Fatalf("page %d ObservedAt %s drifts from page 0 %s across live-clock calls",
				i, stamp.Format(time.RFC3339Nano), stamps[0].Format(time.RFC3339Nano))
		}
	}
}

// TestRunnerImagePagesMatchInTreeOutput drives the image runner through
// every page at a small bound and proves the reassembled facts carry the
// identical kinds, stable keys, generations, and scopes as the direct
// in-tree analyzer output on the same rootfs.
func TestRunnerImagePagesMatchInTreeOutput(t *testing.T) {
	t.Parallel()

	scopeID := "image://registry.example/t@sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	root := writeRootFS(t, 60)
	target := map[string]any{
		"scope_id":         scopeID,
		"rootfs_path":      root,
		"source_uri":       "oci://registry.example/t@sha256:deadbeef",
		"source_record_id": "sha256:deadbeef",
		"image_reference":  "registry.example/t:1.0",
		"image_digest":     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	claim := testClaim(IDImage, scopeID, "scanner-worker:generation-test")
	config := map[string]any{
		"targets":            []any{target},
		"acceptance_unit_id": "image:sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"page_bound_bytes":   8 * 1024,
	}

	var reassembled []sdkcollector.Fact
	var cursor *continuation
	pages := 0
	for {
		raw := testRequestWithContinuation(t, claim, config, cursor)
		page := runOne(t, IDImage, raw)
		if page.ProtocolVersion != "collector-sdk/v1alpha3" {
			t.Fatalf("page protocol = %q, want v1alpha3", page.ProtocolVersion)
		}
		if page.State != sdkcollector.ResultComplete {
			t.Fatalf("page state = %q, want complete", page.State)
		}
		if page.Page == nil {
			t.Fatal("page envelope missing, want v1alpha3 continuation")
		}
		if page.Page.PageIndex != pages {
			t.Fatalf("page_index = %d, want %d", page.Page.PageIndex, pages)
		}
		reassembled = append(reassembled, page.Facts...)
		pages++
		if !page.Page.More {
			if page.Page.Cursor != "" {
				t.Fatalf("final cursor = %q, want empty", page.Page.Cursor)
			}
			break
		}
		cursor = &continuation{Cursor: page.Page.Cursor}
		if pages > 64 {
			t.Fatal("paging did not terminate within 64 pages")
		}
	}
	if pages < 2 {
		t.Fatalf("pages = %d, want multi-page output at the small test bound", pages)
	}

	want := directImageOutput(t, claim, root)
	if len(reassembled) != len(want.Facts) {
		t.Fatalf("reassembled %d facts, want %d", len(reassembled), len(want.Facts))
	}
	for i, envelope := range want.Facts {
		got := reassembled[i]
		if got.Kind != envelope.FactKind || got.StableKey != envelope.StableFactKey ||
			got.SourceRef.GenerationID != envelope.GenerationID || got.SourceRef.ScopeID != envelope.SourceRef.ScopeID {
			t.Fatalf("facts[%d] identity changed: got %+v", i, got.SourceRef)
		}
	}
}

// testClaimInput builds the in-tree analyzer input for the same claim the
// runner receives, through the same TargetScopeForComponent derivation the
// runner uses. The parity comparison below then proves the runner's
// end-to-end derivation (not the helper) matches in-tree truth.
func testClaimInput(t *testing.T, claim sdkcollector.Claim, kind scannerworker.AnalyzerKind, acceptanceUnitID string) scannerworker.ClaimInput {
	t.Helper()

	target, err := scannerworker.TargetScopeForComponent(claim.Scope.ID, acceptanceUnitID, claim.SourceRunID, claim.GenerationID)
	if err != nil {
		t.Fatalf("TargetScopeForComponent() error = %v", err)
	}
	limits, err := scannerworker.DefaultResourceLimits(kind)
	if err != nil {
		t.Fatalf("DefaultResourceLimits() error = %v", err)
	}
	fencing, err := strconv.ParseInt(strings.TrimSpace(claim.FencingToken), 10, 64)
	if err != nil {
		t.Fatalf("ParseInt(fencing) error = %v", err)
	}
	return scannerworker.ClaimInput{
		WorkItemID:     claim.WorkItemID,
		ClaimID:        "claim-test",
		FencingToken:   fencing,
		OwnerID:        claim.ComponentID,
		Analyzer:       kind,
		Target:         target,
		Limits:         limits,
		GenerationID:   claim.GenerationID,
		Attempt:        claim.Attempt,
		ClaimedAt:      testClock(),
		LeaseExpiresAt: claim.Deadline,
		ObservedAt:     testClock(),
	}
}

// TestRunnerOSRuntimePagesMatchInTreeOutput proves the osruntime runner
// reassembles to the identical fact identity as the direct analyzer.
func TestRunnerOSRuntimePagesMatchInTreeOutput(t *testing.T) {
	t.Parallel()

	scopeID := "image://registry.example/t@sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	root := writeRootFS(t, 60)
	target := map[string]any{
		"scope_id":         scopeID,
		"rootfs_path":      root,
		"source_uri":       "oci://registry.example/t@sha256:deadbeef",
		"source_record_id": "sha256:deadbeef",
	}
	claim := testClaim(IDOSRuntime, scopeID, "scanner-worker:generation-test")
	config := map[string]any{
		"targets":            []any{target},
		"acceptance_unit_id": "image:sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"page_bound_bytes":   8 * 1024,
	}

	reassembled, pages := drivePages(t, IDOSRuntime, claim, config)
	if pages < 2 {
		t.Fatalf("pages = %d, want multi-page output at the small test bound", pages)
	}
	want := directOSRuntimeOutput(t, claim, root)
	compareIdentities(t, "osruntime", reassembled, want.Facts)
	assertKindsGranted(t, scannerworker.AnalyzerOSPackageExtraction, reassembled)
}

// TestRunnerSBOMPagesMatchInTreeOutput proves the sbom runner reassembles to
// the identical fact identity as the direct analyzer over the same repo.
func TestRunnerSBOMPagesMatchInTreeOutput(t *testing.T) {
	t.Parallel()

	scopeID := "scanner-worker://repository/sbom-parity"
	root := writeSBOMRepo(t)
	target := map[string]any{
		"scope_id":       scopeID,
		"root_path":      root,
		"subject_digest": "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
	}
	claim := testClaim(IDSBOM, scopeID, "scanner-worker:generation-test")
	claim.Scope.Kind = "repository"
	config := map[string]any{
		"targets":            []any{target},
		"acceptance_unit_id": "repository:sbom-parity",
		"page_bound_bytes":   4 * 1024,
	}

	reassembled, _ := drivePages(t, IDSBOM, claim, config)
	if len(reassembled) == 0 {
		t.Fatal("reassembled 0 facts, want sbom output")
	}
	want := directSBOMOutput(t, claim, root)
	compareIdentities(t, "sbom", reassembled, want.Facts)
	assertKindsGranted(t, scannerworker.AnalyzerSBOMGeneration, reassembled)
}

// drivePages runs one component through every page and returns the
// reassembled facts with the page count.
func drivePages(t *testing.T, componentID string, claim sdkcollector.Claim, config map[string]any) ([]sdkcollector.Fact, int) {
	t.Helper()

	var reassembled []sdkcollector.Fact
	var cursor *continuation
	pages := 0
	for {
		raw := testRequestWithContinuation(t, claim, config, cursor)
		page := runOne(t, componentID, raw)
		if page.ProtocolVersion != "collector-sdk/v1alpha3" {
			t.Fatalf("page protocol = %q, want v1alpha3", page.ProtocolVersion)
		}
		if page.State != sdkcollector.ResultComplete {
			t.Fatalf("page state = %q, want complete", page.State)
		}
		if page.Page == nil {
			t.Fatal("page envelope missing, want v1alpha3 continuation")
		}
		if page.Page.PageIndex != pages {
			t.Fatalf("page_index = %d, want %d", page.Page.PageIndex, pages)
		}
		if page.Page.TotalFacts <= 0 {
			t.Fatalf("total_facts = %d, want declared positive", page.Page.TotalFacts)
		}
		reassembled = append(reassembled, page.Facts...)
		pages++
		if !page.Page.More {
			break
		}
		cursor = &continuation{Cursor: page.Page.Cursor}
		if pages > 64 {
			t.Fatal("paging did not terminate within 64 pages")
		}
	}
	if len(reassembled) != 0 && pages == 0 {
		t.Fatal("no pages driven")
	}
	return reassembled, pages
}

// compareIdentities proves reassembled satellite facts carry the identical
// kinds, stable keys, generations, and scopes as the in-tree output.
func compareIdentities(t *testing.T, name string, reassembled []sdkcollector.Fact, want []facts.Envelope) {
	t.Helper()

	if len(reassembled) != len(want) {
		t.Fatalf("%s: reassembled %d facts, want %d", name, len(reassembled), len(want))
	}
	for i, envelope := range want {
		got := reassembled[i]
		if got.Kind != envelope.FactKind || got.StableKey != envelope.StableFactKey ||
			got.SourceRef.GenerationID != envelope.GenerationID || got.SourceRef.ScopeID != envelope.SourceRef.ScopeID {
			t.Fatalf("%s: facts[%d] identity changed: got %+v", name, i, got.SourceRef)
		}
	}
}

// assertKindsGranted proves every emitted kind is declared in the
// component manifest, so the required grants always cover real output.
func assertKindsGranted(t *testing.T, kind scannerworker.AnalyzerKind, reassembled []sdkcollector.Fact) {
	t.Helper()

	manifest, err := ManifestFor(kind)
	if err != nil {
		t.Fatalf("ManifestFor(%q) error = %v", kind, err)
	}
	declared := map[string]bool{}
	for _, family := range manifest.Spec.EmittedFacts {
		declared[family.Kind] = true
	}
	for _, fact := range reassembled {
		if !declared[fact.Kind] {
			t.Fatalf("emitted kind %q not declared in the %q manifest", fact.Kind, kind)
		}
	}
}

func writeSBOMRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	write("package-lock.json", `{
		"lockfileVersion": 3,
		"packages": {
			"": {"name": "parity-app", "version": "1.0.0"},
			"node_modules/left-pad": {"version": "1.3.0"}
		}
	}`)
	write("go.mod", "module example.test/parity\n\nrequire github.com/gin-gonic/gin v1.9.1\n")
	return root
}

// directOSRuntimeOutput runs the in-tree osruntime analyzer on the same
// rootfs the runner used.
func directOSRuntimeOutput(t *testing.T, claim sdkcollector.Claim, root string) scannerworker.FactOutput {
	t.Helper()

	analyzer, err := osruntime.NewAnalyzer(osruntime.AnalyzerConfig{
		CollectorInstanceID: claim.InstanceID,
		Targets: []osruntime.TargetConfig{{
			ScopeID:        claim.Scope.ID,
			RootFSPath:     root,
			SourceURI:      "oci://registry.example/t@sha256:deadbeef",
			SourceRecordID: "sha256:deadbeef",
		}},
		Now: testClock,
	})
	if err != nil {
		t.Fatalf("NewAnalyzer() error = %v", err)
	}
	input := testClaimInput(t, claim, scannerworker.AnalyzerOSPackageExtraction, "image:sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	return result.Output
}

// directSBOMOutput runs the in-tree sbom analyzer over the same repo the
// runner used, through the same exported repository source.
func directSBOMOutput(t *testing.T, claim sdkcollector.Claim, root string) scannerworker.FactOutput {
	t.Helper()

	source, err := sbomgenerator.NewRepositorySource([]sbomgenerator.RepositoryTarget{{
		ScopeID:       claim.Scope.ID,
		RootPath:      root,
		SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
	}})
	if err != nil {
		t.Fatalf("NewRepositorySource() error = %v", err)
	}
	analyzer := sbomgenerator.Analyzer{Source: source, Now: testClock}
	input := testClaimInput(t, claim, scannerworker.AnalyzerSBOMGeneration, "repository:sbom-parity")
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	return result.Output
}

// TestRunnerOversizeFactFailsTerminal proves a single fact above the page
// bound fails the claim terminal with a bounded class instead of dropping
// the fact or holding the claim past its deadline one page at a time.
func TestRunnerOversizeFactFailsTerminal(t *testing.T) {
	t.Parallel()

	scopeID := "scanner-worker://repository/sbom-hostile"
	root := t.TempDir()
	giant := strings.Repeat("9.9.9-", 256*1024)
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{
		"lockfileVersion": 3,
		"packages": {
			"": {"name": "giant-app", "version": "1.0.0"},
			"node_modules/giant": {"version": "`+giant+`"}
		}
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	claim := testClaim(IDSBOM, scopeID, "scanner-worker:generation-test")
	config := map[string]any{
		"targets": []any{map[string]any{
			"scope_id":  scopeID,
			"root_path": root,
		}},
		"acceptance_unit_id": "repository:sbom-hostile",
	}
	page, stderr := runRaw(t, IDSBOM, testRequest(t, claim, config))
	if page.State != sdkcollector.ResultTerminal {
		t.Fatalf("state = %q, want terminal for the unpageable fact", page.State)
	}
	if len(page.Statuses) != 1 || page.Statuses[0].Class != sdkcollector.StatusFailure {
		t.Fatalf("statuses = %+v, want one failure status", page.Statuses)
	}
	if page.Statuses[0].FailureClass == "" {
		t.Fatal("failure status carries no bounded class")
	}
	if strings.Contains(stderr, giant[:64]) {
		t.Fatal("stderr leaked the oversized value")
	}
}

// TestRunnerAnalyzerErrorMapsToFailureResult proves an analyzer failure
// returns a failure Result (never a Go error the host would read as a
// launch failure): a missing rootfs is target-unavailable, which the
// vocabulary marks retryable.
func TestRunnerAnalyzerErrorMapsToFailureResult(t *testing.T) {
	t.Parallel()

	scopeID := "image://registry.example/missing@sha256:deadbeef"
	missing := filepath.Join(t.TempDir(), "no-such-rootfs")
	claim := testClaim(IDImage, scopeID, "scanner-worker:generation-test")
	config := map[string]any{
		"targets": []any{map[string]any{
			"scope_id":    scopeID,
			"rootfs_path": missing,
		}},
		"acceptance_unit_id": "image:sha256:deadbeef",
	}
	page, _ := runRaw(t, IDImage, testRequest(t, claim, config))
	if page.State == sdkcollector.ResultComplete {
		t.Fatal("state = complete, want a failure result for the missing rootfs")
	}
	if len(page.Statuses) != 1 || page.Statuses[0].Class != sdkcollector.StatusFailure {
		t.Fatalf("statuses = %+v, want one failure status", page.Statuses)
	}
	if page.Statuses[0].FailureClass == "" {
		t.Fatal("failure status carries no bounded class")
	}
	if len(page.Facts) != 0 {
		t.Fatalf("facts = %d, want none on the failure result", len(page.Facts))
	}
}

// directImageOutput runs the in-tree analyzer on the same rootfs the runner
// used, so the test compares satellite output against in-tree truth.
func directImageOutput(t *testing.T, claim sdkcollector.Claim, root string) scannerworker.FactOutput {
	t.Helper()

	analyzer, err := imageanalyzer.NewAnalyzer(imageanalyzer.AnalyzerConfig{
		CollectorInstanceID: claim.InstanceID,
		Targets: []imageanalyzer.TargetConfig{{
			ScopeID:        claim.Scope.ID,
			RootFSPath:     root,
			SourceURI:      "oci://registry.example/t@sha256:deadbeef",
			SourceRecordID: "sha256:deadbeef",
			ImageReference: "registry.example/t:1.0",
			ImageDigest:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
		Now: testClock,
	})
	if err != nil {
		t.Fatalf("NewAnalyzer() error = %v", err)
	}
	input := testClaimInput(t, claim, scannerworker.AnalyzerImageUnpacking, "image:sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	return result.Output
}

func testRequestWithContinuation(t *testing.T, claim sdkcollector.Claim, config map[string]any, cursor *continuation) []byte {
	t.Helper()

	raw, err := json.Marshal(struct {
		extensionhost.Request
		Continuation *continuation `json:"continuation,omitempty"`
	}{
		Request: extensionhost.Request{
			ProtocolVersion: "collector-sdk/v1alpha3",
			Claim:           claim,
			Config:          map[string]any(config),
		},
		Continuation: cursor,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return raw
}
