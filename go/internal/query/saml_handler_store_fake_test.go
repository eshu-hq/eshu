// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/samlauth"
)

type fakeSAMLStore struct {
	provider               SAMLProviderConfig
	providerOK             bool
	createdRequest         SAMLRequestCreateRecord
	requestOK              bool
	returnToPath           string
	replayOK               bool
	resolveOK              bool
	sessionAuth            AuthContext
	consumedRequestIDHash  string
	consumedRelayStateHash string
	reservedReplayHash     string
	created                []BrowserSessionCreateRecord
	// consumeErr, when non-nil, makes ConsumeSAMLRequest return it, driving
	// the handler's request_consume_error DecisionUnavailable branch.
	consumeErr error
}

func (s *fakeSAMLStore) GetSAMLProvider(_ context.Context, providerID string) (SAMLProviderConfig, bool, error) {
	if providerID != s.provider.ProviderConfigID {
		return SAMLProviderConfig{}, false, nil
	}
	if !s.providerOK && s.provider.ProviderConfigID == "" {
		return SAMLProviderConfig{}, false, nil
	}
	return s.provider, true, nil
}

func (s *fakeSAMLStore) ConsumeSAMLRequest(
	_ context.Context,
	_ string,
	requestIDHash string,
	relayStateHash string,
	_ time.Time,
) (string, bool, error) {
	s.consumedRequestIDHash = requestIDHash
	s.consumedRelayStateHash = relayStateHash
	if s.consumeErr != nil {
		return "", false, s.consumeErr
	}
	return s.returnToPath, s.requestOK, nil
}

func (s *fakeSAMLStore) CreateSAMLRequest(
	_ context.Context,
	_ string,
	record SAMLRequestCreateRecord,
) error {
	s.createdRequest = record
	return nil
}

func (s *fakeSAMLStore) ReserveSAMLReplay(
	_ context.Context,
	_ string,
	replayHash string,
	_ time.Time,
) (bool, error) {
	s.reservedReplayHash = replayHash
	return s.replayOK, nil
}

func (s *fakeSAMLStore) ResolveSAMLPrincipal(
	_ context.Context,
	_ string,
	_ samlauth.Principal,
	_ time.Time,
) (AuthContext, bool, error) {
	return s.sessionAuth, s.resolveOK, nil
}

func (s *fakeSAMLStore) CreateBrowserSession(
	_ context.Context,
	record BrowserSessionCreateRecord,
) error {
	s.created = append(s.created, record)
	return nil
}

func (s *fakeSAMLStore) RevokeBrowserSession(context.Context, string, time.Time) error {
	return nil
}

func (s *fakeSAMLStore) SwitchBrowserSessionWorkspace(
	context.Context,
	string,
	string,
	string,
	time.Time,
) (AuthContext, bool, error) {
	return AuthContext{}, false, nil
}
