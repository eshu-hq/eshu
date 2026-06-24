// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidclogin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type oidcConnector struct {
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
	subjectClaim string
	emailClaim   string
	groupsClaim  string
}

// NewOIDCConnector constructs a connector backed by provider discovery and
// JWKS verification.
func NewOIDCConnector(ctx context.Context, provider ProviderConfig) (Connector, error) {
	clientSecret, err := readClientSecret(provider.ClientSecretFile)
	if err != nil {
		return nil, err
	}
	oidcProvider, err := oidc.NewProvider(ctx, provider.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover oidc provider: %w", err)
	}
	oauth2Config := oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: clientSecret,
		RedirectURL:  provider.RedirectURL,
		Endpoint:     oidcProvider.Endpoint(),
		Scopes:       append([]string(nil), provider.Scopes...),
	}
	return &oidcConnector{
		oauth2Config: oauth2Config,
		verifier:     oidcProvider.Verifier(&oidc.Config{ClientID: provider.ClientID}),
		subjectClaim: provider.SubjectClaim,
		emailClaim:   provider.EmailClaim,
		groupsClaim:  provider.GroupsClaim,
	}, nil
}

func (c *oidcConnector) AuthCodeURL(state string, nonce string) string {
	return c.oauth2Config.AuthCodeURL(state, oidc.Nonce(nonce))
}

func (c *oidcConnector) Exchange(ctx context.Context, code string) (TokenSet, error) {
	token, err := c.oauth2Config.Exchange(ctx, code)
	if err != nil {
		return TokenSet{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || strings.TrimSpace(rawIDToken) == "" {
		return TokenSet{}, errors.New("id token missing from provider response")
	}
	return TokenSet{IDToken: rawIDToken}, nil
}

func (c *oidcConnector) VerifyIDToken(ctx context.Context, rawIDToken string) (VerifiedClaims, error) {
	idToken, err := c.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return VerifiedClaims{}, fmt.Errorf("verify id token: %w", err)
	}
	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		return VerifiedClaims{}, fmt.Errorf("decode id token claims: %w", err)
	}
	subject := idToken.Subject
	if c.subjectClaim != "" && c.subjectClaim != "sub" {
		subject = stringClaim(claims, c.subjectClaim)
	}
	return VerifiedClaims{
		Subject: subject,
		Nonce:   idToken.Nonce,
		Email:   stringClaim(claims, c.emailClaim),
		Groups:  stringSliceClaim(claims, c.groupsClaim),
	}, nil
}

func readClientSecret(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read oidc client secret file: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

func stringClaim(claims map[string]any, name string) string {
	value, ok := claims[strings.TrimSpace(name)]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func stringSliceClaim(claims map[string]any, name string) []string {
	value, ok := claims[strings.TrimSpace(name)]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return cleanStrings(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		return cleanStrings(values)
	case string:
		return cleanStrings(strings.Split(typed, ","))
	default:
		return nil
	}
}
