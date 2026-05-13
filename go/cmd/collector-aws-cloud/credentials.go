package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
)

type awsCredentialProvider struct{}

func (p awsCredentialProvider) Acquire(
	ctx context.Context,
	target awsruntime.Target,
	leaseExpiresAt time.Time,
) (awsruntime.CredentialLease, error) {
	cfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(target.Region),
		awsconfig.WithRetryMode(aws.RetryModeAdaptive),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	switch target.Credentials.Mode {
	case awsruntime.CredentialModeLocalWorkloadIdentity:
		return &awsCredentialLease{config: cfg}, nil
	case awsruntime.CredentialModeCentralAssumeRole:
		provider := stscreds.NewAssumeRoleProvider(
			sts.NewFromConfig(cfg),
			target.Credentials.RoleARN,
			func(options *stscreds.AssumeRoleOptions) {
				options.RoleSessionName = roleSessionName(target)
				if externalID := strings.TrimSpace(target.Credentials.ExternalID); externalID != "" {
					options.ExternalID = aws.String(externalID)
				}
				if !leaseExpiresAt.IsZero() {
					duration := time.Until(leaseExpiresAt)
					// STS enforces a 15 minute minimum. Shorter claim leases
					// still release the in-process credential lease on claim
					// completion or failure.
					if duration >= 15*time.Minute {
						options.Duration = duration
					}
				}
			},
		)
		credentials, err := provider.Retrieve(ctx)
		if err != nil {
			return nil, fmt.Errorf("assume AWS role: %w", err)
		}
		credentialProvider := newClaimCredentialProvider(credentials)
		credentialCache := aws.NewCredentialsCache(credentialProvider)
		cfg.Credentials = credentialCache
		return &awsCredentialLease{
			config:             cfg,
			credentialProvider: credentialProvider,
			credentialCache:    credentialCache,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported AWS credential mode %q", target.Credentials.Mode)
	}
}

type awsCredentialLease struct {
	config             aws.Config
	credentialProvider *claimCredentialProvider
	credentialCache    *aws.CredentialsCache
}

func (l *awsCredentialLease) Release() error {
	if l.credentialCache != nil {
		l.credentialCache.Invalidate()
	}
	if l.credentialProvider != nil {
		l.credentialProvider.Release()
	}
	l.config.Credentials = aws.AnonymousCredentials{}
	return nil
}

type claimCredentialProvider struct {
	mu          sync.Mutex
	credentials aws.Credentials
	released    bool
}

func newClaimCredentialProvider(credentials aws.Credentials) *claimCredentialProvider {
	return &claimCredentialProvider{credentials: credentials}
}

func (p *claimCredentialProvider) Retrieve(context.Context) (aws.Credentials, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.released {
		return aws.Credentials{}, errors.New("AWS credential lease has been released")
	}
	if !p.credentials.HasKeys() {
		return aws.Credentials{}, errors.New("AWS credential lease has no credentials")
	}
	return p.credentials, nil
}

func (p *claimCredentialProvider) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.credentials = aws.Credentials{}
	p.released = true
}

func roleSessionName(target awsruntime.Target) string {
	account := strings.TrimSpace(target.AccountID)
	service := strings.TrimSpace(target.ServiceKind)
	if account == "" {
		account = "unknown"
	}
	if service == "" {
		service = "unknown"
	}
	return "eshu-" + account + "-" + service
}
