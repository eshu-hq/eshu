package awsruntime

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	ec2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ec2"
	ec2awssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ec2/awssdk"
	ecrservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecr"
	ecrawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecr/awssdk"
	ecsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecs"
	ecsawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecs/awssdk"
	eksservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/eks"
	eksawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/eks/awssdk"
	elbv2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elbv2"
	elbv2awssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elbv2/awssdk"
	eventbridgeservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/eventbridge"
	eventbridgeawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/eventbridge/awssdk"
	iamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam"
	iamawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam/awssdk"
	lambdaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/lambda"
	lambdaawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/lambda/awssdk"
	rdsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/rds"
	rdsawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/rds/awssdk"
	route53service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53"
	route53awssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53/awssdk"
	s3service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/s3"
	s3awssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/s3/awssdk"
	snsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sns"
	snsawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sns/awssdk"
	sqsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sqs"
	sqsawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sqs/awssdk"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DefaultScannerFactory maps AWS service claims to their production scanner
// adapters.
type DefaultScannerFactory struct {
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Checkpoints CheckpointStore
	// RedactionKey produces ECS task-definition environment value markers.
	// It is required only when building ECS scanners.
	RedactionKey redact.Key
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
	case awscloud.ServiceS3:
		return s3service.Scanner{
			Client: s3awssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceRDS:
		return rdsservice.Scanner{
			Client: rdsawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
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
	default:
		return nil, fmt.Errorf("unsupported AWS service_kind %q", target.ServiceKind)
	}
}
