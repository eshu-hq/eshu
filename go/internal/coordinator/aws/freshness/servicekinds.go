// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import "slices"

// GlobalRegion is the scheduling region for global AWS service families.
// Triggers and planned targets for these families carry this region instead
// of a concrete one so their scope IDs match the collector claim shape.
const GlobalRegion = "aws-global"

// supportedServiceKinds is the host-owned scheduling vocabulary: every
// service_kind value the coordinator may plan work for and the trigger store
// may admit. It is seeded from the in-tree scanner registry (one entry per
// registered production scanner) and pinned to it by
// TestSupportedServiceKindsMatchScannerRegistry, so adding a scanner means
// extending this list in the same change. The sorted order is load-bearing
// for the pin test's comparison; keep it sorted.
var supportedServiceKinds = []string{
	"accessanalyzer",
	"acm",
	"acm-pca",
	"amp",
	"amplify",
	"apigateway",
	"apigatewayv2",
	"appconfig",
	"appflow",
	"applicationautoscaling",
	"appmesh",
	"apprunner",
	"appstream",
	"appsync",
	"athena",
	"auditmanager",
	"autoscaling",
	"backup",
	"batch",
	"bedrock",
	"cleanrooms",
	"cloudformation",
	"cloudfront",
	"cloudhsmv2",
	"cloudtrail",
	"cloudwatch",
	"cloudwatchlogs",
	"codeartifact",
	"codebuild",
	"codecommit",
	"codedeploy",
	"codeguru",
	"codepipeline",
	"cognito",
	"computeoptimizer",
	"config",
	"controltower",
	"databrew",
	"datasync",
	"datazone",
	"dax",
	"detective",
	"directconnect",
	"dms",
	"docdb",
	"docdbelastic",
	"drs",
	"ds",
	"dynamodb",
	"ec2",
	"ecr",
	"ecs",
	"efs",
	"eks",
	"elasticache",
	"elasticbeanstalk",
	"elb",
	"elbv2",
	"emr",
	"eventbridge",
	"firehose",
	"fis",
	"fms",
	"fsx",
	"globalaccelerator",
	"glue",
	"grafana",
	"guardduty",
	"iam",
	"imagebuilder",
	"inspector2",
	"keyspaces",
	"kinesis",
	"kinesisanalyticsv2",
	"kms",
	"lakeformation",
	"lambda",
	"licensemanager",
	"lightsail",
	"location",
	"macie2",
	"memorydb",
	"mgn",
	"mq",
	"msk",
	"mwaa",
	"neptune",
	"networkfirewall",
	"networkmanager",
	"opensearch",
	"opensearchserverless",
	"organizations",
	"outposts",
	"pinpoint",
	"proton",
	"quicksight",
	"ram",
	"rds",
	"redshift",
	"resiliencehub",
	"resourcegroups",
	"rolesanywhere",
	"route53",
	"route53recoverycontrolconfig",
	"route53resolver",
	"s3",
	"sagemaker",
	"secretsmanager",
	"securityhub",
	"securitylake",
	"servicecatalog",
	"servicecatalogappregistry",
	"servicediscovery",
	"servicequotas",
	"ses",
	"shield",
	"signer",
	"sns",
	"sqs",
	"ssm",
	"ssoadmin",
	"stepfunctions",
	"storagegateway",
	"synthetics",
	"timestream",
	"transfer",
	"transitgateway",
	"verifiedaccess",
	"verifiedpermissions",
	"vpc",
	"vpclattice",
	"wafv2",
	"workspaces",
	"xray",
}

// service-kind constants hardcoded by the EventBridge normalization maps and
// the global scheduling boundary below. Each value is a member of
// supportedServiceKinds, pinned by TestNormalizationKindsAreSupported and the
// normalization behavior tests, so a typo fails loudly instead of scheduling
// an unscannable kind.
const (
	serviceAPIGateway     = "apigateway"
	serviceAPIGatewayV2   = "apigatewayv2"
	serviceCloudFront     = "cloudfront"
	serviceCloudWatchLogs = "cloudwatchlogs"
	serviceDynamoDB       = "dynamodb"
	serviceEC2            = "ec2"
	serviceECR            = "ecr"
	serviceECS            = "ecs"
	serviceEKS            = "eks"
	serviceELBv2          = "elbv2"
	serviceEventBridge    = "eventbridge"
	serviceGlue           = "glue"
	serviceIAM            = "iam"
	serviceLambda         = "lambda"
	serviceRDS            = "rds"
	serviceRedshift       = "redshift"
	serviceRoute53        = "route53"
	serviceS3             = "s3"
	serviceSecretsManager = "secretsmanager"
	serviceSecurityHub    = "securityhub"
	serviceSNS            = "sns"
	serviceSQS            = "sqs"
	serviceSSM            = "ssm"
)

// globalServiceKinds are the AWS families whose triggers and planned targets
// use GlobalRegion instead of a concrete region, matching the collector claim
// shape for global scanner families.
var globalServiceKinds = []string{
	serviceCloudFront,
	serviceIAM,
	serviceRoute53,
}

// SupportedServiceKinds returns the schedulable service_kind values in
// sorted order. The returned slice is a copy; callers must not mutate it.
func SupportedServiceKinds() []string {
	return append([]string(nil), supportedServiceKinds...)
}

// SupportsServiceKind reports whether service is a schedulable service kind.
func SupportsServiceKind(service string) bool {
	return slices.Contains(supportedServiceKinds, service)
}

// IsGlobalServiceKind reports whether service is a global family whose
// triggers and planned targets use GlobalRegion.
func IsGlobalServiceKind(service string) bool {
	return slices.Contains(globalServiceKinds, service)
}
