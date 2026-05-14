package awsruntime

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestDefaultScannerFactoryBuildsSSMScanner(t *testing.T) {
	factory := DefaultScannerFactory{}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSSM,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSSM,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want SSM scanner")
	}
}
