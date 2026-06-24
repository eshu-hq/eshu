// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestAnalyzerExtractsOSPackagesFromLayerEvidence(t *testing.T) {
	t.Parallel()

	layer := writeLayer(t, map[string]string{
		"etc/os-release":          "ID=alpine\nVERSION_ID=3.19.1\n",
		"etc/apk/repositories":    "https://dl-cdn.alpinelinux.org/alpine/v3.19/main\n",
		"lib/apk/db/installed":    "P:openssl\nV:3.1.4-r5\nA:x86_64\no:openssl\n\n",
		"var/log/private-content": "ignored",
	})
	input := testImageClaimInput(t)
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:        input.Target.ScopeID,
		ImageReference: "registry.example/team/app:1.2.3",
		ImageDigest:    "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
		LayerPaths:     []string{layer},
		SourceURI:      "oci://registry.example/team/app@sha256:11111111111111111111111111111111111111111111111111111111111111aa?token=redacted",
		SourceRecordID: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
	})

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	if got, want := result.Output.TargetCount, 1; got != want {
		t.Fatalf("TargetCount = %d, want %d", got, want)
	}
	if got, want := result.Output.ResultCount, 1; got != want {
		t.Fatalf("ResultCount = %d, want %d", got, want)
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
	fact := firstFactByKind(t, result.Output.Facts, facts.VulnerabilityOSPackageFactKind)
	if got, want := fact.Payload["name"], "openssl"; got != want {
		t.Fatalf("name = %#v, want %q", got, want)
	}
	if got, want := fact.Payload["installed_version_raw"], "3.1.4-r5"; got != want {
		t.Fatalf("installed_version_raw = %#v, want %q", got, want)
	}
	if got, want := fact.Payload["image_digest"], "sha256:11111111111111111111111111111111111111111111111111111111111111aa"; got != want {
		t.Fatalf("image_digest = %#v, want %q", got, want)
	}
	if got, want := fact.Payload["image_reference"], "registry.example/team/app:1.2.3"; got != want {
		t.Fatalf("image_reference = %#v, want %q", got, want)
	}
	if got, want := fact.Payload["evidence_source"], "layer"; got != want {
		t.Fatalf("evidence_source = %#v, want %q", got, want)
	}
	if strings.Contains(fact.SourceRef.SourceURI, "token=redacted") {
		t.Fatalf("SourceRef.SourceURI leaked credential-bearing query string: %q", fact.SourceRef.SourceURI)
	}
	if result.Usage.PeakMemoryBytes <= 0 {
		t.Fatalf("PeakMemoryBytes = %d, want resource usage recorded", result.Usage.PeakMemoryBytes)
	}
}

func TestAnalyzerExtractsOSPackagesFromRootFSEvidence(t *testing.T) {
	t.Parallel()

	rootfs := t.TempDir()
	writeRootFSFile(t, rootfs, "etc/os-release", "ID=debian\nVERSION_ID=12\n")
	writeRootFSFile(t, rootfs, "var/lib/dpkg/status", strings.Join([]string{
		"Package: openssl",
		"Status: install ok installed",
		"Version: 3.0.11-1~deb12u2",
		"Architecture: amd64",
		"Source: openssl (3.0.11-1~deb12u2)",
		"",
	}, "\n"))
	input := testImageClaimInput(t)
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:        input.Target.ScopeID,
		RootFSPath:     rootfs,
		ImageReference: "registry.example/team/api@sha256:22222222222222222222222222222222222222222222222222222222222222bb",
		ImageDigest:    "sha256:22222222222222222222222222222222222222222222222222222222222222bb",
	})

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
	fact := firstFactByKind(t, result.Output.Facts, facts.VulnerabilityOSPackageFactKind)
	if got, want := fact.Payload["package_manager"], "dpkg"; got != want {
		t.Fatalf("package_manager = %#v, want %q", got, want)
	}
	if got, want := fact.Payload["distro"], "debian"; got != want {
		t.Fatalf("distro = %#v, want %q", got, want)
	}
	if got, want := fact.Payload["evidence_source"], "rootfs"; got != want {
		t.Fatalf("evidence_source = %#v, want %q", got, want)
	}
}

