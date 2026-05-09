package doctruth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestDeploymentDriftAnalyzerReturnsMatchForDocumentedDeploymentTruth(t *testing.T) {
	t.Parallel()

	analyzer := doctruth.NewDeploymentDriftAnalyzer(doctruth.DriftOptions{})
	findings := analyzer.FindServiceDeploymentDrift(context.Background(), []doctruth.DeploymentDriftInput{{
		SourceSystem: "confluence",
		Claim:        deploymentClaim("payment-api", "payment-prod"),
		Mentions: []facts.DocumentationEntityMentionPayload{
			exactMention("mention:service:payment-api", "service", "payment-api", "service:payment-api"),
			exactMention("mention:workload:payment-prod", "workload", "payment-prod", "workload:payment-prod"),
		},
		Truth: doctruth.ServiceDeploymentTruth{
			ServiceID:      "service:payment-api",
			DeploymentRefs: []facts.DocumentationEvidenceRef{{Kind: "workload", ID: "workload:payment-prod", Confidence: facts.SourceConfidenceObserved}},
			EvidenceRefs:   []facts.DocumentationEvidenceRef{{Kind: "argocd_application", ID: "argocd:payments/payment-prod", Confidence: facts.SourceConfidenceObserved}},
			FreshnessState: doctruth.FreshnessFresh,
			ObservedAt:     time.Date(2026, 5, 9, 21, 40, 0, 0, time.UTC),
		},
	}})

	if got, want := len(findings), 1; got != want {
		t.Fatalf("findings len = %d, want %d", got, want)
	}
	finding := findings[0]
	if got, want := finding.FindingType, doctruth.FindingTypeServiceDeploymentDrift; got != want {
		t.Fatalf("FindingType = %q, want %q", got, want)
	}
	if got, want := finding.Status, doctruth.FindingStatusMatch; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := finding.TruthLevel, doctruth.TruthLevelExact; got != want {
		t.Fatalf("TruthLevel = %q, want %q", got, want)
	}
	if got, want := finding.FreshnessState, doctruth.FreshnessFresh; got != want {
		t.Fatalf("FreshnessState = %q, want %q", got, want)
	}
	if got, want := finding.ServiceID, "service:payment-api"; got != want {
		t.Fatalf("ServiceID = %q, want %q", got, want)
	}
	assertStrings(t, finding.DocumentedDeploymentIDs, []string{"workload:payment-prod"})
	assertStrings(t, finding.CurrentDeploymentIDs, []string{"workload:payment-prod"})
	if got, want := len(finding.EvidenceRefs), 2; got != want {
		t.Fatalf("EvidenceRefs len = %d, want %d", got, want)
	}
}

func TestDeploymentDriftAnalyzerReturnsConflictForMismatchedDeploymentTruth(t *testing.T) {
	t.Parallel()

	analyzer := doctruth.NewDeploymentDriftAnalyzer(doctruth.DriftOptions{})
	findings := analyzer.FindServiceDeploymentDrift(context.Background(), []doctruth.DeploymentDriftInput{{
		SourceSystem: "confluence",
		Claim:        deploymentClaim("payment-api", "payment-old"),
		Mentions: []facts.DocumentationEntityMentionPayload{
			exactMention("mention:service:payment-api", "service", "payment-api", "service:payment-api"),
			exactMention("mention:workload:payment-old", "workload", "payment-old", "workload:payment-old"),
		},
		Truth: doctruth.ServiceDeploymentTruth{
			ServiceID:      "service:payment-api",
			DeploymentRefs: []facts.DocumentationEvidenceRef{{Kind: "workload", ID: "workload:payment-prod", Confidence: facts.SourceConfidenceObserved}},
			EvidenceRefs:   []facts.DocumentationEvidenceRef{{Kind: "helm_release", ID: "helm:payment-prod", Confidence: facts.SourceConfidenceObserved}},
			FreshnessState: doctruth.FreshnessFresh,
		},
	}})

	if got, want := findings[0].Status, doctruth.FindingStatusConflict; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := findings[0].UnsupportedReason, ""; got != want {
		t.Fatalf("UnsupportedReason = %q, want empty", got)
	}
	assertStrings(t, findings[0].DocumentedDeploymentIDs, []string{"workload:payment-old"})
	assertStrings(t, findings[0].CurrentDeploymentIDs, []string{"workload:payment-prod"})
}

