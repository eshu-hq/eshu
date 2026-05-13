package awsruntime

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	iamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam"
	iamawssdk "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam/awssdk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DefaultScannerFactory maps AWS service claims to their production scanner
// adapters.
type DefaultScannerFactory struct {
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
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
	case awscloud.ServiceIAM:
		return iamservice.Scanner{
			Client: iamawssdk.NewClient(configLease.AWSConfig(), boundary, f.Tracer, f.Instruments),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported AWS service_kind %q", target.ServiceKind)
	}
}
