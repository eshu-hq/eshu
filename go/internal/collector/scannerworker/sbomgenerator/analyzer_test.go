// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomgenerator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestAnalyzerGeneratesDocumentAndComponentSourceFacts(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	source := &stubSource{
		inventory: Inventory{
			SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
			SpecVersion:   "1.6",
			SourceTool:    "eshu_scanner_sbom_generator 0.0.1",
			FileCount:     10,
			InputBytes:    1024,
			Components: []Component{
				{PURL: "pkg:npm/foo@1.2.3", Name: "foo", Version: "1.2.3", Type: "library", BomRef: "comp-foo"},
				{PURL: "pkg:npm/bar@2.0.0", Name: "bar", Version: "2.0.0", Type: "library", BomRef: "comp-bar"},
			},
		},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	if result.Output.TargetCount != 1 {
		t.Fatalf("TargetCount = %d, want 1", result.Output.TargetCount)
	}
	if result.Output.ResultCount != 2 {
		t.Fatalf("ResultCount = %d, want 2 components", result.Output.ResultCount)
	}
	counts := countFactKinds(result.Output.Facts)
	if counts[facts.SBOMDocumentFactKind] != 1 {
		t.Fatalf("document facts = %d, want 1", counts[facts.SBOMDocumentFactKind])
	}
	if counts[facts.SBOMComponentFactKind] != 2 {
		t.Fatalf("component facts = %d, want 2", counts[facts.SBOMComponentFactKind])
	}
	if counts[facts.SBOMWarningFactKind] != 0 {
		t.Fatalf("warning facts = %d, want 0", counts[facts.SBOMWarningFactKind])
	}
	doc := firstFact(result.Output.Facts, facts.SBOMDocumentFactKind)
	if got := doc.Payload["subject_digest"]; got != source.inventory.SubjectDigest {
		t.Fatalf("subject_digest = %v, want %q", got, source.inventory.SubjectDigest)
	}
	if got := doc.Payload["parse_status"]; got != "generated" {
		t.Fatalf("parse_status = %v, want %q", got, "generated")
	}
	if got := doc.Payload["format"]; got != "cyclonedx" {
		t.Fatalf("format = %v, want %q", got, "cyclonedx")
	}
	if got := doc.Payload["component_count"]; got != 2 {
		t.Fatalf("component_count = %v, want 2", got)
	}
	if got := doc.Payload["generated_by_analyzer"]; got != string(scannerworker.AnalyzerSBOMGeneration) {
		t.Fatalf("generated_by_analyzer = %v, want %q", got, scannerworker.AnalyzerSBOMGeneration)
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
}

