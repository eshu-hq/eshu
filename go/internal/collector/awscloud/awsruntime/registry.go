package awsruntime

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	accessanalyzerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/accessanalyzer"
	accessanalyzerawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/accessanalyzer/awssdk"
	apigatewayservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apigateway"
	apigatewayawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apigateway/awssdk"
	athenaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/athena"
	athenaawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/athena/awssdk"
	cloudfrontservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudfront"
	cloudfrontawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudfront/awssdk"
	cloudwatchlogsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudwatchlogs"
	cloudwatchlogsawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudwatchlogs/awssdk"
	dynamodbservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/dynamodb"
	dynamodbawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/dynamodb/awssdk"
	ec2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ec2"
	ec2awssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ec2/awssdk"
	ecrservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecr"
	ecrawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecr/awssdk"
	ecsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecs"
	ecsawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecs/awssdk"
	eksservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/eks"
	eksawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/eks/awssdk"
	elasticacheservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elasticache"
	elasticacheawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elasticache/awssdk"
	elbv2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elbv2"
	elbv2awssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elbv2/awssdk"
	eventbridgeservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/eventbridge"
	eventbridgeawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/eventbridge/awssdk"
	glueservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/glue"
	glueawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/glue/awssdk"
	guarddutyservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/guardduty"
	guarddutyawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/guardduty/awssdk"
	iamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam"
	iamawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam/awssdk"
	lambdaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/lambda"
	lambdaawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/lambda/awssdk"
	mskservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/msk"
	mskawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/msk/awssdk"
	organizationsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/organizations"
	organizationsawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/organizations/awssdk"
	rdsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/rds"
	rdsawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/rds/awssdk"
	redshiftservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/redshift"
	redshiftawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/redshift/awssdk"
	route53service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53"
	route53awssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53/awssdk"
	s3service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/s3"
	s3awssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/s3/awssdk"
	secretsmanagerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/secretsmanager"
	secretsmanagerawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/secretsmanager/awssdk"
	securityhubservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/securityhub"
	securityhubawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/securityhub/awssdk"
	snsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sns"
	snsawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sns/awssdk"
	sqsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sqs"
	sqsawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sqs/awssdk"
	ssmservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ssm"
	ssmawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ssm/awssdk"
	stepfunctionsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/stepfunctions"
	stepfunctionsawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/stepfunctions/awssdk"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DefaultScannerFactory maps AWS service claims to their production scanner
// adapters.
type DefaultScannerFactory struct {
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Checkpoints CheckpointStore
	// RedactionKey produces deterministic markers for sensitive metadata fields.
	// It is required when building ECS, Lambda, Security Hub, or Organizations
	// scanners.
	RedactionKey redact.Key
}

var supportedServiceKinds = []string{
	awscloud.ServiceECS,
	awscloud.ServiceECR,
	awscloud.ServiceEC2,
	awscloud.ServiceEKS,
	awscloud.ServiceELBv2,
	awscloud.ServiceRoute53,
	awscloud.ServiceSQS,
	awscloud.ServiceSNS,
	awscloud.ServiceEventBridge,
	awscloud.ServiceOrganizations,
	awscloud.ServiceGuardDuty,
	awscloud.ServiceS3,
	awscloud.ServiceRDS,
	awscloud.ServiceDynamoDB,
	awscloud.ServiceCloudWatchLogs,
	awscloud.ServiceCloudFront,
	awscloud.ServiceAPIGateway,
	awscloud.ServiceSecretsManager,
	awscloud.ServiceSSM,
	awscloud.ServiceAthena,
	awscloud.ServiceGlue,
	awscloud.ServiceElastiCache,
	awscloud.ServiceMSK,
	awscloud.ServiceStepFunctions,
	awscloud.ServiceAccessAnalyzer,
	awscloud.ServiceIAM,
	awscloud.ServiceLambda,
	awscloud.ServiceSecurityHub,
	awscloud.ServiceRedshift,
}

// SupportedServiceKinds returns the service_kind values backed by production
// scanner adapters. Callers receive a copy so package registry state cannot be
// mutated through config validation or tests.
func SupportedServiceKinds() []string {
	return append([]string(nil), supportedServiceKinds...)
}