func TestDeploymentDriftAnalyzerClassifiesNonComparableStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		claim             facts.DocumentationClaimCandidatePayload
		mentions          []facts.DocumentationEntityMentionPayload
		truth             doctruth.ServiceDeploymentTruth
		wantStatus        doctruth.FindingStatus
		wantTruthLevel    doctruth.TruthLevel
		wantFreshness     doctruth.FreshnessState
		wantUnsupported   string
		wantAmbiguityNote string
	}{
		{
			name:            "missing graph truth is unsupported",
			claim:           deploymentClaim("payment-api", "payment-prod"),
			mentions:        exactDeploymentMentions("payment-api", "payment-prod"),
			truth:           doctruth.ServiceDeploymentTruth{ServiceID: "service:payment-api", FreshnessState: doctruth.FreshnessFresh},
			wantStatus:      doctruth.FindingStatusUnsupported,
			wantTruthLevel:  doctruth.TruthLevelDerived,
			wantFreshness:   doctruth.FreshnessFresh,
			wantUnsupported: "missing_graph_truth",
		},
		{
			name:  "missing subject mention is unsupported even when truth service is supplied",
			claim: deploymentClaim("payment-api", "payment-prod"),
			mentions: []facts.DocumentationEntityMentionPayload{
				exactMention("mention:workload:payment-prod", "workload", "payment-prod", "workload:payment-prod"),
			},
			truth:           deploymentTruth("service:payment-api", "workload:payment-prod", doctruth.FreshnessFresh),
			wantStatus:      doctruth.FindingStatusUnsupported,
			wantTruthLevel:  doctruth.TruthLevelDerived,
			wantFreshness:   doctruth.FreshnessFresh,
			wantUnsupported: "missing_service_identity",
		},
		{
			name:  "unmatched subject mention is unsupported even when truth service is supplied",
			claim: deploymentClaim("payment-api", "payment-prod"),
			mentions: []facts.DocumentationEntityMentionPayload{
				unmatchedMention("mention:service:payment-api", "service", "payment-api"),
				exactMention("mention:workload:payment-prod", "workload", "payment-prod", "workload:payment-prod"),
			},
			truth:           deploymentTruth("service:payment-api", "workload:payment-prod", doctruth.FreshnessFresh),
			wantStatus:      doctruth.FindingStatusUnsupported,
			wantTruthLevel:  doctruth.TruthLevelDerived,
			wantFreshness:   doctruth.FreshnessFresh,
			wantUnsupported: "missing_service_identity",
		},
		{
			name:              "ambiguous graph truth stays ambiguous",
			claim:             deploymentClaim("payment-api", "payment-prod"),
			mentions:          exactDeploymentMentions("payment-api", "payment-prod"),
			truth:             deploymentTruth("service:payment-api", "workload:payment-prod", doctruth.FreshnessFresh, "multiple deployment sources are equally current"),
			wantStatus:        doctruth.FindingStatusAmbiguous,
			wantTruthLevel:    doctruth.TruthLevelDerived,
			wantFreshness:     doctruth.FreshnessFresh,
			wantAmbiguityNote: "multiple deployment sources are equally current",
		},
		{
			name:  "ambiguous deployment mention stays ambiguous",
			claim: deploymentClaim("payment-api", "payment-prod"),
			mentions: []facts.DocumentationEntityMentionPayload{
				exactMention("mention:service:payment-api", "service", "payment-api", "service:payment-api"),
				ambiguousMention("mention:workload:payment-prod", "workload", "payment-prod", "workload:payment-prod-blue", "workload:payment-prod-green"),
			},
			truth:             deploymentTruth("service:payment-api", "workload:payment-prod-blue", doctruth.FreshnessFresh),
			wantStatus:        doctruth.FindingStatusAmbiguous,
			wantTruthLevel:    doctruth.TruthLevelDerived,
			wantFreshness:     doctruth.FreshnessFresh,
			wantAmbiguityNote: "ambiguous_deployment_mention",
		},
		{
			name:           "stale graph truth is explicit",
			claim:          deploymentClaim("payment-api", "payment-prod"),
			mentions:       exactDeploymentMentions("payment-api", "payment-prod"),
			truth:          deploymentTruth("service:payment-api", "workload:payment-prod", doctruth.FreshnessStale),
			wantStatus:     doctruth.FindingStatusStale,
			wantTruthLevel: doctruth.TruthLevelDerived,
			wantFreshness:  doctruth.FreshnessStale,
		},
		{
			name:           "building graph truth is explicit",
			claim:          deploymentClaim("payment-api", "payment-prod"),
			mentions:       exactDeploymentMentions("payment-api", "payment-prod"),
			truth:          deploymentTruth("service:payment-api", "workload:payment-prod", doctruth.FreshnessBuilding),
			wantStatus:     doctruth.FindingStatusBuilding,
			wantTruthLevel: doctruth.TruthLevelDerived,
			wantFreshness:  doctruth.FreshnessBuilding,
		},
		{
			name:            "non deployment claim is unsupported",
			claim:           ownershipClaim(),
			mentions:        exactDeploymentMentions("payment-api", "payment-prod"),
			truth:           deploymentTruth("service:payment-api", "workload:payment-prod", doctruth.FreshnessFresh),
			wantStatus:      doctruth.FindingStatusUnsupported,
			wantTruthLevel:  doctruth.TruthLevelDerived,
			wantFreshness:   doctruth.FreshnessFresh,
			wantUnsupported: "unsupported_claim_type",
		},
	}

	analyzer := doctruth.NewDeploymentDriftAnalyzer(doctruth.DriftOptions{})
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			findings := analyzer.FindServiceDeploymentDrift(context.Background(), []doctruth.DeploymentDriftInput{{
				SourceSystem: "confluence",
				Claim:        tt.claim,
				Mentions:     tt.mentions,
				Truth:        tt.truth,
			}})
			if got, want := findings[0].Status, tt.wantStatus; got != want {
				t.Fatalf("Status = %q, want %q", got, want)
			}
			if got, want := findings[0].TruthLevel, tt.wantTruthLevel; got != want {
				t.Fatalf("TruthLevel = %q, want %q", got, want)
			}
			if got, want := findings[0].FreshnessState, tt.wantFreshness; got != want {
				t.Fatalf("FreshnessState = %q, want %q", got, want)
			}
			if got, want := findings[0].UnsupportedReason, tt.wantUnsupported; got != want {
				t.Fatalf("UnsupportedReason = %q, want %q", got, want)
			}
			if tt.wantAmbiguityNote != "" {
				assertStrings(t, findings[0].AmbiguityReasons, []string{tt.wantAmbiguityNote})
			}
		})
	}
}

