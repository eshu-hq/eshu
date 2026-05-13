package harbor

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveHarborRepositoryTags(t *testing.T) {
	if os.Getenv("ESHU_HARBOR_OCI_LIVE") != "1" {
		t.Skip("set ESHU_HARBOR_OCI_LIVE=1 to run the live Harbor OCI smoke")
	}
	baseURL := firstEnv("ESHU_HARBOR_OCI_URL", "HARBOR_URL")
	repository := firstEnv("ESHU_HARBOR_OCI_REPOSITORY", "HARBOR_IMAGE_REPOSITORY")
	if baseURL == "" || repository == "" {
		t.Skip("set ESHU_HARBOR_OCI_URL and ESHU_HARBOR_OCI_REPOSITORY for tag listing")
	}
	repository, err := RepositoryName(repository)
	if err != nil {
		t.Fatalf("RepositoryName() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client, err := NewDistributionClient(Config{
		BaseURL:     baseURL,
		Repository:  repository,
		Username:    firstEnv("ESHU_HARBOR_OCI_USERNAME", "HARBOR_USERNAME"),
		Password:    firstEnv("ESHU_HARBOR_OCI_PASSWORD", "HARBOR_PASSWORD", "HARBOR_TOKEN"),
		BearerToken: firstEnv("ESHU_HARBOR_OCI_BEARER_TOKEN", "HARBOR_BEARER_TOKEN"),
		Client:      &http.Client{Timeout: 20 * time.Second},
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
	if reference := firstEnv("ESHU_HARBOR_OCI_REFERENCE", "HARBOR_IMAGE_REFERENCE"); reference != "" {
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
