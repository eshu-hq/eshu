package secretsiam

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestNewPermissionPolicyEnvelopeRedactsPolicyBodyAndConditionValues(t *testing.T) {
	ctx := testContext()
	env, err := NewPermissionPolicyEnvelope(PermissionPolicyObservation{
		Context:       ctx,
		PrincipalARN:  "arn:aws:iam::123456789012:role/eshu-runtime",
		PrincipalType: PrincipalTypeAWSRole,
		PolicySource:  PolicySourceInline,
		PolicyName:    "inline-escalate",
		StatementSID:  "AllowPassRole",
		Effect:        "allow",
		Actions:       []string{"iam:PassRole", " sts:AssumeRole ", "iam:passrole"},
		Resources:     []string{"arn:aws:iam::123456789012:role/*"},
		ConditionKeys: []string{"aws:SourceIp", "aws:SourceIp"},
		ConditionOperators: []string{
			"StringEquals",
			" IpAddress ",
			"StringEquals",
		},
	})
	if err != nil {
		t.Fatalf("NewPermissionPolicyEnvelope() error = %v", err)
	}
	if env.FactKind != facts.AWSIAMPermissionPolicyFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.AWSIAMPermissionPolicyFactKind)
	}
	if env.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", env.CollectorKind, CollectorKind)
	}
	if env.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want %q", env.SourceConfidence, facts.SourceConfidenceReported)
	}
	assertPayloadString(t, env.Payload, "redaction_policy_version", RedactionPolicyVersion)
	assertPayloadString(t, env.Payload, "effect", "Allow")

	actions, ok := env.Payload["actions"].([]string)
	if !ok {
		t.Fatalf("actions = %T, want []string", env.Payload["actions"])
	}
	wantActions := []string{"iam:passrole", "sts:assumerole"}
	if len(actions) != len(wantActions) {
		t.Fatalf("actions = %v, want %v", actions, wantActions)
	}
	for index, want := range wantActions {
		if actions[index] != want {
			t.Fatalf("actions[%d] = %q, want %q", index, actions[index], want)
		}
	}

	if got, _ := env.Payload["has_conditions"].(bool); !got {
		t.Fatalf("has_conditions = %v, want true", got)
	}
	keys, ok := env.Payload["condition_keys"].([]string)
	if !ok {
		t.Fatalf("condition_keys = %T, want []string", env.Payload["condition_keys"])
	}
	if len(keys) != 1 || keys[0] != "aws:SourceIp" {
		t.Fatalf("condition_keys = %v, want [aws:SourceIp]", keys)
	}
	operators, ok := env.Payload["condition_operators"].([]string)
	if !ok {
		t.Fatalf("condition_operators = %T, want []string", env.Payload["condition_operators"])
	}
	if len(operators) != 2 || operators[0] != "IpAddress" || operators[1] != "StringEquals" {
		t.Fatalf("condition_operators = %v, want [IpAddress StringEquals]", operators)
	}
	if got, _ := env.Payload["condition_operator_count"].(int); got != 2 {
		t.Fatalf("condition_operator_count = %v, want 2", got)
	}
	assertNoForbiddenPayloadKeys(t, env.Payload)
}

