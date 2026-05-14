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
	// ServiceEC2 identifies the regional Amazon Elastic Compute Cloud network
	// topology scan slice.
	ServiceEC2 = "ec2"
	// ServiceELBv2 identifies the regional Elastic Load Balancing v2 service
	// scan slice.
	ServiceELBv2 = "elbv2"
	// ServiceRoute53 identifies the global Amazon Route 53 service scan slice.
	ServiceRoute53 = "route53"
	// ServiceLambda identifies the regional AWS Lambda service scan slice.
	ServiceLambda = "lambda"
	// ServiceEKS identifies the regional Amazon Elastic Kubernetes Service scan
	// slice.
	ServiceEKS = "eks"
	// ServiceSQS identifies the regional Amazon Simple Queue Service metadata
	// scan slice.
	ServiceSQS = "sqs"
	// ServiceSNS identifies the regional Amazon Simple Notification Service
	// metadata scan slice.
	ServiceSNS = "sns"
	// ServiceEventBridge identifies the regional Amazon EventBridge metadata
	// scan slice.
	ServiceEventBridge = "eventbridge"
	// ServiceS3 identifies the regional Amazon Simple Storage Service bucket
	// metadata scan slice.
	ServiceS3 = "s3"
	// ServiceRDS identifies the regional Amazon Relational Database Service
	// metadata scan slice.
	ServiceRDS = "rds"
	// ServiceDynamoDB identifies the regional Amazon DynamoDB metadata scan
	// slice.
	ServiceDynamoDB = "dynamodb"
	// ServiceCloudWatchLogs identifies the regional Amazon CloudWatch Logs log
	// group metadata scan slice.
	ServiceCloudWatchLogs = "cloudwatchlogs"
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
	// ResourceTypeEC2VPC identifies an EC2 VPC.
	ResourceTypeEC2VPC = "aws_ec2_vpc"
	// ResourceTypeEC2Subnet identifies an EC2 subnet.
	ResourceTypeEC2Subnet = "aws_ec2_subnet"
	// ResourceTypeEC2SecurityGroup identifies an EC2 security group.
	ResourceTypeEC2SecurityGroup = "aws_ec2_security_group"
	// ResourceTypeEC2SecurityGroupRule identifies an EC2 security group rule.
	ResourceTypeEC2SecurityGroupRule = "aws_ec2_security_group_rule"
	// ResourceTypeEC2NetworkInterface identifies an EC2 network interface.
	ResourceTypeEC2NetworkInterface = "aws_ec2_network_interface"
	// ResourceTypeELBv2LoadBalancer identifies an ELBv2 load balancer.
	ResourceTypeELBv2LoadBalancer = "aws_elbv2_load_balancer"
	// ResourceTypeELBv2Listener identifies an ELBv2 listener.
	ResourceTypeELBv2Listener = "aws_elbv2_listener"
	// ResourceTypeELBv2TargetGroup identifies an ELBv2 target group.
	ResourceTypeELBv2TargetGroup = "aws_elbv2_target_group"
	// ResourceTypeELBv2Rule identifies an ELBv2 listener rule.
	ResourceTypeELBv2Rule = "aws_elbv2_rule"
	// ResourceTypeRoute53HostedZone identifies a Route 53 hosted zone.
	ResourceTypeRoute53HostedZone = "aws_route53_hosted_zone"
	// ResourceTypeLambdaFunction identifies a Lambda function.
	ResourceTypeLambdaFunction = "aws_lambda_function"
	// ResourceTypeLambdaAlias identifies a Lambda alias.
	ResourceTypeLambdaAlias = "aws_lambda_alias"
	// ResourceTypeLambdaEventSourceMapping identifies a Lambda event source
	// mapping.
	ResourceTypeLambdaEventSourceMapping = "aws_lambda_event_source_mapping"
	// ResourceTypeEKSCluster identifies an EKS cluster.
	ResourceTypeEKSCluster = "aws_eks_cluster"
	// ResourceTypeEKSNodegroup identifies an EKS managed node group.
	ResourceTypeEKSNodegroup = "aws_eks_nodegroup"
	// ResourceTypeEKSAddon identifies an EKS managed add-on.
	ResourceTypeEKSAddon = "aws_eks_addon"
	// ResourceTypeEKSOIDCProvider identifies OIDC provider evidence associated
	// with an EKS cluster.
	ResourceTypeEKSOIDCProvider = "aws_eks_oidc_provider"
	// ResourceTypeSQSQueue identifies an SQS queue metadata resource.
	ResourceTypeSQSQueue = "aws_sqs_queue"
	// ResourceTypeSNSTopic identifies an SNS topic metadata resource.
	ResourceTypeSNSTopic = "aws_sns_topic"
	// ResourceTypeEventBridgeEventBus identifies an EventBridge event bus
	// metadata resource.
	ResourceTypeEventBridgeEventBus = "aws_eventbridge_event_bus"
	// ResourceTypeEventBridgeRule identifies an EventBridge rule metadata
	// resource.
	ResourceTypeEventBridgeRule = "aws_eventbridge_rule"
	// ResourceTypeS3Bucket identifies an S3 bucket metadata resource.
	ResourceTypeS3Bucket = "aws_s3_bucket"
	// ResourceTypeRDSDBInstance identifies an RDS DB instance metadata
	// resource.
	ResourceTypeRDSDBInstance = "aws_rds_db_instance"
	// ResourceTypeRDSDBCluster identifies an RDS DB cluster metadata resource.
	ResourceTypeRDSDBCluster = "aws_rds_db_cluster"
	// ResourceTypeRDSDBSubnetGroup identifies an RDS DB subnet group metadata
	// resource.
	ResourceTypeRDSDBSubnetGroup = "aws_rds_db_subnet_group"
	// ResourceTypeDynamoDBTable identifies a DynamoDB table metadata resource.
	ResourceTypeDynamoDBTable = "aws_dynamodb_table"
	// ResourceTypeCloudWatchLogsLogGroup identifies a CloudWatch Logs log group
	// metadata resource.
	ResourceTypeCloudWatchLogsLogGroup = "aws_cloudwatch_logs_log_group"
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
	// RelationshipECSTaskUsesNetworkInterface records an ECS task ENI
	// attachment reported by DescribeTasks.
	RelationshipECSTaskUsesNetworkInterface = "ecs_task_uses_network_interface"
	// RelationshipEC2SubnetInVPC records subnet membership in a VPC.
	RelationshipEC2SubnetInVPC = "ec2_subnet_in_vpc"
	// RelationshipEC2SecurityGroupInVPC records security group membership in a
	// VPC.
	RelationshipEC2SecurityGroupInVPC = "ec2_security_group_in_vpc"
	// RelationshipEC2SecurityGroupHasRule records a security group child rule.
	RelationshipEC2SecurityGroupHasRule = "ec2_security_group_has_rule"
	// RelationshipEC2NetworkInterfaceInSubnet records ENI placement in a
	// subnet.
	RelationshipEC2NetworkInterfaceInSubnet = "ec2_network_interface_in_subnet"
	// RelationshipEC2NetworkInterfaceInVPC records ENI placement in a VPC.
	RelationshipEC2NetworkInterfaceInVPC = "ec2_network_interface_in_vpc"
	// RelationshipEC2NetworkInterfaceUsesSecurityGroup records security group
	// attachment to an ENI.
	RelationshipEC2NetworkInterfaceUsesSecurityGroup = "ec2_network_interface_uses_security_group"
	// RelationshipEC2NetworkInterfaceAttachedToResource records ENI attachment
	// evidence without emitting the attached resource as an inventory fact.
	RelationshipEC2NetworkInterfaceAttachedToResource = "ec2_network_interface_attached_to_resource"
	// RelationshipELBv2LoadBalancerHasListener records listener membership on a
	// load balancer.
	RelationshipELBv2LoadBalancerHasListener = "elbv2_load_balancer_has_listener"
	// RelationshipELBv2ListenerHasRule records rule membership on a listener.
	RelationshipELBv2ListenerHasRule = "elbv2_listener_has_rule"
	// RelationshipELBv2ListenerRoutesToTargetGroup records listener or rule
	// routing to a target group.
	RelationshipELBv2ListenerRoutesToTargetGroup = "elbv2_listener_routes_to_target_group"
	// RelationshipELBv2TargetGroupAttachedToLoadBalancer records target group
	// attachment to a load balancer.
	RelationshipELBv2TargetGroupAttachedToLoadBalancer = "elbv2_target_group_attached_to_load_balancer"
	// RelationshipLambdaAliasTargetsFunction records alias routing to a Lambda
	// function version.
	RelationshipLambdaAliasTargetsFunction = "lambda_alias_targets_function"
	// RelationshipLambdaEventSourceMappingTargetsFunction records an event
	// source mapping target function.
	RelationshipLambdaEventSourceMappingTargetsFunction = "lambda_event_source_mapping_targets_function"
	// RelationshipLambdaFunctionUsesImage records a Lambda container image URI.
	RelationshipLambdaFunctionUsesImage = "lambda_function_uses_image"
	// RelationshipLambdaFunctionUsesExecutionRole records a Lambda execution
	// role.
	RelationshipLambdaFunctionUsesExecutionRole = "lambda_function_uses_execution_role"
	// RelationshipLambdaFunctionUsesSubnet records Lambda VPC subnet placement.
	RelationshipLambdaFunctionUsesSubnet = "lambda_function_uses_subnet"
	// RelationshipLambdaFunctionUsesSecurityGroup records Lambda VPC security
	// group attachment.
	RelationshipLambdaFunctionUsesSecurityGroup = "lambda_function_uses_security_group"
	// RelationshipEKSClusterUsesIAMRole records an EKS cluster service role.
	RelationshipEKSClusterUsesIAMRole = "eks_cluster_uses_iam_role"
	// RelationshipEKSClusterUsesSubnet records EKS cluster subnet placement.
	RelationshipEKSClusterUsesSubnet = "eks_cluster_uses_subnet"
	// RelationshipEKSClusterUsesSecurityGroup records EKS cluster security group
	// placement.
	RelationshipEKSClusterUsesSecurityGroup = "eks_cluster_uses_security_group"
	// RelationshipEKSClusterHasOIDCProvider records an EKS cluster's OIDC
	// provider evidence for IRSA trust.
	RelationshipEKSClusterHasOIDCProvider = "eks_cluster_has_oidc_provider"
	// RelationshipEKSClusterHasNodegroup records managed node group membership
	// on an EKS cluster.
	RelationshipEKSClusterHasNodegroup = "eks_cluster_has_nodegroup"
	// RelationshipEKSClusterHasAddon records managed add-on membership on an EKS
	// cluster.
	RelationshipEKSClusterHasAddon = "eks_cluster_has_addon"
	// RelationshipEKSNodegroupUsesIAMRole records the IAM role used by an EKS
	// managed node group.
	RelationshipEKSNodegroupUsesIAMRole = "eks_nodegroup_uses_iam_role"
	// RelationshipEKSNodegroupUsesSubnet records an EKS managed node group
	// subnet.
	RelationshipEKSNodegroupUsesSubnet = "eks_nodegroup_uses_subnet"
	// RelationshipEKSAddonUsesIAMRole records the IAM role used by an EKS
	// managed add-on.
	RelationshipEKSAddonUsesIAMRole = "eks_addon_uses_iam_role"
	// RelationshipSQSQueueUsesDeadLetterQueue records SQS redrive policy
	// evidence from a source queue to its dead-letter queue.
	RelationshipSQSQueueUsesDeadLetterQueue = "sqs_queue_uses_dead_letter_queue"
	// RelationshipSNSTopicDeliversToResource records SNS subscription
	// evidence from a topic to an ARN-addressable subscriber.
	RelationshipSNSTopicDeliversToResource = "sns_topic_delivers_to_resource"
	// RelationshipEventBridgeRuleOnEventBus records EventBridge rule
	// membership on an event bus.
	RelationshipEventBridgeRuleOnEventBus = "eventbridge_rule_on_event_bus"
	// RelationshipEventBridgeRuleTargetsResource records EventBridge rule target
	// evidence when the target is ARN-addressable.
	RelationshipEventBridgeRuleTargetsResource = "eventbridge_rule_targets_resource"
	// RelationshipS3BucketLogsToBucket records S3 server-access-log delivery
	// metadata from a source bucket to its target bucket.
	RelationshipS3BucketLogsToBucket = "s3_bucket_logs_to_bucket"
	// RelationshipRDSDBInstanceMemberOfCluster records an RDS instance's
	// reported DB cluster membership.
	RelationshipRDSDBInstanceMemberOfCluster = "rds_db_instance_member_of_cluster"
	// RelationshipRDSDBInstanceInSubnetGroup records an RDS instance's reported
	// DB subnet group placement.
	RelationshipRDSDBInstanceInSubnetGroup = "rds_db_instance_in_subnet_group"
	// RelationshipRDSDBClusterInSubnetGroup records an RDS cluster's reported
	// DB subnet group placement.
	RelationshipRDSDBClusterInSubnetGroup = "rds_db_cluster_in_subnet_group"
	// RelationshipRDSDBInstanceUsesSecurityGroup records an RDS instance's
	// reported VPC security group attachment.
	RelationshipRDSDBInstanceUsesSecurityGroup = "rds_db_instance_uses_security_group"
	// RelationshipRDSDBClusterUsesSecurityGroup records an RDS cluster's
	// reported VPC security group attachment.
	RelationshipRDSDBClusterUsesSecurityGroup = "rds_db_cluster_uses_security_group"
	// RelationshipRDSDBInstanceUsesKMSKey records an RDS instance's reported KMS
	// key dependency.
	RelationshipRDSDBInstanceUsesKMSKey = "rds_db_instance_uses_kms_key"
	// RelationshipRDSDBClusterUsesKMSKey records an RDS cluster's reported KMS
	// key dependency.
	RelationshipRDSDBClusterUsesKMSKey = "rds_db_cluster_uses_kms_key"
	// RelationshipRDSDBInstanceUsesMonitoringRole records an RDS instance's
	// enhanced-monitoring IAM role dependency.
	RelationshipRDSDBInstanceUsesMonitoringRole = "rds_db_instance_uses_monitoring_role"
	// RelationshipRDSDBClusterUsesIAMRole records an RDS cluster's reported
	// associated IAM role dependency.
	RelationshipRDSDBClusterUsesIAMRole = "rds_db_cluster_uses_iam_role"
	// RelationshipRDSDBInstanceUsesParameterGroup records an RDS instance's
	// reported DB parameter group dependency.
	RelationshipRDSDBInstanceUsesParameterGroup = "rds_db_instance_uses_parameter_group"
	// RelationshipRDSDBClusterUsesParameterGroup records an RDS cluster's
	// reported DB cluster parameter group dependency.
	RelationshipRDSDBClusterUsesParameterGroup = "rds_db_cluster_uses_parameter_group"
	// RelationshipRDSDBInstanceUsesOptionGroup records an RDS instance's
	// reported option group dependency.
	RelationshipRDSDBInstanceUsesOptionGroup = "rds_db_instance_uses_option_group"
	// RelationshipDynamoDBTableUsesKMSKey records a DynamoDB table's reported
	// server-side encryption KMS key dependency.
	RelationshipDynamoDBTableUsesKMSKey = "dynamodb_table_uses_kms_key"
	// RelationshipCloudWatchLogsLogGroupUsesKMSKey records a CloudWatch Logs
	// log group's reported KMS key dependency.
	RelationshipCloudWatchLogsLogGroupUsesKMSKey = "cloudwatch_logs_log_group_uses_kms_key"
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