func TestAnalyzerEmitsMissingSubjectWarningWithoutFailing(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	source := &stubSource{
		inventory: Inventory{
			SubjectDigest: "",
			Components: []Component{
				{PURL: "pkg:pypi/baz@9.9.9", Name: "baz", Version: "9.9.9", Type: "library"},
			},
		},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil for warning-only outcome", err)
	}
	counts := countFactKinds(result.Output.Facts)
	if counts[facts.SBOMDocumentFactKind] != 1 {
		t.Fatalf("document facts = %d, want 1", counts[facts.SBOMDocumentFactKind])
	}
	if counts[facts.SBOMComponentFactKind] != 1 {
		t.Fatalf("component facts = %d, want 1", counts[facts.SBOMComponentFactKind])
	}
	if counts[facts.SBOMWarningFactKind] == 0 {
		t.Fatalf("warning facts = 0, want at least 1 (missing_subject)")
	}
	if !hasWarningReason(result.Output.Facts, "missing_subject") {
		t.Fatalf("warnings = %v, want missing_subject", warningReasons(result.Output.Facts))
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
}

func TestAnalyzerSkipsComponentsMissingIdentityWithWarning(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	source := &stubSource{
		inventory: Inventory{
			SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
			Components: []Component{
				{PURL: "pkg:npm/foo@1.0.0", Name: "foo", Version: "1.0.0"},
				{}, // no identity
			},
		},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	counts := countFactKinds(result.Output.Facts)
	if counts[facts.SBOMComponentFactKind] != 1 {
		t.Fatalf("component facts = %d, want 1 (malformed skipped)", counts[facts.SBOMComponentFactKind])
	}
	if !hasWarningReason(result.Output.Facts, "component_missing_identity") {
		t.Fatalf("warnings = %v, want component_missing_identity", warningReasons(result.Output.Facts))
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
}

func TestAnalyzerPreservesLockfileComponentAndWarningEvidence(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	source := &stubSource{
		inventory: Inventory{
			SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
			Components: []Component{{
				PURL:             "pkg:composer/symfony/console@v7.0.0",
				Name:             "symfony/console",
				Version:          "v7.0.0",
				Type:             "library",
				Ecosystem:        "composer",
				EvidenceSource:   "repository_lockfile",
				LockfilePath:     "services/api/composer.lock",
				DependencyScope:  "packages",
				DependencyType:   "runtime",
				ExtractionReason: "lockfile_exact_version",
			}},
			Warnings: []Warning{{
				Reason:           WarningReasonLockfileMalformed,
				Summary:          "services/api/packages.lock.json could not be parsed as nuget lockfile evidence: unexpected EOF",
				Ecosystem:        "nuget",
				EvidenceSource:   "repository_lockfile",
				LockfilePath:     "services/api/packages.lock.json",
				ExtractionReason: "lockfile_malformed",
			}},
		},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	component := firstFact(result.Output.Facts, facts.SBOMComponentFactKind)
	assertPayloadString(t, component.Payload, "ecosystem", "composer")
	assertPayloadString(t, component.Payload, "evidence_source", "repository_lockfile")
	assertPayloadString(t, component.Payload, "lockfile_path", "services/api/composer.lock")
	assertPayloadString(t, component.Payload, "dependency_scope", "packages")
	assertPayloadString(t, component.Payload, "dependency_type", "runtime")
	assertPayloadString(t, component.Payload, "extraction_reason", "lockfile_exact_version")
	warning := firstFact(result.Output.Facts, facts.SBOMWarningFactKind)
	assertPayloadString(t, warning.Payload, "reason", WarningReasonLockfileMalformed)
	assertPayloadString(t, warning.Payload, "ecosystem", "nuget")
	assertPayloadString(t, warning.Payload, "lockfile_path", "services/api/packages.lock.json")
	assertPayloadString(t, warning.Payload, "extraction_reason", "lockfile_malformed")
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
}

func TestAnalyzerNeverEmitsSilentCleanOutput(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	source := &stubSource{
		inventory: Inventory{Components: nil},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	if len(result.Output.Facts) == 0 {
		t.Fatal("analyzer emitted zero facts; scanner workers must emit at least one source or warning fact")
	}
	if !hasWarningReason(result.Output.Facts, "no_components_found") {
		t.Fatalf("warnings = %v, want no_components_found", warningReasons(result.Output.Facts))
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
}

func TestAnalyzerEnforcesFileCountLimit(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	source := &stubSource{
		inventory: Inventory{
			SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
			FileCount:     input.Limits.MaxFiles + 1,
			Components:    []Component{{PURL: "pkg:npm/x@1", Name: "x", Version: "1"}},
		},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	_, err := analyzer.Analyze(context.Background(), input)
	assertFailure(t, err, scannerworker.FailureClassFileLimitExceeded, false)
}

func TestAnalyzerEnforcesInputByteLimit(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	source := &stubSource{
		inventory: Inventory{
			SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
			InputBytes:    input.Limits.MaxInputBytes + 1,
			Components:    []Component{{PURL: "pkg:npm/x@1", Name: "x", Version: "1"}},
		},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	_, err := analyzer.Analyze(context.Background(), input)
	assertFailure(t, err, scannerworker.FailureClassInputLimitExceeded, false)
}

func TestAnalyzerEnforcesFactLimit(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	input.Limits.MaxFacts = 2
	components := []Component{
		{PURL: "pkg:npm/a@1", Name: "a", Version: "1"},
		{PURL: "pkg:npm/b@1", Name: "b", Version: "1"},
		{PURL: "pkg:npm/c@1", Name: "c", Version: "1"},
	}
	source := &stubSource{
		inventory: Inventory{
			SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
			Components:    components,
		},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	_, err := analyzer.Analyze(context.Background(), input)
	assertFailure(t, err, scannerworker.FailureClassFactLimitExceeded, false)
}

func TestAnalyzerRejectsUnsupportedTarget(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	source := &stubSource{err: ErrUnsupportedTarget}
	analyzer := Analyzer{Source: source, Now: testClock}

	_, err := analyzer.Analyze(context.Background(), input)
	assertFailure(t, err, scannerworker.FailureClassUnsupportedTarget, false)
}

func TestAnalyzerRetriesOnSourceUnavailable(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	source := &stubSource{err: ErrSourceUnavailable}
	analyzer := Analyzer{Source: source, Now: testClock}

	_, err := analyzer.Analyze(context.Background(), input)
	assertFailure(t, err, scannerworker.FailureClassSourceUnavailable, true)
}

func TestAnalyzerDeadLettersOnGenericSourceError(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	source := &stubSource{err: errors.New("scanner panic with private repo path /Users/private/repo")}
	analyzer := Analyzer{Source: source, Now: testClock}

	_, err := analyzer.Analyze(context.Background(), input)
	assertFailure(t, err, scannerworker.FailureClassAnalyzerFailed, false)
	if strings.Contains(err.Error(), "/Users/private/repo") {
		t.Fatalf("error string leaked private locator: %q", err.Error())
	}
}

func TestAnalyzerDeduplicatesComponentsByCanonicalIdentity(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	source := &stubSource{
		inventory: Inventory{
			SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
			Components: []Component{
				{PURL: "pkg:npm/foo@1.2.3", Name: "Foo", Version: "1.2.3"},
				// Same canonical PURL with different casing and a different
				// bom_ref must collapse to one emitted component fact and
				// surface a component_missing_identity duplicate warning.
				{PURL: "PKG:NPM/foo@1.2.3", Name: "foo", Version: "1.2.3", BomRef: "alt"},
				// Same canonical name+version identity expressed without a PURL
				// must also collapse against an already-emitted PURL entry that
				// resolves to the same canonical identity bucket.
				{Name: "bar", Version: "9.9.9"},
				{Name: "BAR", Version: "9.9.9"},
			},
		},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	counts := countFactKinds(result.Output.Facts)
	if counts[facts.SBOMComponentFactKind] != 2 {
		t.Fatalf("component facts = %d, want 2 (canonical dedup collapsed casing duplicates)", counts[facts.SBOMComponentFactKind])
	}
	if got := warningReasons(result.Output.Facts); !containsString(got, "component_missing_identity") {
		t.Fatalf("warnings = %v, want component_missing_identity duplicate warning", got)
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
}

func TestAnalyzerRejectsMalformedSubjectDigest(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	source := &stubSource{
		inventory: Inventory{
			SubjectDigest: "not-a-digest",
			Components:    []Component{{PURL: "pkg:npm/x@1", Name: "x", Version: "1"}},
		},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil; malformed subject is a warning not a failure", err)
	}
	if !hasWarningReason(result.Output.Facts, "malformed_subject_digest") {
		t.Fatalf("warnings = %v, want malformed_subject_digest", warningReasons(result.Output.Facts))
	}
	doc := firstFact(result.Output.Facts, facts.SBOMDocumentFactKind)
	if got := doc.Payload["subject_digest"]; got != "" {
		t.Fatalf("subject_digest = %v, want empty for malformed input", got)
	}
}

// ----- helpers -----

func countFactKinds(envelopes []facts.Envelope) map[string]int {
	out := make(map[string]int, len(envelopes))
	for _, env := range envelopes {
		out[env.FactKind]++
	}
	return out
}

func firstFact(envelopes []facts.Envelope, factKind string) facts.Envelope {
	for _, env := range envelopes {
		if env.FactKind == factKind {
			return env
		}
	}
	return facts.Envelope{}
}

func hasWarningReason(envelopes []facts.Envelope, reason string) bool {
	for _, env := range envelopes {
		if env.FactKind != facts.SBOMWarningFactKind {
			continue
		}
		if got, _ := env.Payload["reason"].(string); got == reason {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func warningReasons(envelopes []facts.Envelope) []string {
	out := make([]string, 0)
	for _, env := range envelopes {
		if env.FactKind != facts.SBOMWarningFactKind {
			continue
		}
		if reason, ok := env.Payload["reason"].(string); ok {
			out = append(out, reason)
		}
	}
	return out
}

func assertPayloadString(t *testing.T, payload map[string]any, key string, want string) {
	t.Helper()

	if got, _ := payload[key].(string); got != want {
		t.Fatalf("payload[%q] = %#v, want %q", key, payload[key], want)
	}
}

func testClock() time.Time {
	return time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
}

func assertFailure(t *testing.T, err error, want scannerworker.FailureClass, retryable bool) {
	t.Helper()
	if err == nil {
		t.Fatalf("Analyze() error = nil, want failure class %q", want)
	}
	var analyzerErr scannerworker.AnalyzerFailure
	if !errors.As(err, &analyzerErr) {
		t.Fatalf("Analyze() error = %T %v, want AnalyzerFailure", err, err)
	}
	if got := analyzerErr.FailureClass(); got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
	if got := analyzerErr.Retryable(); got != retryable {
		t.Fatalf("Retryable = %v, want %v", got, retryable)
	}
}

type stubSource struct {
	inventory Inventory
	err       error
}

func (s *stubSource) Collect(_ context.Context, _ scannerworker.ClaimInput) (Inventory, error) {
	if s.err != nil {
		return Inventory{}, s.err
	}
	return s.inventory, nil
}

func testClaimInput(t *testing.T) scannerworker.ClaimInput {
	t.Helper()

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	item := workflow.WorkItem{
		WorkItemID:          "scanner-worker:collector-scanner:work-1",
		RunID:               "scanner-worker:run-1",
		CollectorKind:       scope.CollectorScannerWorker,
		CollectorInstanceID: "collector-scanner",
		SourceSystem:        string(scope.CollectorScannerWorker),
		ScopeID:             "scanner-worker://repository/repo-private-name",
		AcceptanceUnitID:    "repository:repo-123",
		SourceRunID:         "scanner-worker:generation-1",
		GenerationID:        "scanner-worker:generation-1",
		FairnessKey:         "scanner_worker:collector-scanner:repository",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        2,
		CurrentClaimID:      "claim-1",
		CurrentFencingToken: 7,
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
		LeaseExpiresAt: now.Add(time.Minute),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	target := scannerworker.TargetScope{
		Kind:             scannerworker.TargetRepository,
		ScopeID:          item.ScopeID,
		AcceptanceUnitID: item.AcceptanceUnitID,
		SourceRunID:      item.SourceRunID,
		GenerationID:     item.GenerationID,
		LocatorHash:      "sha256:6b1f0b588fce9b40d6f56e4b5d6f3ef9d76c3ee6f2c2b66f7f4b3b6fb2c5c111",
	}
	limits := scannerworker.ResourceLimits{
		CPUMillis:     2000,
		MemoryBytes:   1 << 30,
		Timeout:       10 * time.Minute,
		MaxInputBytes: 2 << 30,
		MaxFiles:      250000,
		MaxFacts:      50000,
	}
	input, err := scannerworker.NewClaimInput(item, claim, scannerworker.AnalyzerSBOMGeneration, target, limits)
	if err != nil {
		t.Fatalf("NewClaimInput() error = %v", err)
	}
	return input
}