func TestNewPrincipalBoundaryAndAttachmentEnvelopes(t *testing.T) {
	ctx := testContext()
	principal, err := NewPrincipalEnvelope(PrincipalObservation{
		Context:       ctx,
		PrincipalARN:  "arn:aws:iam::123456789012:role/eshu-runtime",
		PrincipalType: PrincipalTypeAWSRole,
		Name:          "eshu-runtime",
		Path:          "/service/",
	})
	if err != nil {
		t.Fatalf("NewPrincipalEnvelope() error = %v", err)
	}
	assertFact(t, principal, facts.AWSIAMPrincipalFactKind)

	boundary, err := NewPermissionBoundaryEnvelope(PermissionBoundaryObservation{
		Context:           ctx,
		PrincipalARN:      "arn:aws:iam::123456789012:role/eshu-runtime",
		PrincipalType:     PrincipalTypeAWSRole,
		BoundaryPolicyARN: "arn:aws:iam::123456789012:policy/developer-boundary",
		BoundaryType:      "PermissionsBoundaryPolicy",
	})
	if err != nil {
		t.Fatalf("NewPermissionBoundaryEnvelope() error = %v", err)
	}
	assertFact(t, boundary, facts.AWSIAMPermissionBoundaryFactKind)

	attachment, err := NewPolicyAttachmentEnvelope(PolicyAttachmentObservation{
		Context:       ctx,
		PrincipalARN:  "arn:aws:iam::123456789012:role/eshu-runtime",
		PrincipalType: PrincipalTypeAWSRole,
		PolicyARN:     "arn:aws:iam::123456789012:policy/eshu-read",
		PolicySource:  PolicySourceAttachedManaged,
	})
	if err != nil {
		t.Fatalf("NewPolicyAttachmentEnvelope() error = %v", err)
	}
	assertFact(t, attachment, facts.AWSIAMPolicyAttachmentFactKind)
}

func TestNewAccessAnalyzerAndCoverageWarningEnvelopes(t *testing.T) {
	ctx := testContext()
	finding, err := NewAccessAnalyzerFindingEnvelope(AccessAnalyzerFindingObservation{
		Context:       ctx,
		FindingID:     "finding-123",
		AnalyzerARN:   "arn:aws:access-analyzer:us-east-1:123456789012:analyzer/account",
		ResourceARN:   "arn:aws:iam::123456789012:role/eshu-runtime",
		ResourceType:  PrincipalTypeAWSRole,
		Status:        "ACTIVE",
		FindingType:   "ExternalAccess",
		ConditionKeys: []string{"aws:PrincipalOrgID"},
	})
	if err != nil {
		t.Fatalf("NewAccessAnalyzerFindingEnvelope() error = %v", err)
	}
	assertFact(t, finding, facts.AWSIAMAccessAnalyzerFindingFactKind)
	assertNoForbiddenPayloadKeys(t, finding.Payload)

	warning, err := NewCoverageWarningEnvelope(CoverageWarningObservation{
		Context:     ctx,
		WarningKind: "access_analyzer_not_enabled",
		SourceState: SourceStateUnsupported,
		ErrorClass:  "unsupported_source",
		Message:     "Access Analyzer source facts are not enabled for this fixture",
	})
	if err != nil {
		t.Fatalf("NewCoverageWarningEnvelope() error = %v", err)
	}
	assertFact(t, warning, facts.SecretsIAMCoverageWarningFactKind)
	assertPayloadString(t, warning.Payload, "source_state", SourceStateUnsupported)
}

func testContext() EnvelopeContext {
	return EnvelopeContext{
		AccountID:           "123456789012",
		Region:              "aws-global",
		ScopeID:             "aws:123456789012:aws-global",
		GenerationID:        "aws:123456789012:aws-global:iam:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
	}
}

func assertFact(t *testing.T, env facts.Envelope, factKind string) {
	t.Helper()
	if env.FactKind != factKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, factKind)
	}
	if env.SchemaVersion != facts.SecretsIAMSchemaVersionV1 {
		t.Fatalf("SchemaVersion = %q, want %q", env.SchemaVersion, facts.SecretsIAMSchemaVersionV1)
	}
	if env.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", env.CollectorKind, CollectorKind)
	}
}

func assertPayloadString(t *testing.T, payload map[string]any, key, want string) {
	t.Helper()
	got, ok := payload[key].(string)
	if !ok || got != want {
		t.Fatalf("payload[%q] = %#v, want %q", key, payload[key], want)
	}
}

func assertNoForbiddenPayloadKeys(t *testing.T, payload map[string]any) {
	t.Helper()
	forbidden := []string{
		"Statement",
		"policy_document",
		"raw_policy",
		"document",
		"condition_values",
		"aws_secret_access_key",
		"aws_session_token",
	}
	for _, key := range forbidden {
		if _, ok := payload[key]; ok {
			t.Fatalf("payload carries forbidden key %q: %#v", key, payload)
		}
	}
}
