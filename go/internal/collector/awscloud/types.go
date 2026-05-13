package awscloud

import "time"

const (
	// CollectorKind is the durable collector_kind value for AWS cloud facts.
	CollectorKind = "aws"

	// ServiceIAM identifies the global IAM service scan slice.
	ServiceIAM = "iam"
	// ServiceECR identifies the regional Amazon Elastic Container Registry
	// service scan slice.
	ServiceECR = "ecr"
	// ServiceECS identifies the regional Amazon Elastic Container Service
	// service scan slice.
	ServiceECS = "ecs"
)

const (
	// ResourceTypeIAMRole identifies an IAM role.
	ResourceTypeIAMRole = "aws_iam_role"
	// ResourceTypeIAMPolicy identifies an IAM policy.
	ResourceTypeIAMPolicy = "aws_iam_policy"
	// ResourceTypeIAMInstanceProfile identifies an IAM instance profile.
	ResourceTypeIAMInstanceProfile = "aws_iam_instance_profile"
	// ResourceTypeIAMPrincipal identifies a principal from an IAM trust policy.
	ResourceTypeIAMPrincipal = "aws_iam_principal"
	// ResourceTypeECRRepository identifies an ECR repository.
	ResourceTypeECRRepository = "aws_ecr_repository"
	// ResourceTypeECRLifecyclePolicy identifies an ECR repository lifecycle
	// policy child resource.
	ResourceTypeECRLifecyclePolicy = "aws_ecr_lifecycle_policy"
	// ResourceTypeECSCluster identifies an ECS cluster.
	ResourceTypeECSCluster = "aws_ecs_cluster"
	// ResourceTypeECSService identifies an ECS service.
	ResourceTypeECSService = "aws_ecs_service"
	// ResourceTypeECSTaskDefinition identifies an ECS task definition.
	ResourceTypeECSTaskDefinition = "aws_ecs_task_definition"
	// ResourceTypeECSTask identifies an ECS task.
	ResourceTypeECSTask = "aws_ecs_task"
)

const (
	// RelationshipIAMRoleTrustsPrincipal records a role trust-policy principal.
	RelationshipIAMRoleTrustsPrincipal = "iam_role_trusts_principal"
	// RelationshipIAMRoleAttachedPolicy records a managed policy attachment.
	RelationshipIAMRoleAttachedPolicy = "iam_role_attached_policy"
	// RelationshipIAMRoleInInstanceProfile records a role/profile membership.
	RelationshipIAMRoleInInstanceProfile = "iam_role_in_instance_profile"
	// RelationshipECSServiceUsesTaskDefinition records the task definition a
	// service currently runs.
	RelationshipECSServiceUsesTaskDefinition = "ecs_service_uses_task_definition"
	// RelationshipECSTaskDefinitionUsesImage records a container image
	// referenced by a task definition.
	RelationshipECSTaskDefinitionUsesImage = "ecs_task_definition_uses_image"
	// RelationshipECSServiceTargetsLoadBalancer records an ECS service load
	// balancer or target group binding.
	RelationshipECSServiceTargetsLoadBalancer = "ecs_service_targets_load_balancer"
)

// Boundary carries the durable scope-generation and claim identity shared by
// all facts emitted for one AWS claim.
type Boundary struct {
	AccountID           string
	Region              string
	ServiceKind         string
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
}

// ResourceObservation describes one AWS resource reported by a service API.
type ResourceObservation struct {
	Boundary           Boundary
	ARN                string
	ResourceID         string
	ResourceType       string
	Name               string
	State              string
	Tags               map[string]string
	Attributes         map[string]any
	CorrelationAnchors []string
	SourceURI          string
	SourceRecordID     string
}

// RelationshipObservation describes one relationship reported by AWS APIs.
type RelationshipObservation struct {
	Boundary         Boundary
	RelationshipType string
	SourceResourceID string
	SourceARN        string
	TargetResourceID string
	TargetARN        string
	TargetType       string
	Attributes       map[string]any
	SourceURI        string
	SourceRecordID   string
}

// ImageReferenceObservation describes one ECR image digest and tag reference.
type ImageReferenceObservation struct {
	Boundary          Boundary
	RepositoryARN     string
	RepositoryName    string
	RegistryID        string
	ImageDigest       string
	ManifestDigest    string
	Tag               string
	PushedAt          time.Time
	ImageSizeInBytes  int64
	ManifestMediaType string
	ArtifactMediaType string
	SourceURI         string
	SourceRecordID    string
}

// WarningObservation describes one non-fatal AWS scan warning.
type WarningObservation struct {
	Boundary       Boundary
	WarningKind    string
	ErrorClass     string
	Message        string
	SourceURI      string
	SourceRecordID string
	Attributes     map[string]any
}