func TestAnalyzerEmitsUnsupportedEvidenceForRootFSMissingPackageDatabase(t *testing.T) {
	t.Parallel()

	rootfs := t.TempDir()
	writeRootFSFile(t, rootfs, "etc/os-release", "ID=debian\nVERSION_ID=12\n")
	input := testImageClaimInput(t)
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:        input.Target.ScopeID,
		RootFSPath:     rootfs,
		ImageReference: "registry.example/team/api:missing-db",
		ImageDigest:    "sha256:44444444444444444444444444444444444444444444444444444444444444dd",
	})

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil warning output", err)
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
	warning := firstFactByKind(t, result.Output.Facts, facts.ScannerWorkerWarningFactKind)
	if got, want := warning.Payload["distro"], "debian"; got != want {
		t.Fatalf("distro = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["distro_version"], "12"; got != want {
		t.Fatalf("distro_version = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["package_manager"], "dpkg"; got != want {
		t.Fatalf("package_manager = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["extraction_reason"], "unsupported_or_missing_package_database"; got != want {
		t.Fatalf("extraction_reason = %#v, want %q", got, want)
	}
}

func TestAnalyzerEmitsUnsupportedEvidenceForMissingPackageDatabase(t *testing.T) {
	t.Parallel()

	layer := writeLayer(t, map[string]string{
		"etc/os-release": "ID=wolfi\nVERSION_ID=2026\n",
	})
	input := testImageClaimInput(t)
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:        input.Target.ScopeID,
		ImageReference: "registry.example/team/wolfi:latest",
		ImageDigest:    "sha256:33333333333333333333333333333333333333333333333333333333333333cc",
		LayerPaths:     []string{layer},
	})

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil warning output", err)
	}
	if got, want := result.Output.ResultCount, 0; got != want {
		t.Fatalf("ResultCount = %d, want %d", got, want)
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
	warning := firstFactByKind(t, result.Output.Facts, facts.ScannerWorkerWarningFactKind)
	if got, want := warning.Payload["reason"], "image_analyzer_unsupported_target"; got != want {
		t.Fatalf("reason = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["extraction_reason"], "unsupported_or_missing_package_database"; got != want {
		t.Fatalf("extraction_reason = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["image_digest"], "sha256:33333333333333333333333333333333333333333333333333333333333333cc"; got != want {
		t.Fatalf("image_digest = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["distro"], "wolfi"; got != want {
		t.Fatalf("distro = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["distro_version"], "2026"; got != want {
		t.Fatalf("distro_version = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["evidence_source"], "layer"; got != want {
		t.Fatalf("evidence_source = %#v, want %q", got, want)
	}
}

func TestAnalyzerEnforcesLayerInputLimit(t *testing.T) {
	t.Parallel()

	layer := writeLayer(t, map[string]string{
		"etc/os-release":       "ID=alpine\nVERSION_ID=3.19.1\n",
		"lib/apk/db/installed": strings.Repeat("x", 128),
	})
	input := testImageClaimInput(t)
	input.Limits.MaxInputBytes = 64
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:    input.Target.ScopeID,
		LayerPaths: []string{layer},
	})

	_, err := analyzer.Analyze(context.Background(), input)
	if err == nil {
		t.Fatal("Analyze() error = nil, want input limit failure")
	}
	if got, want := err.Error(), string(scannerworker.FailureClassInputLimitExceeded); !strings.Contains(got, want) {
		t.Fatalf("Analyze() error = %q, want failure class %q", got, want)
	}
}

func TestAnalyzerEnforcesLayerFileLimitAcrossArchiveEntries(t *testing.T) {
	t.Parallel()

	layer := writeLayer(t, map[string]string{
		"etc/os-release":       "ID=alpine\nVERSION_ID=3.19.1\n",
		"tmp/ignored":          "still counts toward file budget",
		"lib/apk/db/installed": "P:openssl\nV:3.1.4-r5\nA:x86_64\n\n",
	})
	input := testImageClaimInput(t)
	input.Limits.MaxFiles = 2
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:    input.Target.ScopeID,
		LayerPaths: []string{layer},
	})

	_, err := analyzer.Analyze(context.Background(), input)
	if err == nil {
		t.Fatal("Analyze() error = nil, want file limit failure")
	}
	if got, want := err.Error(), string(scannerworker.FailureClassFileLimitExceeded); !strings.Contains(got, want) {
		t.Fatalf("Analyze() error = %q, want failure class %q", got, want)
	}
}

func TestAnalyzerDoesNotLeakLayerPathInFailurePayload(t *testing.T) {
	t.Parallel()

	privateLayerPath := filepath.Join(t.TempDir(), "private-layer.tar.gz")
	input := testImageClaimInput(t)
	item, claim := testImageWorkItemAndClaim()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &serviceClaimStore{item: item, claim: claim, cancel: cancel}
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:    input.Target.ScopeID,
		LayerPaths: []string{privateLayerPath},
	})
	service := scannerworker.Service{
		ControlStore:        store,
		Committer:           &serviceCommitter{},
		Analyzer:            analyzer,
		AnalyzerKind:        scannerworker.AnalyzerImageUnpacking,
		CollectorInstanceID: item.CollectorInstanceID,
		OwnerID:             item.CurrentOwnerID,
		ClaimIDFunc:         func() string { return claim.ClaimID },
		PollInterval:        time.Second,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   20 * time.Second,
		Clock:               fixedNow,
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if !store.retryable {
		t.Fatalf("retryable = false, want true for missing layer source")
	}
	if strings.Contains(store.failureMessage(), privateLayerPath) {
		t.Fatalf("failure payload leaked private layer path: %q", store.failureMessage())
	}
}

func newTestAnalyzer(t *testing.T, target TargetConfig) *Analyzer {
	t.Helper()
	analyzer, err := NewAnalyzer(AnalyzerConfig{
		CollectorInstanceID: "scanner-worker-image",
		Targets:             []TargetConfig{target},
		Now:                 fixedNow,
	})
	if err != nil {
		t.Fatalf("NewAnalyzer() error = %v, want nil", err)
	}
	return analyzer
}

