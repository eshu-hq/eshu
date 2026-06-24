// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import "time"

const (
	// CollectorKind is the durable collector_kind for secrets/IAM posture facts.
	CollectorKind = "secrets_iam_posture"
	// ProviderAWSIAM identifies AWS IAM as the provider-native source.
	ProviderAWSIAM = "aws_iam"
	// RedactionPolicyVersion identifies the redaction contract used for this
	// source fact family.
	RedactionPolicyVersion = "secrets_iam_posture_v1"

	// PrincipalTypeAWSRole identifies an AWS IAM role principal.
	PrincipalTypeAWSRole = "aws_iam_role"
	// PrincipalTypeAWSUser identifies an AWS IAM user principal.
	PrincipalTypeAWSUser = "aws_iam_user"
	// PrincipalTypeAWSOIDCProvider identifies an AWS IAM OIDC provider.
	PrincipalTypeAWSOIDCProvider = "aws_iam_oidc_provider"

	// PolicySourceInline marks a statement from an inline identity policy.
	PolicySourceInline = "inline"
	// PolicySourceAttachedManaged marks a statement from an attached managed
	// policy document.
	PolicySourceAttachedManaged = "attached_managed"
	// PolicySourceTrust marks a statement from an IAM role trust policy.
	PolicySourceTrust = "trust"
	// PolicySourcePermissionBoundary marks a statement from a managed policy
	// attached as a permissions boundary. It is a ceiling on identity grants, not
	// an attached managed identity-policy grant.
	PolicySourcePermissionBoundary = "permission_boundary"

	// SourceStateUnsupported marks evidence the collector cannot observe in the
	// current runtime slice.
	SourceStateUnsupported = "unsupported"
	// SourceStatePartial marks source evidence that was only partly observed.
	SourceStatePartial = "partial"
	// SourceStatePermissionHidden marks source evidence hidden by IAM
	// permissions.
	SourceStatePermissionHidden = "permission_hidden"
	// SourceStateRateLimited marks source evidence skipped because the provider
	// rate-limited the collector.
	SourceStateRateLimited = "rate_limited"
	// SourceStateStale marks source evidence known to be stale.
	SourceStateStale = "stale"
)

// EnvelopeContext carries Eshu fact boundary fields for one secrets/IAM source
// observation.
type EnvelopeContext struct {
	AccountID           string
	Region              string
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// PrincipalObservation describes one provider-native AWS IAM principal source
// identity. It preserves join identity without carrying credential material.
type PrincipalObservation struct {
	Context          EnvelopeContext
	PrincipalARN     string
	PrincipalType    string
	Name             string
	Path             string
	URLFingerprint   string
	ClientIDCount    int
	ThumbprintCount  int
	SourceURI        string
	SourceRecordID   string
	CorrelationHints []string
}

// TrustPolicyObservation describes one normalized IAM role trust policy
// statement. It carries condition summaries and assume principals, never raw
// policy JSON or condition values.
type TrustPolicyObservation struct {
	Context                        EnvelopeContext
	RoleARN                        string
	StatementSID                   string
	Effect                         string
	Actions                        []string
	ConditionKeys                  []string
	ConditionOperators             []string
	AssumePrincipals               []string
	WebIdentitySubjectFingerprints []string
	WebIdentitySubjectWildcard     bool
	SourceURI                      string
	SourceRecordID                 string
}

// PermissionPolicyObservation describes one normalized IAM identity policy
// statement. It carries action/resource patterns and condition summaries, never
// raw policy JSON or condition values.
type PermissionPolicyObservation struct {
	Context            EnvelopeContext
	PrincipalARN       string
	PrincipalType      string
	PolicySource       string
	PolicyARN          string
	PolicyName         string
	StatementSID       string
	Effect             string
	Actions            []string
	NotActions         []string
	Resources          []string
	NotResources       []string
	ConditionKeys      []string
	ConditionOperators []string
	SourceURI          string
	SourceRecordID     string
}

// PolicyAttachmentObservation describes one managed IAM policy attachment.
type PolicyAttachmentObservation struct {
	Context        EnvelopeContext
	PrincipalARN   string
	PrincipalType  string
	PolicyARN      string
	PolicyName     string
	PolicySource   string
	SourceURI      string
	SourceRecordID string
}

// PermissionBoundaryObservation describes one permissions boundary attached to
// an IAM principal.
type PermissionBoundaryObservation struct {
	Context           EnvelopeContext
	PrincipalARN      string
	PrincipalType     string
	BoundaryPolicyARN string
	BoundaryType      string
	SourceURI         string
	SourceRecordID    string
}

// InstanceProfileObservation describes one IAM instance profile and its role
// membership identity.
type InstanceProfileObservation struct {
	Context        EnvelopeContext
	ProfileARN     string
	Name           string
	Path           string
	RoleARNs       []string
	SourceURI      string
	SourceRecordID string
}

// AccessAnalyzerFindingObservation describes optional Access Analyzer finding
// metadata without embedding finding bodies.
type AccessAnalyzerFindingObservation struct {
	Context        EnvelopeContext
	FindingID      string
	AnalyzerARN    string
	ResourceARN    string
	ResourceType   string
	Status         string
	FindingType    string
	ConditionKeys  []string
	SourceURI      string
	SourceRecordID string
}

// CoverageWarningObservation describes explicit source-local coverage state for
// partial, hidden, unsupported, rate-limited, or stale collection.
type CoverageWarningObservation struct {
	Context        EnvelopeContext
	WarningKind    string
	SourceState    string
	ErrorClass     string
	Message        string
	Attributes     map[string]any
	SourceURI      string
	SourceRecordID string
}
