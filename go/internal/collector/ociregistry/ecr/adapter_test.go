package ecr

import (
	"encoding/base64"
	"testing"
)

func TestPrivateRegistryHostBuildsECRHost(t *testing.T) {
	t.Parallel()

	got, err := PrivateRegistryHost("123456789012", "us-east-1")
	if err != nil {
		t.Fatalf("PrivateRegistryHost() error = %v", err)
	}
	want := "123456789012.dkr.ecr.us-east-1.amazonaws.com"
	if got != want {
		t.Fatalf("PrivateRegistryHost() = %q, want %q", got, want)
	}
}

func TestRepositoryIdentityUsesECRProvider(t *testing.T) {
	t.Parallel()

	identity := RepositoryIdentity("123456789012.dkr.ecr.us-east-1.amazonaws.com", "team/api")
	if identity.Provider != "ecr" {
		t.Fatalf("Provider = %q", identity.Provider)
	}
	if identity.Repository != "team/api" {
		t.Fatalf("Repository = %q", identity.Repository)
	}
}

func TestDistributionBaseURLUsesRegistryHost(t *testing.T) {
	t.Parallel()

	got, err := DistributionBaseURL(" 123456789012.dkr.ecr.us-east-1.amazonaws.com ")
	if err != nil {
		t.Fatalf("DistributionBaseURL() error = %v", err)
	}
	if want := "https://123456789012.dkr.ecr.us-east-1.amazonaws.com"; got != want {
		t.Fatalf("DistributionBaseURL() = %q, want %q", got, want)
	}
}

func TestDistributionBaseURLRejectsCredentialedURL(t *testing.T) {
	t.Parallel()

	if _, err := DistributionBaseURL("https://user:token@123456789012.dkr.ecr.us-east-1.amazonaws.com"); err == nil {
		t.Fatal("DistributionBaseURL() error = nil for credentialed URL")
	}
}

func TestBasicAuthFromAuthorizationToken(t *testing.T) {
	t.Parallel()

	token := base64.StdEncoding.EncodeToString([]byte("AWS:secret-password"))
	username, password, err := BasicAuthFromAuthorizationToken(token)
	if err != nil {
		t.Fatalf("BasicAuthFromAuthorizationToken() error = %v", err)
	}
	if username != "AWS" || password != "secret-password" {
		t.Fatalf("credentials = %q/%q", username, password)
	}
}

func TestPrivateRegistryHostRejectsMissingFields(t *testing.T) {
	t.Parallel()

	if _, err := PrivateRegistryHost("", "us-east-1"); err == nil {
		t.Fatal("PrivateRegistryHost() error = nil for blank account")
	}
	if _, err := PrivateRegistryHost("123456789012", ""); err == nil {
		t.Fatal("PrivateRegistryHost() error = nil for blank region")
	}
}