func testImageClaimInput(t *testing.T) scannerworker.ClaimInput {
	t.Helper()
	item, claim := testImageWorkItemAndClaim()
	target, err := scannerworker.TargetScopeFromWorkItem(item)
	if err != nil {
		t.Fatalf("TargetScopeFromWorkItem() error = %v, want nil", err)
	}
	limits, err := scannerworker.DefaultResourceLimits(scannerworker.AnalyzerImageUnpacking)
	if err != nil {
		t.Fatalf("DefaultResourceLimits() error = %v, want nil", err)
	}
	input, err := scannerworker.NewClaimInputAt(item, claim, scannerworker.AnalyzerImageUnpacking, target, limits, fixedNow())
	if err != nil {
		t.Fatalf("NewClaimInputAt() error = %v, want nil", err)
	}
	return input
}

func testImageWorkItemAndClaim() (workflow.WorkItem, workflow.Claim) {
	now := fixedNow()
	item := workflow.WorkItem{
		WorkItemID:          "scanner-worker:image:work-1",
		RunID:               "scanner-worker:run-1",
		CollectorKind:       scope.CollectorScannerWorker,
		CollectorInstanceID: "scanner-worker-image",
		SourceSystem:        string(scope.CollectorScannerWorker),
		ScopeID:             "image://registry.example/team/app@sha256:11111111111111111111111111111111111111111111111111111111111111aa",
		AcceptanceUnitID:    "image:sha256:11111111111111111111111111111111111111111111111111111111111111aa",
		SourceRunID:         "scanner-worker:generation-1",
		GenerationID:        "scanner-worker:generation-1",
		FairnessKey:         "scanner_worker:scanner-worker-image:image",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentClaimID:      "claim-image",
		CurrentFencingToken: 17,
		CurrentOwnerID:      "scanner-worker-1",
		LeaseExpiresAt:      now.Add(time.Minute),
		VisibleAt:           now.Add(-time.Minute),
		LastClaimedAt:       now,
		CreatedAt:           now.Add(-time.Hour),
		UpdatedAt:           now,
	}
	claim := workflow.Claim{
		ClaimID:        item.CurrentClaimID,
		WorkItemID:     item.WorkItemID,
		FencingToken:   item.CurrentFencingToken,
		OwnerID:        item.CurrentOwnerID,
		Status:         workflow.ClaimStatusActive,
		ClaimedAt:      now,
		HeartbeatAt:    now,
		LeaseExpiresAt: item.LeaseExpiresAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return item, claim
}

func writeLayer(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "layer.tar.gz")
	var body bytes.Buffer
	gzipWriter := gzip.NewWriter(&body)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, content := range files {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatalf("WriteHeader(%q) error = %v", name, err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatalf("Write(%q) error = %v", name, err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("tar Close() error = %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("gzip Close() error = %v", err)
	}
	if err := os.WriteFile(path, body.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}

func writeRootFSFile(t *testing.T, rootfs string, rel string, body string) {
	t.Helper()
	path := filepath.Join(rootfs, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func firstFactByKind(t *testing.T, envelopes []facts.Envelope, kind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			return envelope
		}
	}
	t.Fatalf("fact kind %q not found in %d facts", kind, len(envelopes))
	return facts.Envelope{}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
}

type serviceClaimStore struct {
	item             workflow.WorkItem
	claim            workflow.Claim
	cancel           context.CancelFunc
	claimed          bool
	retryable        bool
	retryMutation    workflow.ClaimMutation
	terminalMutation workflow.ClaimMutation
}

func (s *serviceClaimStore) ClaimNextEligible(context.Context, workflow.ClaimSelector, time.Time, time.Duration) (workflow.WorkItem, workflow.Claim, bool, error) {
	if s.claimed {
		return workflow.WorkItem{}, workflow.Claim{}, false, nil
	}
	s.claimed = true
	return s.item, s.claim, true, nil
}

func (s *serviceClaimStore) HeartbeatClaim(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (s *serviceClaimStore) CompleteClaim(context.Context, workflow.ClaimMutation) error {
	s.cancel()
	return nil
}

func (s *serviceClaimStore) FailClaimRetryable(_ context.Context, mutation workflow.ClaimMutation) error {
	s.retryable = true
	s.retryMutation = mutation
	s.cancel()
	return nil
}

func (s *serviceClaimStore) FailClaimTerminal(_ context.Context, mutation workflow.ClaimMutation) error {
	s.terminalMutation = mutation
	s.cancel()
	return nil
}

func (s *serviceClaimStore) failureMessage() string {
	if s.retryable {
		return s.retryMutation.FailureMessage
	}
	return s.terminalMutation.FailureMessage
}

type serviceCommitter struct{}

func (serviceCommitter) CommitClaimedScopeGeneration(context.Context, workflow.ClaimMutation, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error {
	return errors.New("commit should not be called for analyzer failures")
}
