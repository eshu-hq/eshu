package awsruntime

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestSupportedServiceKindsBuildScanners(t *testing.T) {
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	factory := DefaultScannerFactory{RedactionKey: key}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	for _, service := range SupportedServiceKinds() {
		t.Run(service, func(t *testing.T) {
			target := Target{
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
