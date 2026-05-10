package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/tfstateruntime"
)

const legacyAWSCredentialCacheKey = "__legacy__"

type targetScopeSourceFactoryConfig struct {
	DefaultCredentials      awsCredentialConfig
	TargetScopes            []awsTargetScopeConfig
	S3FallbackLockTableName string
	MaxBytes                int64
}

type targetScopeSourceFactory struct {
	config      targetScopeSourceFactoryConfig
	mu          sync.Mutex
	s3Clients   map[string]terraformstate.S3ObjectClient
	lockClients map[string]terraformstate.LockMetadataClient
}

func newTargetScopeSourceFactory(config targetScopeSourceFactoryConfig) *targetScopeSourceFactory {
	return &targetScopeSourceFactory{
		config:      config,
		s3Clients:   map[string]terraformstate.S3ObjectClient{},
		lockClients: map[string]terraformstate.LockMetadataClient{},
	}
}

func (f *targetScopeSourceFactory) OpenSource(
	ctx context.Context,
	candidate terraformstate.DiscoveryCandidate,
) (terraformstate.StateSource, error) {
	if candidate.State.BackendKind == terraformstate.BackendLocal {
		if err := f.validateLocalTargetScope(candidate); err != nil {
			return nil, err
		}
		return tfstateruntime.DefaultSourceFactory{
			MaxBytes: f.config.MaxBytes,
		}.OpenSource(ctx, candidate)
	}

	credentials, cacheKey, err := f.credentialsForCandidate(candidate)
	if err != nil {
		return nil, err
	}
	return tfstateruntime.DefaultSourceFactory{
		S3Client:                f.s3Client(cacheKey, credentials),
		S3FallbackLockTableName: f.config.S3FallbackLockTableName,
		S3LockMetadataClient:    f.lockMetadataClient(cacheKey, credentials),
		MaxBytes:                f.config.MaxBytes,
	}.OpenSource(ctx, candidate)
}

func (f *targetScopeSourceFactory) credentialsForCandidate(
	candidate terraformstate.DiscoveryCandidate,
) (awsCredentialConfig, string, error) {
	if err := candidate.Validate(); err != nil {
		return awsCredentialConfig{}, "", err
	}
	targetScopeID := strings.TrimSpace(candidate.TargetScopeID)
	if targetScopeID != "" {
		for _, targetScope := range f.config.TargetScopes {
			if targetScope.TargetScopeID != targetScopeID {
				continue
			}
			if !targetScopeAllowsCandidate(targetScope, candidate) {
				return awsCredentialConfig{}, "", fmt.Errorf(
					"terraform state candidate is outside target_scope_id %q allowlist",
					targetScopeID,
				)
			}
			return targetScope.Credentials, targetScopeID, nil
		}
		return awsCredentialConfig{}, "", fmt.Errorf("terraform state target_scope_id %q is unknown", targetScopeID)
	}
	if len(f.config.TargetScopes) == 0 {
		return f.config.DefaultCredentials, legacyAWSCredentialCacheKey, nil
	}

	var matched *awsTargetScopeConfig
	for index := range f.config.TargetScopes {
		targetScope := &f.config.TargetScopes[index]
		if !targetScopeAllowsCandidate(*targetScope, candidate) {
			continue
		}
		if matched != nil {
			return awsCredentialConfig{}, "", fmt.Errorf(
				"terraform state candidate has ambiguous target_scope_id for backend %q region %q",
				candidate.State.BackendKind,
				candidate.Region,
			)
		}
		matched = targetScope
	}
	if matched == nil {
		return awsCredentialConfig{}, "", fmt.Errorf(
			"terraform state candidate does not match any configured target_scope_id for backend %q region %q",
			candidate.State.BackendKind,
			candidate.Region,
		)
	}
	return matched.Credentials, matched.TargetScopeID, nil
}

func targetScopeAllowsCandidate(
	targetScope awsTargetScopeConfig,
	candidate terraformstate.DiscoveryCandidate,
) bool {
	if !stringListAllows(targetScope.AllowedBackends, string(candidate.State.BackendKind)) {
		return false
	}
	if candidate.State.BackendKind == terraformstate.BackendS3 &&
		!stringListAllows(targetScope.AllowedRegions, candidate.Region) {
		return false
	}
	return true
}

func stringListAllows(values []string, want string) bool {
	if len(values) == 0 {
		return true
	}
	want = strings.ToLower(strings.TrimSpace(want))
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == want {
			return true
		}
	}
	return false
}

func (f *targetScopeSourceFactory) validateLocalTargetScope(
	candidate terraformstate.DiscoveryCandidate,
) error {
	if err := candidate.Validate(); err != nil {
		return err
	}
	targetScopeID := strings.TrimSpace(candidate.TargetScopeID)
	if targetScopeID == "" {
		return nil
	}
	for _, targetScope := range f.config.TargetScopes {
		if targetScope.TargetScopeID != targetScopeID {
			continue
		}
		if !targetScopeAllowsCandidate(targetScope, candidate) {
			return fmt.Errorf(
				"terraform state candidate is outside target_scope_id %q allowlist",
				targetScopeID,
			)
		}
		return nil
	}
	return fmt.Errorf("terraform state target_scope_id %q is unknown", targetScopeID)
}

func (f *targetScopeSourceFactory) s3Client(
	cacheKey string,
	credentials awsCredentialConfig,
) terraformstate.S3ObjectClient {
	f.mu.Lock()
	defer f.mu.Unlock()
	if client := f.s3Clients[cacheKey]; client != nil {
		return client
	}
	client := newAWSS3ObjectClient(credentials)
	f.s3Clients[cacheKey] = client
	return client
}

func (f *targetScopeSourceFactory) lockMetadataClient(
	cacheKey string,
	credentials awsCredentialConfig,
) terraformstate.LockMetadataClient {
	f.mu.Lock()
	defer f.mu.Unlock()
	if client := f.lockClients[cacheKey]; client != nil {
		return client
	}
	client := newAWSDynamoDBLockMetadataClient(credentials)
	f.lockClients[cacheKey] = client
	return client
}
