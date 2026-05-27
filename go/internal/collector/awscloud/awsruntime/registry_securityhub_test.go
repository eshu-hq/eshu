package awsruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestDefaultScannerFactoryBuildsSecurityHubScanner(t *testing.T) {
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	factory := DefaultScannerFactory{RedactionKey: key}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSecurityHub,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSecurityHub,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want Security Hub scanner")
	}
}

func TestDefaultScannerFactoryRequiresRedactionKeyForSecurityHub(t *testing.T) {
	factory := DefaultScannerFactory{}
	_, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSecurityHub,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSecurityHub,
	}, staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}})
	if err == nil {
		t.Fatalf("Scanner() error = nil, want missing Security Hub redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("Scanner() error = %q, want redaction key", err)
	}
}
