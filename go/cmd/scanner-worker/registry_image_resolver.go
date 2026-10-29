// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/distribution"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/ecr"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var errRegistryImageUnauthorized = errors.New("registry image target unauthorized")

// registryImageTargetStore loads the immutable admitted target for one claimed
// work item. Its implementation must reject stale claims and disabled instances.
type registryImageTargetStore interface {
	GetScannerImageTarget(context.Context, string, string, int64, string) (workflow.ScannerImageTarget, error)
}

type registryDomainConfig struct {
	AuthorizationDomain string `json:"authorization_domain"`
	Registry            string `json:"registry"`
	BaseURL             string `json:"base_url"`
	AuthMode            string `json:"auth_mode"`
	UsernameEnv         string `json:"username_env"`
	PasswordEnv         string `json:"password_env"`
	BearerTokenEnv      string `json:"bearer_token_env"`
	AWSRegion           string `json:"aws_region"`
	AWSProfile          string `json:"aws_profile"`
}

type registryImageTargetResolver struct {
	store   registryImageTargetStore
	domains []registryDomainConfig
	getenv  func(string) string
}

func newRegistryImageResolver(store registryImageTargetStore, domains []registryDomainConfig, getenv func(string) string) registryImageTargetResolver {
	return registryImageTargetResolver{store: store, domains: append([]registryDomainConfig(nil), domains...), getenv: getenv}
}

func (r registryImageTargetResolver) ResolveImage(ctx context.Context, input scannerworker.ClaimInput) (registryImageTarget, error) {
	if r.store == nil || r.getenv == nil {
		return registryImageTarget{}, errRegistryImageUnauthorized
	}
	target, err := r.store.GetScannerImageTarget(ctx, input.WorkItemID, input.ClaimID, input.FencingToken, input.OwnerID)
	if err != nil {
		if errors.Is(err, postgres.ErrScannerTargetNotAuthorized) {
			return registryImageTarget{}, errRegistryImageUnauthorized
		}
		return registryImageTarget{}, fmt.Errorf("registry image target read failed")
	}
	if target.Validate() != nil || target.WorkItemID != input.WorkItemID || target.ScanTargetID != input.Target.ScopeID || len(target.Sources) != 1 {
		return registryImageTarget{}, errRegistryImageUnauthorized
	}
	source := target.Sources[0]
	var domain registryDomainConfig
	matches := 0
	for _, candidate := range r.domains {
		if candidate.AuthorizationDomain == target.AuthorizationDomain {
			domain = candidate
			matches++
		}
	}
	if matches != 1 || domain.Registry != source.Registry {
		return registryImageTarget{}, errRegistryImageUnauthorized
	}
	var currentDomain registryDomainConfig
	if len(target.RegistryDomainConfig) == 0 || json.Unmarshal(target.RegistryDomainConfig, &currentDomain) != nil || currentDomain != domain {
		return registryImageTarget{}, errRegistryImageUnauthorized
	}
	baseURL, err := url.Parse(domain.BaseURL)
	if err != nil || !validRegistryBaseURL(baseURL, domain.Registry) {
		return registryImageTarget{}, errRegistryImageUnauthorized
	}
	auth, err := r.credentials(ctx, domain)
	if err != nil {
		return registryImageTarget{}, errRegistryImageUnauthorized
	}
	client, err := distribution.NewClient(distribution.ClientConfig{
		BaseURL: domain.BaseURL, Username: auth.username, Password: auth.password, BearerToken: auth.bearerToken,
	})
	if err != nil {
		return registryImageTarget{}, errRegistryImageUnauthorized
	}
	return registryImageTarget{
		ScanTargetID: target.ScanTargetID, Client: client, Repository: source.Repository,
		ManifestDigest: target.ManifestDigest, RuntimeDigest: source.RuntimeDigest,
		RuntimeDigestKind: string(source.RuntimeDigestKind),
	}, nil
}

type registryCredentials struct {
	username    string
	password    string
	bearerToken string
}

