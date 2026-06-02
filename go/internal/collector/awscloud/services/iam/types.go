package iam

// Policy-statement source kinds. They classify which document an emitted derived
// permission statement came from so downstream posture analysis can distinguish
// an inline grant from an attached managed policy or a role trust policy. They
// mirror the awscloud.IAMPolicySource* constants the envelope builder accepts.
const (
	// PolicySourceInline marks a statement from an inline policy embedded on a
	// role, user, or group.
	PolicySourceInline = "inline"
	// PolicySourceAttachedManaged marks a statement from an attached managed
	// policy document (customer- or AWS-managed).
	PolicySourceAttachedManaged = "attached_managed"
	// PolicySourceTrust marks a statement from a role trust / assume-role policy.
	PolicySourceTrust = "trust"
)

// Role is the scanner-owned representation of an IAM role.
type Role struct {
	ARN                string
	Name               string
	Path               string
	AssumeRolePolicy   map[string]any
	TrustPrincipals    []TrustPrincipal
	PermissionBoundary PermissionBoundary
	AttachedPolicyARNs []string
	InlinePolicyNames  []string
	// PermissionStatements are the normalized, metadata-only statements derived
	// from this role's trust, inline, and attached managed policy documents. The
	// adapter normalizes them at the SDK boundary; this package never holds the
	// raw policy JSON.
	PermissionStatements []PolicyStatement
}

// User is the scanner-owned representation of an IAM user principal.
type User struct {
	ARN                string
	Name               string
	Path               string
	PermissionBoundary PermissionBoundary
	AttachedPolicyARNs []string
	InlinePolicyNames  []string
	// PermissionStatements are the normalized, metadata-only statements derived
	// from this user's inline and attached managed policy documents.
	PermissionStatements []PolicyStatement
}

// PolicyStatement is the scanner-owned, normalized projection of a single IAM
// policy statement. It carries identifiers, the action/resource patterns, and a
// condition-key summary only. It deliberately holds no raw policy JSON body and
// no condition values.
type PolicyStatement struct {
	Source        string
	PolicyARN     string
	PolicyName    string
	StatementSID  string
	Effect        string
	Actions       []string
	NotActions    []string
	Resources     []string
	NotResources  []string
	ConditionKeys []string
	// AssumePrincipals lists the principals a trust statement grants assume-role
	// to. It is only set when Source is PolicySourceTrust.
	AssumePrincipals []string
}

// Policy is the scanner-owned representation of an IAM managed policy.
type Policy struct {
	ARN              string
	Name             string
	Path             string
	DefaultVersionID string
	AttachmentCount  int32
}

// InstanceProfile is the scanner-owned representation of an IAM instance
// profile.
type InstanceProfile struct {
	ARN      string
	Name     string
	Path     string
	RoleARNs []string
}

// CoverageWarning describes explicit source-local IAM coverage state that
// should be persisted as secrets_iam_coverage_warning evidence.
type CoverageWarning struct {
	WarningKind string
	SourceState string
	ErrorClass  string
	Message     string
	Attributes  map[string]any
}

// PermissionBoundary is the scanner-owned representation of one IAM
// permissions boundary attachment to a role or user.
type PermissionBoundary struct {
	PolicyARN string
	Type      string
}

// OIDCProvider is the scanner-owned representation of an IAM OpenID Connect
// identity provider. URLFingerprint is deterministic source identity for the
// provider URL; the raw URL stays out of secrets/IAM posture facts.
type OIDCProvider struct {
	ARN             string
	URLFingerprint  string
	ClientIDCount   int
	ThumbprintCount int
}

// TrustPrincipal identifies one principal granted access by a role trust
// policy.
type TrustPrincipal struct {
	Type       string
	Identifier string
}
