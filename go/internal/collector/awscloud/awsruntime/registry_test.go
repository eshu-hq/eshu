package awsruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/redact"
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

func TestDefaultScannerFactoryBuildsECRScanner(t *testing.T) {
	factory := DefaultScannerFactory{}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceECR,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceECR,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want ECR scanner")
	}
}

func TestDefaultScannerFactoryBuildsEC2Scanner(t *testing.T) {
	factory := DefaultScannerFactory{}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceEC2,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceEC2,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want EC2 scanner")
	}
}

func TestDefaultScannerFactoryBuildsEKSScanner(t *testing.T) {
	factory := DefaultScannerFactory{}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceEKS,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceEKS,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want EKS scanner")
	}
}

func TestDefaultScannerFactoryBuildsSQSScanner(t *testing.T) {
	factory := DefaultScannerFactory{}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSQS,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSQS,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want SQS scanner")
	}
}

func TestDefaultScannerFactoryBuildsSNSScanner(t *testing.T) {
	factory := DefaultScannerFactory{}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSNS,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSNS,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want SNS scanner")
	}
}

func TestDefaultScannerFactoryBuildsEventBridgeScanner(t *testing.T) {
	factory := DefaultScannerFactory{}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceEventBridge,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceEventBridge,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want EventBridge scanner")
	}
}

func TestDefaultScannerFactoryBuildsELBv2Scanner(t *testing.T) {
	factory := DefaultScannerFactory{}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceELBv2,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceELBv2,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want ELBv2 scanner")
	}
}

func TestDefaultScannerFactoryBuildsRoute53Scanner(t *testing.T) {
	factory := DefaultScannerFactory{}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "aws-global",
		ServiceKind: awscloud.ServiceRoute53,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "aws-global",
		ServiceKind: awscloud.ServiceRoute53,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want Route53 scanner")
	}
}

func TestDefaultScannerFactoryBuildsECSScanner(t *testing.T) {
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	factory := DefaultScannerFactory{RedactionKey: key}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceECS,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceECS,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want ECS scanner")
	}
}

func TestDefaultScannerFactoryBuildsLambdaScanner(t *testing.T) {
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	factory := DefaultScannerFactory{RedactionKey: key}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceLambda,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceLambda,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want Lambda scanner")
	}
}

func TestDefaultScannerFactoryRequiresRedactionKeyForECS(t *testing.T) {
	factory := DefaultScannerFactory{}
	_, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceECS,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceECS,
	}, staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}})
	if err == nil {
		t.Fatalf("Scanner() error = nil, want missing ECS redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("Scanner() error = %q, want redaction key", err)
	}
}

func TestDefaultScannerFactoryRequiresRedactionKeyForLambda(t *testing.T) {
	factory := DefaultScannerFactory{}
	_, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceLambda,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceLambda,
	}, staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}})
	if err == nil {
		t.Fatalf("Scanner() error = nil, want missing Lambda redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("Scanner() error = %q, want redaction key", err)
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
