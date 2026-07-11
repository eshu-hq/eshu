// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidclogin

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Shared test fakes for service_test.go, split into their own file to keep
// service_test.go under the repo's 500-line cap.

func fakeConnectorFactory(t *testing.T) ConnectorFactory {
	t.Helper()
	return func(context.Context, ProviderConfig) (Connector, error) {
		return &fakeConnector{}, nil
	}
}

type fakeStateStore struct {
	created      []StateRecord
	consume      StateRecord
	consumeOK    bool
	consumedHash string
}

func (s *fakeStateStore) CreateState(_ context.Context, record StateRecord) error {
	s.created = append(s.created, record)
	return nil
}

func (s *fakeStateStore) ConsumeState(
	_ context.Context,
	stateHash string,
	_ time.Time,
) (StateRecord, bool, error) {
	s.consumedHash = stateHash
	return s.consume, s.consumeOK, nil
}

type failingGrantResolver struct {
	called bool
}

func (r *failingGrantResolver) ResolveGroupGrants(
	context.Context,
	GrantQuery,
) (GrantResolution, bool, error) {
	r.called = true
	return GrantResolution{}, false, errors.New("grant resolver should not be called")
}

// fixedGrantResolver simulates a live-authority resolver (the DB-backed group
// mapping path), which is allowed to populate PolicyRevisionHash — unlike
// StaticGrantResolver (#5038, see static_grants_test.go), it has a real
// authority for the workspace's current policy revision.
type fixedGrantResolver struct {
	resolution GrantResolution
}

func (r fixedGrantResolver) ResolveGroupGrants(
	context.Context,
	GrantQuery,
) (GrantResolution, bool, error) {
	return r.resolution, true, nil
}

type fakeConnector struct {
	claims          VerifiedClaims
	exchangedCode   string
	verifiedIDToken string
}

func (c *fakeConnector) AuthCodeURL(state string, nonce string) string {
	return "https://idp.example.test/authorize?state=" + state + "&nonce=" + nonce
}

func (c *fakeConnector) Exchange(_ context.Context, code string) (TokenSet, error) {
	c.exchangedCode = code
	return TokenSet{IDToken: "id-token"}, nil
}

func (c *fakeConnector) VerifyIDToken(_ context.Context, rawIDToken string) (VerifiedClaims, error) {
	c.verifiedIDToken = rawIDToken
	if c.claims.Subject == "" {
		return VerifiedClaims{Subject: "external-subject", Nonce: "nonce-secret", Groups: []string{"Eshu Developers"}}, nil
	}
	return c.claims, nil
}

func sequenceOIDCSecrets(values ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		value := values[index]
		index++
		return value, nil
	}
}
