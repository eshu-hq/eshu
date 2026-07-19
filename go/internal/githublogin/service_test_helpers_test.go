// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package githublogin

import (
	"context"
	"time"
)

// Shared test fakes for service_test.go, split into their own file to keep
// service_test.go under the repo's 500-line cap (mirrors oidclogin's
// service_test_helpers_test.go split).

func fakeConnectorFactory(identity Identity) ConnectorFactory {
	return func(context.Context, ProviderConfig) (Connector, error) {
		return &fakeConnector{identity: identity}, nil
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

type fakeConnector struct {
	identity      Identity
	exchangedCode string
	fetchedOrgs   []string
	exchangeErr   error
	identityErr   error
}

func (c *fakeConnector) AuthCodeURL(state string) string {
	return "https://github.example.test/login/oauth/authorize?state=" + state
}

func (c *fakeConnector) Exchange(_ context.Context, code string) (TokenSet, error) {
	c.exchangedCode = code
	if c.exchangeErr != nil {
		return TokenSet{}, c.exchangeErr
	}
	return TokenSet{AccessToken: "access-token"}, nil
}

func (c *fakeConnector) FetchIdentity(_ context.Context, _ string, allowedOrgs []string) (Identity, error) {
	c.fetchedOrgs = allowedOrgs
	if c.identityErr != nil {
		return Identity{}, c.identityErr
	}
	return c.identity, nil
}

func sequenceSecrets(values ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		value := values[index]
		index++
		return value, nil
	}
}
