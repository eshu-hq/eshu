package jfrog

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
)

func TestLiveJFrogDistributionChallenge(t *testing.T) {
	if envFirst("ESHU_JFROG_OCI_LIVE") != "1" {
		t.Skip("set ESHU_JFROG_OCI_LIVE=1 to run the live JFrog OCI smoke")
	}
	baseURL := jfrogBaseURL()
	if baseURL == "" {
		t.Skip("set ESHU_JFROG_OCI_URL to run the live JFrog OCI smoke")
	}

	client, err := jfrogLiveClient(baseURL, &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("build live JFrog client: %v", err)
	}
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestLiveJFrogRepositoryTags(t *testing.T) {
	if envFirst("ESHU_JFROG_OCI_LIVE") != "1" {
		t.Skip("set ESHU_JFROG_OCI_LIVE=1 to run the live JFrog OCI smoke")
	}
	baseURL := jfrogBaseURL()
	repositoryKey := jfrogRepositoryKey()
	imageRepository := envFirst("ESHU_JFROG_OCI_IMAGE_REPOSITORY", "JFROG_IMAGE_REPOSITORY")
	if imageRepository == "" {
		t.Skip("set ESHU_JFROG_OCI_IMAGE_REPOSITORY for tag listing")
	}
	client, err := jfrogRepositoryClient(baseURL, repositoryKey, &http.Client{Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("build live JFrog repository client: %v", err)
	}
	tags, err := client.ListTags(context.Background(), imageRepository)
	if err != nil {
		t.Fatalf("ListTags(%s) error = %v", imageRepository, err)
	}
	if len(tags) == 0 {
		t.Fatalf("ListTags(%s) returned no tags", imageRepository)
	}
	if reference := envFirst("ESHU_JFROG_OCI_REFERENCE", "JFROG_IMAGE_REFERENCE"); reference != "" {
		if _, err := client.GetManifest(context.Background(), imageRepository, reference); err != nil {
			t.Fatalf("GetManifest(%s) error = %v", imageRepository, err)
		}
	}
}

func jfrogLiveClient(baseURL string, client *http.Client) (*distribution.Client, error) {
	repositoryKey := jfrogRepositoryKey()
	if repositoryKey == "" {
		return distribution.NewClient(distribution.ClientConfig{
			BaseURL: baseURL,
			Client:  client,
		})
	}
	return jfrogRepositoryClient(baseURL, repositoryKey, client)
}

func jfrogRepositoryClient(baseURL, repositoryKey string, client *http.Client) (*distribution.Client, error) {
	if repositoryKey == "" {
		return distribution.NewClient(distribution.ClientConfig{
			BaseURL:     baseURL,
			Username:    envFirst("ESHU_JFROG_OCI_USERNAME", "JFROG_USERNAME", "JFROG_USER"),
			Password:    envFirst("ESHU_JFROG_OCI_PASSWORD", "JFROG_PASSWORD"),
			BearerToken: envFirst("ESHU_JFROG_OCI_BEARER_TOKEN", "JFROG_ACCESS_TOKEN", "JFROG_BEARER_TOKEN"),
			Client:      client,
		})
	}
	return NewDistributionClient(Config{
		BaseURL:       baseURL,
		RepositoryKey: repositoryKey,
		Username:      envFirst("ESHU_JFROG_OCI_USERNAME", "JFROG_USERNAME", "JFROG_USER"),
		Password:      envFirst("ESHU_JFROG_OCI_PASSWORD", "JFROG_PASSWORD"),
		BearerToken:   envFirst("ESHU_JFROG_OCI_BEARER_TOKEN", "JFROG_ACCESS_TOKEN", "JFROG_BEARER_TOKEN"),
		Client:        client,
	})
}

func jfrogBaseURL() string {
	return envFirst("ESHU_JFROG_OCI_URL", "JFROG_URL", "JFROG_BASE_URL")
}

func jfrogRepositoryKey() string {
	return envFirst("ESHU_JFROG_OCI_REPOSITORY_KEY", "JFROG_DOCKER_REPOSITORY_KEY")
}

func envFirst(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}