// SupportsServiceKind reports whether service is backed by a production AWS
// scanner adapter.
func SupportsServiceKind(service string) bool {
	for _, supported := range supportedServiceKinds {
		if service == supported {
			return true
		}
	}
	return false
}

// Scanner implements ScannerFactory.
func (f DefaultScannerFactory) Scanner(
	_ context.Context,
	target Target,
	boundary awscloud.Boundary,
	lease CredentialLease,
) (ServiceScanner, error) {
	configLease, ok := lease.(AWSConfigLease)
	if !ok {
		return nil, fmt.Errorf("unsupported AWS credential lease %T", lease)
	}
	switch target.ServiceKind {
	case awscloud.ServiceECS:
		if f.RedactionKey.IsZero() {
			return nil, fmt.Errorf("ecs scanner redaction key is required")
		}
		return ecsservice.Scanner{
			Client:       ecsawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
			RedactionKey: f.RedactionKey,
		}, nil
	case awscloud.ServiceECR:
		return ecrservice.Scanner{
			Client: ecrawssdk.NewClientWithCheckpoints(
				configLease.AWSConfig(),
				boundary,
				f.Tracer,
				f.Instruments,
				f.Checkpoints,
			),
		}, nil
	case awscloud.ServiceEC2:
		return ec2service.Scanner{
			Client: ec2awssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceEKS:
		return eksservice.Scanner{
			Client: eksawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceELBv2:
		return elbv2service.Scanner{
			Client: elbv2awssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceRoute53:
		return route53service.Scanner{
			Client: route53awssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceSQS:
		return sqsservice.Scanner{
			Client: sqsawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceSNS:
		return snsservice.Scanner{
			Client: snsawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceEventBridge:
		return eventbridgeservice.Scanner{
			Client: eventbridgeawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceOrganizations:
		if f.RedactionKey.IsZero() {
			return nil, fmt.Errorf("organizations scanner redaction key is required")
		}
		return organizationsservice.Scanner{
			Client:       organizationsawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
			RedactionKey: f.RedactionKey,
		}, nil
	case awscloud.ServiceGuardDuty:
		return guarddutyservice.Scanner{
			Client: guarddutyawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceS3:
		return s3service.Scanner{
			Client: s3awssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceRDS:
		return rdsservice.Scanner{
			Client: rdsawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceDynamoDB:
		return dynamodbservice.Scanner{
			Client: dynamodbawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceCloudWatchLogs:
		return cloudwatchlogsservice.Scanner{
			Client: cloudwatchlogsawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceCloudFront:
		return cloudfrontservice.Scanner{
			Client: cloudfrontawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceAPIGateway:
		return apigatewayservice.Scanner{
			Client: apigatewayawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceSecretsManager:
		return secretsmanagerservice.Scanner{
			Client: secretsmanagerawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceSSM:
		return ssmservice.Scanner{
			Client: ssmawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceAthena:
		return athenaservice.Scanner{
			Client: athenaawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceGlue:
		return glueservice.Scanner{
			Client: glueawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceElastiCache:
		return elasticacheservice.Scanner{
			Client: elasticacheawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceMSK:
		return mskservice.Scanner{
			Client: mskawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceStepFunctions:
		return stepfunctionsservice.Scanner{
			Client: stepfunctionsawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceAccessAnalyzer:
		return accessanalyzerservice.Scanner{
			Client: accessanalyzerawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceIAM:
		return iamservice.Scanner{
			Client: iamawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceLambda:
		if f.RedactionKey.IsZero() {
			return nil, fmt.Errorf("lambda scanner redaction key is required")
		}
		return lambdaservice.Scanner{
			Client:       lambdaawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
			RedactionKey: f.RedactionKey,
		}, nil
	case awscloud.ServiceSecurityHub:
		if f.RedactionKey.IsZero() {
			return nil, fmt.Errorf("securityhub scanner redaction key is required")
		}
		return securityhubservice.Scanner{
			Client:       securityhubawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
			RedactionKey: f.RedactionKey,
		}, nil
	case awscloud.ServiceRedshift:
		return redshiftservice.Scanner{
			Client: redshiftawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported AWS service_kind %q", target.ServiceKind)
	}
}
