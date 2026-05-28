package bindings_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
)

// TestBindingsRegistersEverySupportedServiceKind confirms importing the
// aggregator populates the awsruntime registry with every service_kind the
// collector-aws-cloud command depends on. The list is intentionally
// duplicated here so a new scanner PR has to update both the aggregator and
// this guard test.
func TestBindingsRegistersEverySupportedServiceKind(t *testing.T) {
	want := []string{
		awscloud.ServiceAccessAnalyzer,
		awscloud.ServiceACM,
		awscloud.ServiceAPIGateway,
		awscloud.ServiceAthena,
		awscloud.ServiceCloudFront,
		awscloud.ServiceCloudWatchLogs,
		awscloud.ServiceDynamoDB,
		awscloud.ServiceEC2,
		awscloud.ServiceECR,
		awscloud.ServiceECS,
		awscloud.ServiceEKS,
		awscloud.ServiceELBv2,
		awscloud.ServiceElastiCache,
		awscloud.ServiceEventBridge,
		awscloud.ServiceGlue,
		awscloud.ServiceGuardDuty,
		awscloud.ServiceIAM,
		awscloud.ServiceLambda,
		awscloud.ServiceMSK,
		awscloud.ServiceOrganizations,
		awscloud.ServiceRDS,
		awscloud.ServiceRedshift,
		awscloud.ServiceRoute53,
		awscloud.ServiceS3,
		awscloud.ServiceSNS,
		awscloud.ServiceSQS,
		awscloud.ServiceSSM,
		awscloud.ServiceSecretsManager,
		awscloud.ServiceSecurityHub,
		awscloud.ServiceStepFunctions,
	}
	for _, kind := range want {
		if _, ok := awsruntime.LookupBuilder(kind); !ok {
			t.Errorf("LookupBuilder(%q) ok = false after importing bindings", kind)
		}
	}
}
