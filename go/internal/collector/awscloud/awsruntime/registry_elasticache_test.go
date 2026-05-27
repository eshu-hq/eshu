package awsruntime

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestDefaultScannerFactoryBuildsElastiCacheScanner(t *testing.T) {
	factory := DefaultScannerFactory{}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceElastiCache,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceElastiCache,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want ElastiCache scanner")
	}
}

func TestDefaultScannerFactorySupportsElastiCacheServiceKind(t *testing.T) {
	if !SupportsServiceKind(awscloud.ServiceElastiCache) {
		t.Fatalf("SupportsServiceKind(elasticache) = false, want true")
	}
	found := false
	for _, kind := range SupportedServiceKinds() {
		if kind == awscloud.ServiceElastiCache {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("SupportedServiceKinds() missing %q", awscloud.ServiceElastiCache)
	}
}
