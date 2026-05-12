package ghcr

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveGHCRRepositoryTags(t *testing.T) {
	if os.Getenv("ESHU_GHCR_OCI_LIVE") != "1" {
		t.Skip("set ESHU_GHCR_OCI_LIVE=1 to run the live GHCR OCI smoke")
	}
	repository := firstEnv("ESHU_GHCR_OCI_REPOSITORY", "GHCR_IMAGE_REPOSITORY")
	if repository == "" {
		repository = "stargz-containers/busybox"
	}
	repository, err := RepositoryName(repository)
	if err != nil {
		t.Fatalf("RepositoryName() error = %v", err)
	}
	reference := firstEnv("ESHU_GHCR_OCI_REFERENCE", "GHCR_IMAGE_REFERENCE")
	if reference == "" {
		reference = "1.32.0-org"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client, err := NewDistributionClient(ctx, Config{
		Repository: repository,
		Username:   firstEnv("ESHU_GHCR_OCI_USERNAME", "GHCR_USERNAME"),
		Password:   firstEnv("ESHU_GHCR_OCI_PASSWORD", "GHCR_PASSWORD", "GHCR_TOKEN"),
		Client:     &http.Client{Timeout: 20 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewDistributionClient() error = %v", err)
	}
	tags, err := client.ListTags(ctx, repository)
	if err != nil {
		t.Fatalf("ListTags(%s) error = %v", repository, err)
	}
	if len(tags) == 0 {
		t.Fatalf("ListTags(%s) returned no tags", repository)
	}
	if _, err := client.GetManifest(ctx, repository, reference); err != nil {
		t.Fatalf("GetManifest(%s:%s) error = %v", repository, reference, err)
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