func (r registryImageTargetResolver) credentials(ctx context.Context, domain registryDomainConfig) (registryCredentials, error) {
	switch domain.AuthMode {
	case "anonymous":
		return registryCredentials{}, nil
	case "basic_env":
		if domain.UsernameEnv == "" || domain.PasswordEnv == "" {
			return registryCredentials{}, errRegistryImageUnauthorized
		}
		username, password := strings.TrimSpace(r.getenv(domain.UsernameEnv)), r.getenv(domain.PasswordEnv)
		if username == "" || password == "" {
			return registryCredentials{}, errRegistryImageUnauthorized
		}
		return registryCredentials{username: username, password: password}, nil
	case "bearer_env":
		if domain.BearerTokenEnv == "" {
			return registryCredentials{}, errRegistryImageUnauthorized
		}
		token := r.getenv(domain.BearerTokenEnv)
		if token == "" {
			return registryCredentials{}, errRegistryImageUnauthorized
		}
		return registryCredentials{bearerToken: token}, nil
	case "ecr":
		if domain.AWSRegion == "" {
			return registryCredentials{}, errRegistryImageUnauthorized
		}
		options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(domain.AWSRegion)}
		if domain.AWSProfile != "" {
			options = append(options, awsconfig.WithSharedConfigProfile(domain.AWSProfile))
		}
		aws, err := awsconfig.LoadDefaultConfig(ctx, options...)
		if err != nil {
			return registryCredentials{}, errRegistryImageUnauthorized
		}
		credentials, err := ecr.GetDistributionCredentials(ctx, awsecr.NewFromConfig(aws))
		if err != nil {
			return registryCredentials{}, errRegistryImageUnauthorized
		}
		endpoint, err := url.Parse(credentials.ProxyEndpoint)
		if err != nil || endpoint.Host != domain.Registry || endpoint.Scheme != "https" {
			return registryCredentials{}, errRegistryImageUnauthorized
		}
		return registryCredentials{username: credentials.Username, password: credentials.Password}, nil
	default:
		return registryCredentials{}, errRegistryImageUnauthorized
	}
}

func isLoopbackRegistry(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "[::1]", "::1":
		return true
	}
	return false
}

func parseRegistryDomains(raw string) ([]registryDomainConfig, error) {
	var decoded struct {
		RegistryDomains []registryDomainConfig `json:"registry_domains"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("decode scanner registry domains")
	}
	if len(decoded.RegistryDomains) == 0 {
		return nil, fmt.Errorf("scanner registry domains are required")
	}
	seen := make(map[string]struct{}, len(decoded.RegistryDomains))
	for _, domain := range decoded.RegistryDomains {
		if domain.AuthorizationDomain == "" || domain.Registry == "" || domain.BaseURL == "" {
			return nil, fmt.Errorf("invalid scanner registry domain")
		}
		if _, ok := seen[domain.AuthorizationDomain]; ok {
			return nil, fmt.Errorf("duplicate scanner registry domain")
		}
		seen[domain.AuthorizationDomain] = struct{}{}
		parsed, err := url.Parse(domain.BaseURL)
		if err != nil || !validRegistryBaseURL(parsed, domain.Registry) {
			return nil, fmt.Errorf("invalid scanner registry endpoint")
		}
		switch domain.AuthMode {
		case "anonymous":
			if domain.UsernameEnv != "" || domain.PasswordEnv != "" || domain.BearerTokenEnv != "" {
				return nil, fmt.Errorf("invalid anonymous scanner registry auth")
			}
		case "basic_env":
			if domain.UsernameEnv == "" || domain.PasswordEnv == "" || domain.BearerTokenEnv != "" {
				return nil, fmt.Errorf("invalid scanner registry basic auth")
			}
		case "bearer_env":
			if domain.BearerTokenEnv == "" || domain.UsernameEnv != "" || domain.PasswordEnv != "" {
				return nil, fmt.Errorf("invalid scanner registry bearer auth")
			}
		case "ecr":
			if domain.AWSRegion == "" || domain.UsernameEnv != "" || domain.PasswordEnv != "" || domain.BearerTokenEnv != "" {
				return nil, fmt.Errorf("invalid scanner ECR auth")
			}
		default:
			return nil, fmt.Errorf("unsupported scanner registry auth mode")
		}
	}
	return decoded.RegistryDomains, nil
}

func validRegistryBaseURL(parsed *url.URL, registry string) bool {
	if parsed == nil || parsed.Host != registry || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	return parsed.Scheme == "http" && isLoopbackRegistry(parsed.Hostname())
}