func TestDeploymentDriftAnalyzerRecordsMetricsAndStructuredLog(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("documentation-drift-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	var logs bytes.Buffer
	bootstrap, err := telemetry.NewBootstrap("documentation-drift-test")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v, want nil", err)
	}
	logger := telemetry.NewLoggerWithWriter(bootstrap, "documentation", "drift", &logs)
	analyzer := doctruth.NewDeploymentDriftAnalyzer(doctruth.DriftOptions{
		Instruments: instruments,
		Logger:      logger,
	})

	analyzer.FindServiceDeploymentDrift(context.Background(), []doctruth.DeploymentDriftInput{{
		SourceSystem: "confluence",
		Claim:        deploymentClaim("payment-api", "payment-old"),
		Mentions: []facts.DocumentationEntityMentionPayload{
			exactMention("mention:service:payment-api", "service", "payment-api", "service:payment-api"),
			exactMention("mention:workload:payment-old", "workload", "payment-old", "workload:payment-old"),
		},
		Truth: deploymentTruth("service:payment-api", "workload:payment-prod", doctruth.FreshnessFresh),
	}})

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	assertCounterValueWithAttrs(t, rm, "eshu_dp_documentation_drift_findings_total", map[string]string{
		"source_system": "confluence",
		"outcome":       string(doctruth.FindingStatusConflict),
	}, 1)
	assertHistogramCountWithAttrs(t, rm, "eshu_dp_documentation_drift_generation_duration_seconds", map[string]string{
		"source_system": "confluence",
		"outcome":       string(doctruth.FindingStatusConflict),
	}, 1)

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("json.Unmarshal(log) error = %v, want nil; log=%s", err, logs.String())
	}
	if got, want := entry["event_name"], "documentation.drift.completed"; got != want {
		t.Fatalf("event_name = %v, want %q", got, want)
	}
	if got, want := entry["finding_type"], string(doctruth.FindingTypeServiceDeploymentDrift); got != want {
		t.Fatalf("finding_type = %v, want %q", got, want)
	}
	if got, want := entry["conflicts"], float64(1); got != want {
		t.Fatalf("conflicts = %v, want %v", got, want)
	}
}

