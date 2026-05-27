package awsruntime_test

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestSupportedServiceKindsBuildScanners exercises every registered service
// builder through DefaultScannerFactory.Scanner, so adding a runtimebind
// import to bindings.go is the only step needed for a new scanner to be
// reachable from the runtime entry point.
func TestSupportedServiceKindsBuildScanners(t *testing.T) {
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	factory := awsruntime.DefaultScannerFactory{RedactionKey: key}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	for _, service := range awsruntime.SupportedServiceKinds() {
		t.Run(service, func(t *testing.T) {
			target := awsruntime.Target{
				AccountID:   "123456789012",
				Region:      "us-east-1",
				ServiceKind: service,
			}
			boundary := awscloud.Boundary{
				AccountID:   target.AccountID,
				Region:      target.Region,
				ServiceKind: target.ServiceKind,
			}
			scanner, err := factory.Scanner(context.Background(), target, boundary, lease)
			if err != nil {
				t.Fatalf("Scanner(%q) error = %v", service, err)
			}
			if scanner == nil {
				t.Fatalf("Scanner(%q) = nil", service)
			}
		})
	}
}

// TestSupportedServiceKindsCoversEveryAWSService asserts the registry holds
// the entire list of AWS service constants the collector promises to support.
// A new scanner PR has to update awscloud constants and the bindings list;
// failure here means one of the two is missing.
func TestSupportedServiceKindsCoversEveryAWSService(t *testing.T) {
	want := map[string]bool{
		awscloud.ServiceAccessAnalyzer:  true,
		awscloud.ServiceACM:             true,
		awscloud.ServiceAPIGateway:      true,
		awscloud.ServiceAthena:          true,
		awscloud.ServiceCloudFront:      true,
		awscloud.ServiceCloudTrail:      true,
		awscloud.ServiceCloudWatchLogs:  true,
		awscloud.ServiceDynamoDB:        true,
		awscloud.ServiceEC2:             true,
		awscloud.ServiceECR:             true,
		awscloud.ServiceECS:             true,
		awscloud.ServiceEKS:             true,
		awscloud.ServiceELBv2:           true,
		awscloud.ServiceElastiCache:     true,
		awscloud.ServiceEventBridge:     true,
		awscloud.ServiceGlue:            true,
		awscloud.ServiceGuardDuty:       true,
		awscloud.ServiceIAM:             true,
		awscloud.ServiceLambda:          true,
		awscloud.ServiceMSK:             true,
		awscloud.ServiceOrganizations:   true,
		awscloud.ServiceRDS:             true,
		awscloud.ServiceRedshift:        true,
		awscloud.ServiceRoute53:         true,
		awscloud.ServiceS3:              true,
		awscloud.ServiceSNS:             true,
		awscloud.ServiceSQS:             true,
		awscloud.ServiceSSM:             true,
		awscloud.ServiceSecretsManager:  true,
		awscloud.ServiceSecurityHub:     true,
		awscloud.ServiceStepFunctions:   true,
	}
	have := map[string]bool{}
	for _, kind := range awsruntime.SupportedServiceKinds() {
		have[kind] = true
	}
	for kind := range want {
		if !have[kind] {
			t.Errorf("SupportedServiceKinds() missing %q", kind)
		}
	}
	for kind := range have {
		if !want[kind] {
			t.Errorf("SupportedServiceKinds() reports unexpected %q (update want list?)", kind)
		}
	}
	if !awsruntime.SupportsServiceKind(awscloud.ServiceIAM) {
		t.Fatalf("SupportsServiceKind(iam) = false, want true")
	}
	if awsruntime.SupportsServiceKind("nonexistent") {
		t.Fatalf("SupportsServiceKind(nonexistent) = true, want false")
	}
}
