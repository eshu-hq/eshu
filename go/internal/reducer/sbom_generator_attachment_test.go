// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/sbomgenerator"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const generatorSubjectDigest = "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

// TestScannerWorkerGeneratedSBOMFactsAdmittedByReducerAttachment proves that
// the bounded sbomgenerator analyzer's source facts flow only through the
// existing reducer-owned attachment path. The reducer classifies the
// generator's document as attached_parse_only when a subject digest is
// present, but reports that evidence as parse-only and unanchored until an OCI
// referrer proves image attachment. Scanner workers must never
// short-circuit attachment truth on their own.
func TestScannerWorkerGeneratedSBOMFactsAdmittedByReducerAttachment(t *testing.T) {
	t.Parallel()

	withSubject := runSBOMGenerator(t, sbomgenerator.Inventory{
		SubjectDigest: generatorSubjectDigest,
		Components: []sbomgenerator.Component{
			{PURL: "pkg:npm/foo@1.2.3", Name: "foo", Version: "1.2.3", Type: "library"},
			{PURL: "pkg:npm/bar@2.0.0", Name: "bar", Version: "2.0.0", Type: "library"},
		},
	})
	decisionsWithSubject := reducer.BuildSBOMAttestationAttachmentDecisions(withSubject)
	if got, want := len(decisionsWithSubject), 1; got != want {
		t.Fatalf("decisions = %d, want %d for one generator document", got, want)
	}
	withSubjectDecision := decisionsWithSubject[0]
	if got, want := withSubjectDecision.AttachmentStatus, reducer.SBOMAttachmentAttachedParseOnly; got != want {
		t.Fatalf("AttachmentStatus = %q, want %q (subject digest present, no verification evidence)", got, want)
	}
	if got, want := withSubjectDecision.SubjectDigest, generatorSubjectDigest; got != want {
		t.Fatalf("SubjectDigest = %q, want %q", got, want)
	}
	if withSubjectDecision.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for unanchored parse-only", withSubjectDecision.CanonicalWrites)
	}
	if got, want := withSubjectDecision.AttachmentScope, "parse_only_unanchored"; got != want {
		t.Fatalf("AttachmentScope = %q, want %q", got, want)
	}
	if got, want := len(withSubjectDecision.MissingEvidence), 2; got != want {
		t.Fatalf("MissingEvidence = %#v, want two missing anchor reasons", withSubjectDecision.MissingEvidence)
	}
	if got, want := withSubjectDecision.ComponentCount, 2; got != want {
		t.Fatalf("ComponentCount = %d, want %d generated components attached", got, want)
	}
	if got, want := withSubjectDecision.Format, sbomgenerator.Format; got != want {
		t.Fatalf("Format = %q, want %q (analyzer output stays CycloneDX-compatible)", got, want)
	}

	missingSubject := runSBOMGenerator(t, sbomgenerator.Inventory{
		Components: []sbomgenerator.Component{
			{PURL: "pkg:pypi/baz@9.9.9", Name: "baz", Version: "9.9.9", Type: "library"},
		},
	})
	decisionsMissingSubject := reducer.BuildSBOMAttestationAttachmentDecisions(missingSubject)
	if got, want := len(decisionsMissingSubject), 1; got != want {
		t.Fatalf("missing-subject decisions = %d, want %d", got, want)
	}
	missingDecision := decisionsMissingSubject[0]
	if got, want := missingDecision.AttachmentStatus, reducer.SBOMAttachmentUnknownSubject; got != want {
		t.Fatalf("missing-subject AttachmentStatus = %q, want %q", got, want)
	}
	if missingDecision.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for unknown-subject", missingDecision.CanonicalWrites)
	}
	if len(missingDecision.WarningSummaries) == 0 {
		t.Fatal("WarningSummaries empty, want missing_subject warning surfaced through reducer attachment")
	}

	withLockfileEvidence := runSBOMGenerator(t, sbomgenerator.Inventory{
		SubjectDigest: generatorSubjectDigest,
		Components: []sbomgenerator.Component{{
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
		Warnings: []sbomgenerator.Warning{{
			Reason:           sbomgenerator.WarningReasonLockfileMalformed,
			Summary:          "services/api/packages.lock.json could not be parsed as nuget lockfile evidence: unexpected EOF",
			Ecosystem:        "nuget",
			EvidenceSource:   "repository_lockfile",
			LockfilePath:     "services/api/packages.lock.json",
			ExtractionReason: "lockfile_malformed",
		}},
	})
	decisionsWithLockfileEvidence := reducer.BuildSBOMAttestationAttachmentDecisions(withLockfileEvidence)
	if got, want := len(decisionsWithLockfileEvidence), 1; got != want {
		t.Fatalf("lockfile-evidence decisions = %d, want %d", got, want)
	}
	lockfileDecision := decisionsWithLockfileEvidence[0]
	if got, want := lockfileDecision.AttachmentStatus, reducer.SBOMAttachmentAttachedParseOnly; got != want {
		t.Fatalf("lockfile-evidence AttachmentStatus = %q, want %q", got, want)
	}
	if got, want := len(lockfileDecision.ComponentEvidence), 1; got != want {
		t.Fatalf("ComponentEvidence len = %d, want %d", got, want)
	}
	componentEvidence := lockfileDecision.ComponentEvidence[0]
	if got, want := componentEvidence["ecosystem"], "composer"; got != want {
		t.Fatalf("ComponentEvidence ecosystem = %q, want %q", got, want)
	}
	if got, want := componentEvidence["lockfile_path"], "services/api/composer.lock"; got != want {
		t.Fatalf("ComponentEvidence lockfile_path = %q, want %q", got, want)
	}
	if got, want := componentEvidence["dependency_scope"], "packages"; got != want {
		t.Fatalf("ComponentEvidence dependency_scope = %q, want %q", got, want)
	}
	if got, want := componentEvidence["extraction_reason"], "lockfile_exact_version"; got != want {
		t.Fatalf("ComponentEvidence extraction_reason = %q, want %q", got, want)
	}
	if len(lockfileDecision.WarningSummaries) != 1 {
		t.Fatalf("WarningSummaries = %#v, want one malformed lockfile summary", lockfileDecision.WarningSummaries)
	}
}

func TestScannerWorkerGeneratedSBOMAggregatedWarningCountFlowsThroughReducerAttachment(t *testing.T) {
	t.Parallel()

	components := []sbomgenerator.Component{
		{PURL: "pkg:npm/kept@1.0.0", Name: "kept", Version: "1.0.0"},
	}
	for range 25 {
		components = append(components, sbomgenerator.Component{Type: "library"})
	}
	envelopes := runSBOMGenerator(t, sbomgenerator.Inventory{
		SubjectDigest: generatorSubjectDigest,
		Components:    components,
	})
	decisions := reducer.BuildSBOMAttestationAttachmentDecisions(envelopes)
	if got, want := len(decisions), 1; got != want {
		t.Fatalf("decisions = %d, want %d for one generator document", got, want)
	}
	decision := decisions[0]
	if got, want := len(decision.WarningSummaries), 1; got != want {
		t.Fatalf("WarningSummaries = %#v, want one aggregate warning", decision.WarningSummaries)
	}
	if got, want := decision.WarningSummaryCount, 25; got != want {
		t.Fatalf("WarningSummaryCount = %d, want %d", got, want)
	}
	if summary := decision.WarningSummaries[0]; !strings.Contains(summary, "25 components missing") {
		t.Fatalf("WarningSummaries[0] = %q, want aggregate count", summary)
	}
}

func runSBOMGenerator(t *testing.T, inventory sbomgenerator.Inventory) []facts.Envelope {
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
		AttemptCount:        1,
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
		t.Fatalf("NewClaimInput() error = %v, want nil", err)
	}
	analyzer := sbomgenerator.Analyzer{
		Source: &fixedSBOMSource{inventory: inventory},
		Now:    func() time.Time { return now },
	}
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
	return result.Output.Facts
}

type fixedSBOMSource struct {
	inventory sbomgenerator.Inventory
}

func (s *fixedSBOMSource) Collect(_ context.Context, _ scannerworker.ClaimInput) (sbomgenerator.Inventory, error) {
	return s.inventory, nil
}