func TestDeploymentDriftAnalyzerSkipsDurationMetricForEmptyInput(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("documentation-drift-empty-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	analyzer := doctruth.NewDeploymentDriftAnalyzer(doctruth.DriftOptions{Instruments: instruments})

	findings := analyzer.FindServiceDeploymentDrift(context.Background(), nil)
	if got := len(findings); got != 0 {
		t.Fatalf("findings len = %d, want 0", got)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	assertHistogramPointCount(t, rm, "eshu_dp_documentation_drift_generation_duration_seconds", 0)
}

func deploymentClaim(serviceName, deploymentName string) facts.DocumentationClaimCandidatePayload {
	return facts.DocumentationClaimCandidatePayload{
		DocumentID:       "doc:payments-runbook",
		RevisionID:       "rev:2026-05-09",
		SectionID:        "section:deployment",
		ClaimID:          "claim:deployment:" + serviceName,
		ClaimType:        doctruth.ClaimTypeServiceDeployment,
		ClaimText:        serviceName + " deploys through " + deploymentName + ".",
		ClaimHash:        "sha256:claim",
		ExcerptHash:      "sha256:excerpt",
		SubjectMentionID: "mention:service:" + serviceName,
		ObjectMentionIDs: []string{"mention:workload:" + deploymentName},
		Authority:        facts.DocumentationClaimAuthorityDocumentEvidence,
		EvidenceRefs: []facts.DocumentationEvidenceRef{{
			Kind:       "document_section",
			ID:         "section:deployment",
			URI:        "https://confluence.example/pages/payments#deployment",
			Confidence: facts.SourceConfidenceObserved,
		}},
	}
}

func ownershipClaim() facts.DocumentationClaimCandidatePayload {
	claim := deploymentClaim("payment-api", "payment-prod")
	claim.ClaimID = "claim:owner:payment-api"
	claim.ClaimType = "service_ownership"
	return claim
}

func exactDeploymentMentions(serviceName, deploymentName string) []facts.DocumentationEntityMentionPayload {
	return []facts.DocumentationEntityMentionPayload{
		exactMention("mention:service:"+serviceName, "service", serviceName, "service:"+serviceName),
		exactMention("mention:workload:"+deploymentName, "workload", deploymentName, "workload:"+deploymentName),
	}
}

func exactMention(mentionID, kind, text, entityID string) facts.DocumentationEntityMentionPayload {
	return facts.DocumentationEntityMentionPayload{
		DocumentID:       "doc:payments-runbook",
		RevisionID:       "rev:2026-05-09",
		SectionID:        "section:deployment",
		MentionID:        mentionID,
		MentionText:      text,
		MentionKind:      kind,
		ResolutionStatus: facts.DocumentationMentionResolutionExact,
		CandidateRefs: []facts.DocumentationEvidenceRef{{
			Kind:       kind,
			ID:         entityID,
			Confidence: facts.SourceConfidenceDerived,
		}},
		ExcerptHash: "sha256:excerpt",
	}
}

func unmatchedMention(mentionID, kind, text string) facts.DocumentationEntityMentionPayload {
	mention := exactMention(mentionID, kind, text, "")
	mention.ResolutionStatus = facts.DocumentationMentionResolutionUnmatched
	mention.CandidateRefs = nil
	return mention
}

func ambiguousMention(mentionID, kind, text string, entityIDs ...string) facts.DocumentationEntityMentionPayload {
	mention := exactMention(mentionID, kind, text, "")
	mention.ResolutionStatus = facts.DocumentationMentionResolutionAmbiguous
	mention.CandidateRefs = mention.CandidateRefs[:0]
	for _, entityID := range entityIDs {
		mention.CandidateRefs = append(mention.CandidateRefs, facts.DocumentationEvidenceRef{
			Kind:       kind,
			ID:         entityID,
			Confidence: facts.SourceConfidenceDerived,
		})
	}
	return mention
}

func deploymentTruth(serviceID, deploymentID string, freshness doctruth.FreshnessState, ambiguityReasons ...string) doctruth.ServiceDeploymentTruth {
	return doctruth.ServiceDeploymentTruth{
		ServiceID:        serviceID,
		DeploymentRefs:   []facts.DocumentationEvidenceRef{{Kind: "workload", ID: deploymentID, Confidence: facts.SourceConfidenceObserved}},
		EvidenceRefs:     []facts.DocumentationEvidenceRef{{Kind: "kubernetes_workload", ID: deploymentID, Confidence: facts.SourceConfidenceObserved}},
		FreshnessState:   freshness,
		AmbiguityReasons: ambiguityReasons,
	}
}

func assertStrings(t *testing.T, got []string, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("strings len = %d, want %d; got=%v want=%v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("strings[%d] = %q, want %q; got=%v want=%v", i, got[i], want[i], got, want)
		}
	}
}

func assertHistogramCountWithAttrs(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
	want uint64,
) {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, metricRecord := range sm.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Histogram[float64]", metricName, metricRecord.Data)
			}
			for _, point := range histogram.DataPoints {
				if !hasMetricAttrs(point.Attributes.ToSlice(), wantAttrs) {
					continue
				}
				if point.Count != want {
					t.Fatalf("%s count = %d, want %d", metricName, point.Count, want)
				}
				return
			}
		}
	}
	t.Fatalf("metric %q with attrs %#v not found", metricName, wantAttrs)
}

func assertHistogramPointCount(t *testing.T, rm metricdata.ResourceMetrics, metricName string, want uint64) {
	t.Helper()

	var got uint64
	for _, sm := range rm.ScopeMetrics {
		for _, metricRecord := range sm.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Histogram[float64]", metricName, metricRecord.Data)
			}
			for _, point := range histogram.DataPoints {
				got += point.Count
			}
		}
	}
	if got != want {
		t.Fatalf("%s point count = %d, want %d", metricName, got, want)
	}
}
