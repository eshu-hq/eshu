package awsruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestDefaultScannerFactoryBuildsIAMScanner(t *testing.T) {
	factory := DefaultScannerFactory{}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceIAM,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceIAM,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want IAM scanner")
	}
}

func TestDefaultScannerFactoryRejectsUnsupportedService(t *testing.T) {
	factory := DefaultScannerFactory{}
	_, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: "s3",
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: "s3",
	}, staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}})
	if err == nil {
		t.Fatalf("Scanner() error = nil, want unsupported service error")
	}
	if !strings.Contains(err.Error(), `unsupported AWS service_kind "s3"`) {
		t.Fatalf("Scanner() error = %q, want unsupported service_kind", err)
	}
}

func TestDefaultScannerFactoryRequiresAWSConfigLease(t *testing.T) {
	factory := DefaultScannerFactory{}
	_, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceIAM,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceIAM,
	}, releaseOnlyLease{})
	if err == nil {
		t.Fatalf("Scanner() error = nil, want unsupported lease error")
	}
	if !strings.Contains(err.Error(), "unsupported AWS credential lease") {
		t.Fatalf("Scanner() error = %q, want unsupported lease", err)
	}
}

type staticAWSConfigLease struct {
	config aws.Config
}

func (l staticAWSConfigLease) AWSConfig() aws.Config {
	return l.config
}

func (l staticAWSConfigLease) Release() error {
	return nil
}

type releaseOnlyLease struct{}

func (l releaseOnlyLease) Release() error {
	return nil
}
