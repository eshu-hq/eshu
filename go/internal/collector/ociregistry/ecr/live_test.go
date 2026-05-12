package ecr

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
)

func TestLiveECRDistributionTags(t *testing.T) {
	if os.Getenv("ESHU_ECR_OCI_LIVE") != "1" {
		t.Skip("set ESHU_ECR_OCI_LIVE=1 to run the live ECR OCI smoke")
	}
	region := firstEnv("ESHU_ECR_OCI_REGION", "AWS_REGION", "AWS_DEFAULT_REGION")
	repository := os.Getenv("ESHU_ECR_OCI_REPOSITORY")
	if region == "" || repository == "" {
		t.Skip("set ESHU_ECR_OCI_REGION and ESHU_ECR_OCI_REPOSITORY for tag listing")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if profile := firstEnv("ESHU_ECR_AWS_PROFILE", "AWS_PROFILE"); profile != "" {
		options = append(options, awsconfig.WithSharedConfigProfile(profile))
	}
	config, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		t.Fatalf("load AWS configuration: %v", err)
	}

	credentials, err := GetDistributionCredentials(
		ctx,
		awsecr.NewFromConfig(config),
	)
	if err != nil {
		t.Fatalf("GetDistributionCredentials() error = %v", err)
	}
	registryHost, err := liveRegistryHost(region, credentials.ProxyEndpoint)
	if err != nil {
		t.Fatalf("liveRegistryHost() error = %v", err)
	}
	client, err := NewDistributionClient(
		registryHost,
		credentials.Username,
		credentials.Password,
		&http.Client{Timeout: 20 * time.Second},
	)
	if err != nil {
		t.Fatalf("NewDistributionClient() error = %v", err)
	}
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if _, err := client.ListTags(ctx, repository); err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if reference := os.Getenv("ESHU_ECR_OCI_REFERENCE"); reference != "" {
		if _, err := client.GetManifest(ctx, repository, reference); err != nil {
			t.Fatalf("GetManifest() error = %v", err)
		}
	}
}

func registryHostFromEndpoint(endpoint string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", err
	}
	return parsed.Host, nil
}

func liveRegistryHost(region, proxyEndpoint string) (string, error) {
	if registryHost := os.Getenv("ESHU_ECR_OCI_REGISTRY_HOST"); registryHost != "" {
		return registryHost, nil
	}
	if registryID := os.Getenv("ESHU_ECR_OCI_REGISTRY_ID"); registryID != "" {
		return PrivateRegistryHost(registryID, region)
	}
	return registryHostFromEndpoint(proxyEndpoint)
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
