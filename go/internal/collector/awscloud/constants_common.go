package awscloud

// Shared resource-type constants that are not owned by a single AWS service
// slice. These appear as relationship targets across multiple service scanners
// and therefore live in the common constants file rather than in a per-service
// file.

const (
	// ResourceTypeAWSAccount identifies an AWS account relationship target.
	ResourceTypeAWSAccount = "aws_account"
)
