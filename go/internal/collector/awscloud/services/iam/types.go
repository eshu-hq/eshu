package iam

// Role is the scanner-owned representation of an IAM role.
type Role struct {
	ARN                string
	Name               string
	Path               string
	AssumeRolePolicy   map[string]any
	TrustPrincipals    []TrustPrincipal
	AttachedPolicyARNs []string
	InlinePolicyNames  []string
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

// TrustPrincipal identifies one principal granted access by a role trust
// policy.
type TrustPrincipal struct {
	Type       string
	Identifier string
}
