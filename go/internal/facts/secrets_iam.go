package facts

import "slices"

const (
	// AWSIAMPrincipalFactKind identifies one AWS IAM principal source fact.
	AWSIAMPrincipalFactKind = "aws_iam_principal"
	// AWSIAMTrustPolicyFactKind identifies one normalized AWS IAM role trust
	// policy statement source fact.
	AWSIAMTrustPolicyFactKind = "aws_iam_trust_policy"
	// AWSIAMPermissionPolicyFactKind identifies one normalized AWS IAM identity
	// permission policy statement source fact.
	AWSIAMPermissionPolicyFactKind = "aws_iam_permission_policy"
	// AWSIAMPolicyAttachmentFactKind identifies one managed policy attachment to
	// an IAM principal.
	AWSIAMPolicyAttachmentFactKind = "aws_iam_policy_attachment"
	// AWSIAMPermissionBoundaryFactKind identifies one permissions boundary
	// attached to an IAM principal.
	AWSIAMPermissionBoundaryFactKind = "aws_iam_permission_boundary"
	// AWSIAMInstanceProfileFactKind identifies one IAM instance profile source
	// fact.
	AWSIAMInstanceProfileFactKind = "aws_iam_instance_profile"
	// AWSIAMAccessAnalyzerFindingFactKind identifies one optional AWS IAM Access
	// Analyzer finding source fact.
	AWSIAMAccessAnalyzerFindingFactKind = "aws_iam_access_analyzer_finding"
	// SecretsIAMCoverageWarningFactKind identifies source-local coverage,
	// redaction, unsupported, partial, permission-hidden, rate-limited, or stale
	// warning evidence for the secrets/IAM posture collector family.
	SecretsIAMCoverageWarningFactKind = "secrets_iam_coverage_warning"

	// SecretsIAMSchemaVersionV1 is the first secrets/IAM posture source schema.
	SecretsIAMSchemaVersionV1 = "1.0.0"
)

var secretsIAMFactKinds = []string{
	AWSIAMPrincipalFactKind,
	AWSIAMTrustPolicyFactKind,
	AWSIAMPermissionPolicyFactKind,
	AWSIAMPolicyAttachmentFactKind,
	AWSIAMPermissionBoundaryFactKind,
	AWSIAMInstanceProfileFactKind,
	AWSIAMAccessAnalyzerFindingFactKind,
	SecretsIAMCoverageWarningFactKind,
}

var secretsIAMSchemaVersions = map[string]string{
	AWSIAMPrincipalFactKind:             SecretsIAMSchemaVersionV1,
	AWSIAMTrustPolicyFactKind:           SecretsIAMSchemaVersionV1,
	AWSIAMPermissionPolicyFactKind:      SecretsIAMSchemaVersionV1,
	AWSIAMPolicyAttachmentFactKind:      SecretsIAMSchemaVersionV1,
	AWSIAMPermissionBoundaryFactKind:    SecretsIAMSchemaVersionV1,
	AWSIAMInstanceProfileFactKind:       SecretsIAMSchemaVersionV1,
	AWSIAMAccessAnalyzerFindingFactKind: SecretsIAMSchemaVersionV1,
	SecretsIAMCoverageWarningFactKind:   SecretsIAMSchemaVersionV1,
}

// SecretsIAMFactKinds returns the accepted secrets/IAM posture source fact
// kinds in source-contract order.
func SecretsIAMFactKinds() []string {
	return slices.Clone(secretsIAMFactKinds)
}

// SecretsIAMSchemaVersion returns the schema version for a secrets/IAM posture
// source fact kind.
func SecretsIAMSchemaVersion(factKind string) (string, bool) {
	version, ok := secretsIAMSchemaVersions[factKind]
	return version, ok
}
