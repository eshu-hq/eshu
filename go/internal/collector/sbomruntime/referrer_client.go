// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomruntime

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ecr"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// ecrProvider is the target provider value that selects the AWS ECR
// token-exchange auth path for an oci_referrer fetch.
const ecrProvider = "ecr"

// ECRAuthorizationClientFunc returns an AWS ECR GetAuthorizationToken client for
// one target. It is the seam where the AWS default credential chain, region,
// and profile are wired by the runtime, keeping AWS configuration out of this
// package.
type ECRAuthorizationClientFunc func(ctx context.Context, target TargetConfig) (ecr.AuthorizationTokenAPI, error)

// ECRReferrerClientFactory builds OCI Distribution clients for oci_referrer
// targets, performing the ECR GetAuthorizationToken exchange for provider=ecr
// targets and deferring to static credentials for every other provider.
type ECRReferrerClientFactory struct {
	// AuthorizationClient supplies the ECR token API for a target. It is
	// required to serve provider=ecr targets.
	AuthorizationClient ECRAuthorizationClientFunc
	// HTTPClient is the bounded HTTP client used for Distribution calls.
	HTTPClient *http.Client
	// Logger records the ECR token-exchange path for operators. It never logs
	// credentials or decoded tokens. A nil logger disables the log.
	Logger *slog.Logger
}

// ReferrerClient returns an ECR-authenticated Distribution client for
// provider=ecr targets. For any other provider it returns a nil client so the
// HTTPProvider falls back to its static-credential client.
func (f ECRReferrerClientFactory) ReferrerClient(ctx context.Context, target TargetConfig) (ReferrerClient, error) {
	if !strings.EqualFold(strings.TrimSpace(target.Provider), ecrProvider) {
		return nil, nil
	}
	if f.AuthorizationClient == nil {
		return nil, fmt.Errorf("ecr authorization client factory is required for provider=ecr referrer targets")
	}
	authClient, err := f.AuthorizationClient(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("build ecr authorization client: %w", err)
	}
	registryHost := strings.TrimSpace(target.RegistryHost)
	if registryHost == "" {
		registryHost = strings.TrimSpace(target.Registry)
	}
	client, err := ecr.NewReferrerClient(ctx, ecr.ReferrerClientOptions{
		AuthorizationClient: authClient,
		RegistryHost:        registryHost,
		HTTPClient:          f.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	if f.Logger != nil {
		f.Logger.InfoContext(
			ctx, "ecr oci_referrer auth via GetAuthorizationToken exchange",
			log.Provider(ecrProvider),
			log.ScopeID(strings.TrimSpace(target.ScopeID)),
			slog.String("repository", strings.TrimSpace(target.Repository)),
			slog.String("registry_host", registryHost),
		)
	}
	return client, nil
}
