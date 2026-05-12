package dockerhub

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveDockerHubRepositoryTags(t *testing.T) {
	if os.Getenv("ESHU_DOCKERHUB_OCI_LIVE") != "1" {
		t.Skip("set ESHU_DOCKERHUB_OCI_LIVE=1 to run the live Docker Hub OCI smoke")
	}
	repository := firstEnv("ESHU_DOCKERHUB_OCI_REPOSITORY", "DOCKERHUB_IMAGE_REPOSITORY")
	if repository == "" {
		repository = "library/busybox"
	}
	repository, err := RepositoryName(repository)
	if err != nil {
		t.Fatalf("RepositoryName() error = %v", err)
	}
	reference := firstEnv("ESHU_DOCKERHUB_OCI_REFERENCE", "DOCKERHUB_IMAGE_REFERENCE")
	if reference == "" {
		reference = "latest"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client, err := NewDistributionClient(ctx, Config{
		Repository: repository,
		Username:   firstEnv("ESHU_DOCKERHUB_OCI_USERNAME", "DOCKERHUB_USERNAME"),
		Password:   firstEnv("ESHU_DOCKERHUB_OCI_PASSWORD", "DOCKERHUB_PASSWORD", "DOCKERHUB_TOKEN"),
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
