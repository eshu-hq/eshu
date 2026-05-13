package awsruntime

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	ecrservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecr"
	ecrawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecr/awssdk"
	ecsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecs"
	ecsawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecs/awssdk"
	elbv2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elbv2"
	elbv2awssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elbv2/awssdk"
	iamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam"
	iamawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam/awssdk"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DefaultScannerFactory maps AWS service claims to their production scanner
// adapters.
type DefaultScannerFactory struct {
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
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
			Client: ecrawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceELBv2:
		return elbv2service.Scanner{
			Client: elbv2awssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	case awscloud.ServiceIAM:
		return iamservice.Scanner{
			Client: iamawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported AWS service_kind %q", target.ServiceKind)
	}
}
