package acr

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveACRRepositoryTags(t *testing.T) {
	if os.Getenv("ESHU_ACR_OCI_LIVE") != "1" {
		t.Skip("set ESHU_ACR_OCI_LIVE=1 to run the live ACR OCI smoke")
	}
	registryHost := firstEnv("ESHU_ACR_OCI_REGISTRY_HOST", "ACR_REGISTRY_HOST")
	repository := firstEnv("ESHU_ACR_OCI_REPOSITORY", "ACR_IMAGE_REPOSITORY")
	if registryHost == "" || repository == "" {
		t.Skip("set ESHU_ACR_OCI_REGISTRY_HOST and ESHU_ACR_OCI_REPOSITORY for tag listing")
	}
	repository, err := RepositoryName(repository)
	if err != nil {
		t.Fatalf("RepositoryName() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client, err := NewDistributionClient(Config{
		RegistryHost: registryHost,
		Repository:   repository,
		Username:     firstEnv("ESHU_ACR_OCI_USERNAME", "ACR_USERNAME"),
		Password:     firstEnv("ESHU_ACR_OCI_PASSWORD", "ACR_PASSWORD", "ACR_TOKEN"),
		BearerToken:  firstEnv("ESHU_ACR_OCI_BEARER_TOKEN", "ACR_BEARER_TOKEN"),
		Client:       &http.Client{Timeout: 20 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewDistributionClient() error = %v", err)
	}
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	tags, err := client.ListTags(ctx, repository)
	if err != nil {
		t.Fatalf("ListTags(%s) error = %v", repository, err)
	}
	if len(tags) == 0 {
		t.Fatalf("ListTags(%s) returned no tags", repository)
	}
	if reference := firstEnv("ESHU_ACR_OCI_REFERENCE", "ACR_IMAGE_REFERENCE"); reference != "" {
		if _, err := client.GetManifest(ctx, repository, reference); err != nil {
			t.Fatalf("GetManifest(%s:%s) error = %v", repository, reference, err)
		}
	}
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
