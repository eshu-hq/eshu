package awscloud

const (
	// ServiceIAM identifies the global IAM service scan slice.
	ServiceIAM = "iam"
)

const (
	// ResourceTypeIAMRole identifies an IAM role.
	ResourceTypeIAMRole = "aws_iam_role"
	// ResourceTypeIAMUser identifies an IAM user.
	ResourceTypeIAMUser = "aws_iam_user"
	// ResourceTypeIAMGroup identifies an IAM group.
	ResourceTypeIAMGroup = "aws_iam_group"
	// ResourceTypeIAMPolicy identifies an IAM policy.
	ResourceTypeIAMPolicy = "aws_iam_policy"
	// ResourceTypeIAMInstanceProfile identifies an IAM instance profile.
	ResourceTypeIAMInstanceProfile = "aws_iam_instance_profile"
	// ResourceTypeIAMPrincipal identifies a principal from an IAM trust policy.
	ResourceTypeIAMPrincipal = "aws_iam_principal"
)

const (
	// IAMPolicySourceInline marks a derived permission from an inline policy
	// embedded on a role, user, or group.
	IAMPolicySourceInline = "inline"
	// IAMPolicySourceAttachedManaged marks a derived permission from an attached
	// managed policy document (customer- or AWS-managed).
	IAMPolicySourceAttachedManaged = "attached_managed"
	// IAMPolicySourceTrust marks a derived permission from a role trust /
	// assume-role policy document.
	IAMPolicySourceTrust = "trust"
	// IAMPolicySourceBoundary marks a derived permission from a principal's
	// permission-boundary policy document. The statement is a ceiling, not a grant:
	// the reducer intersects identity-policy allows with boundary allows before
	// promoting a CAN_PERFORM edge (issue #1331). It carries the same metadata-only
	// shape as any other aws_iam_permission statement; no raw policy JSON or
	// condition values are emitted.
	IAMPolicySourceBoundary = "boundary"
)

const (
	// RelationshipIAMRoleTrustsPrincipal records a role trust-policy principal.
	RelationshipIAMRoleTrustsPrincipal = "iam_role_trusts_principal"
	// RelationshipIAMRoleAttachedPolicy records a managed policy attachment.
	RelationshipIAMRoleAttachedPolicy = "iam_role_attached_policy"
	// RelationshipIAMRoleInInstanceProfile records a role/profile membership.
	RelationshipIAMRoleInInstanceProfile = "iam_role_in_instance_profile"
)