// DNSRecordObservation describes one Route 53 DNS record reported by AWS.
type DNSRecordObservation struct {
	Boundary          Boundary
	HostedZoneID      string
	HostedZoneName    string
	HostedZonePrivate bool
	RecordName        string
	RecordType        string
	SetIdentifier     string
	TTL               *int64
	Values            []string
	AliasTarget       *DNSAliasTarget
	RoutingPolicy     DNSRoutingPolicy
	SourceURI         string
	SourceRecordID    string
}

// DNSAliasTarget captures Route 53 alias target evidence without inferring
// ownership of the target resource.
type DNSAliasTarget struct {
	DNSName              string
	HostedZoneID         string
	EvaluateTargetHealth bool
}

// DNSRoutingPolicy captures non-secret Route 53 routing policy selectors.
type DNSRoutingPolicy struct {
	Weight                  *int64
	Region                  string
	Failover                string
	HealthCheckID           string
	MultiValueAnswer        *bool
	TrafficPolicyInstanceID string
	GeoLocation             DNSGeoLocation
	CIDRCollectionID        string
	CIDRLocationName        string
}

// DNSGeoLocation captures Route 53 geolocation routing selectors.
type DNSGeoLocation struct {
	ContinentCode   string
	CountryCode     string
	SubdivisionCode string
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
