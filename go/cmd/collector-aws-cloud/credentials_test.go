package main

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestCredentialLeaseReleaseInvalidatesCopiedConfigCredentials(t *testing.T) {
	credentials := aws.Credentials{
		AccessKeyID:     "ASIATEMP",
		SecretAccessKey: "secret",
		SessionToken:    "token",
		Source:          "test",
		CanExpire:       true,
		Expires:         time.Now().Add(time.Hour),
	}
	provider := newClaimCredentialProvider(credentials)
	cache := aws.NewCredentialsCache(provider)
	lease := &awsCredentialLease{
		config:             aws.Config{Credentials: cache},
		credentialProvider: provider,
		credentialCache:    cache,
	}
	copiedConfig := lease.config
	if _, err := copiedConfig.Credentials.Retrieve(context.Background()); err != nil {
		t.Fatalf("Retrieve() before release error = %v", err)
	}

	if err := lease.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if got := provider.credentials.AccessKeyID; got != "" {
		t.Fatalf("provider AccessKeyID after release = %q, want empty", got)
	}
	if _, err := copiedConfig.Credentials.Retrieve(context.Background()); err == nil {
		t.Fatalf("Retrieve() after release error = nil, want released lease error")
	}
}
