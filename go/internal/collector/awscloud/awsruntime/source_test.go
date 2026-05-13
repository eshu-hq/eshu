package awsruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceScansMatchedIAMClaim(t *testing.T) {
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	item := awsWorkItem(now)
	lease := &stubCredentialLease{}
	provider := &stubCredentialProvider{lease: lease}
	factory := &stubScannerFactory{scanner: stubScanner{envelopes: []facts.Envelope{
		{
			FactID:           "fact-1",
			ScopeID:          item.ScopeID,
			GenerationID:     item.GenerationID,
			FactKind:         facts.AWSResourceFactKind,
			StableFactKey:    "resource-1",
			SchemaVersion:    facts.AWSResourceSchemaVersion,
			CollectorKind:    awscloud.CollectorKind,
			FencingToken:     item.CurrentFencingToken,
			SourceConfidence: facts.SourceConfidenceReported,
			ObservedAt:       now,
		},
	}}}
	source := ClaimedSource{
		Config: Config{
			CollectorInstanceID: item.CollectorInstanceID,
			Targets: []TargetScope{{
				AccountID:       "123456789012",
				AllowedRegions:  []string{"us-east-1"},
				AllowedServices: []string{awscloud.ServiceIAM},
				Credentials: CredentialConfig{
					Mode:       CredentialModeCentralAssumeRole,
					RoleARN:    "arn:aws:iam::123456789012:role/eshu-readonly",
					ExternalID: "external-1",
				},
			}},
		},
		Credentials: provider,
		Scanners:    factory,
		Clock:       func() time.Time { return now },
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if !ok {
		t.Fatalf("NextClaimed() ok = false, want true")
	}
	if collected.Scope.ScopeID != item.ScopeID {
		t.Fatalf("ScopeID = %q, want %q", collected.Scope.ScopeID, item.ScopeID)
	}
	if collected.Scope.CollectorKind != scope.CollectorAWS {
		t.Fatalf("CollectorKind = %q, want %q", collected.Scope.CollectorKind, scope.CollectorAWS)
	}
	if collected.Generation.GenerationID != item.GenerationID {
		t.Fatalf("GenerationID = %q, want %q", collected.Generation.GenerationID, item.GenerationID)
	}
	if got := drainFacts(t, collected.Facts); len(got) != 1 {
		t.Fatalf("fact count = %d, want 1", len(got))
	}
	if !lease.released {
		t.Fatalf("credential lease was not released")
	}
	if provider.target.Credentials.ExternalID != "external-1" {
		t.Fatalf("external ID = %q, want external-1", provider.target.Credentials.ExternalID)
	}
	if factory.boundary.FencingToken != item.CurrentFencingToken {
		t.Fatalf("boundary fencing token = %d, want %d", factory.boundary.FencingToken, item.CurrentFencingToken)
	}
}

func TestClaimedSourceEmitsWarningWhenCredentialsFail(t *testing.T) {
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	item := awsWorkItem(now)
	source := ClaimedSource{
		Config: Config{
			CollectorInstanceID: item.CollectorInstanceID,
			Targets: []TargetScope{{
				AccountID:       "123456789012",
				AllowedRegions:  []string{"us-east-1"},
				AllowedServices: []string{awscloud.ServiceIAM},
				Credentials: CredentialConfig{
					Mode:       CredentialModeCentralAssumeRole,
					RoleARN:    "arn:aws:iam::123456789012:role/eshu-readonly",
					ExternalID: "external-1",
				},
			}},
		},
		Credentials: &stubCredentialProvider{err: errors.New("sts denied")},
		Scanners:    &stubScannerFactory{scanner: stubScanner{}},
		Clock:       func() time.Time { return now },
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if !ok {
		t.Fatalf("NextClaimed() ok = false, want true")
	}
	envelopes := drainFacts(t, collected.Facts)
	if len(envelopes) != 1 {
		t.Fatalf("fact count = %d, want 1", len(envelopes))
	}
	warning := envelopes[0]
	if warning.FactKind != facts.AWSWarningFactKind {
		t.Fatalf("FactKind = %q, want %q", warning.FactKind, facts.AWSWarningFactKind)
	}
	if got := warning.Payload["warning_kind"]; got != WarningAssumeRoleFailed {
		t.Fatalf("warning_kind = %v, want %q", got, WarningAssumeRoleFailed)
	}
	if got := warning.FencingToken; got != item.CurrentFencingToken {
		t.Fatalf("FencingToken = %d, want %d", got, item.CurrentFencingToken)
	}
}

func TestClaimedSourceRejectsUnauthorizedClaimTarget(t *testing.T) {
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	item := awsWorkItem(now)
	item.AcceptanceUnitID = `{"account_id":"999999999999","region":"us-east-1","service_kind":"iam"}`
	source := ClaimedSource{
		Config: Config{
			CollectorInstanceID: item.CollectorInstanceID,
			Targets: []TargetScope{{
				AccountID:       "123456789012",
				AllowedRegions:  []string{"us-east-1"},
				AllowedServices: []string{awscloud.ServiceIAM},
			}},
		},
		Credentials: &stubCredentialProvider{},
		Scanners:    &stubScannerFactory{scanner: stubScanner{}},
		Clock:       func() time.Time { return now },
	}

	_, _, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatalf("NextClaimed() error = nil, want unauthorized target error")
	}
}

func awsWorkItem(now time.Time) workflow.WorkItem {
	return workflow.WorkItem{
		WorkItemID:          "aws:collector-1:run-1:123456789012:us-east-1:iam",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorAWS,
		CollectorInstanceID: "collector-1",
		SourceSystem:        string(scope.CollectorAWS),
		ScopeID:             "aws:123456789012:us-east-1:iam",
		AcceptanceUnitID:    `{"account_id":"123456789012","region":"us-east-1","service_kind":"iam"}`,
		SourceRunID:         "aws-generation-1",
		GenerationID:        "aws-generation-1",
		FairnessKey:         "aws:123456789012",
		Status:              workflow.WorkItemStatusPending,
		CurrentFencingToken: 42,
		LeaseExpiresAt:      now.Add(5 * time.Minute),
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func drainFacts(t *testing.T, input <-chan facts.Envelope) []facts.Envelope {
	t.Helper()
	var output []facts.Envelope
	for envelope := range input {
		output = append(output, envelope)
	}
	return output
}

type stubCredentialProvider struct {
	lease  CredentialLease
	target Target
	err    error
}

func (p *stubCredentialProvider) Acquire(
	_ context.Context,
	target Target,
	_ time.Time,
) (CredentialLease, error) {
	p.target = target
	if p.err != nil {
		return nil, p.err
	}
	return p.lease, nil
}

type stubCredentialLease struct {
	released bool
}

func (l *stubCredentialLease) Release() error {
	l.released = true
	return nil
}

type stubScannerFactory struct {
	scanner  ServiceScanner
	target   Target
	boundary awscloud.Boundary
}

func (f *stubScannerFactory) Scanner(
	_ context.Context,
	target Target,
	boundary awscloud.Boundary,
	_ CredentialLease,
) (ServiceScanner, error) {
	f.target = target
	f.boundary = boundary
	return f.scanner, nil
}

type stubScanner struct {
	envelopes []facts.Envelope
	err       error
}

func (s stubScanner) Scan(context.Context, awscloud.Boundary) ([]facts.Envelope, error) {
	return s.envelopes, s.err
}
