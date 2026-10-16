// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
)

// samlStep names the SAML store or session call a samlStepStore fails.
type samlStep int

const (
	samlStepNone samlStep = iota
	samlStepProvider
	samlStepCreateRequest
	samlStepConsume
	samlStepReplay
	samlStepResolve
	samlStepCreateSession
)

// samlStepStore is a fakeSAMLStore whose step call returns err; every other
// call answers like a successful login.
type samlStepStore struct {
	*fakeSAMLStore
	step samlStep
	err  error
}

func newSAMLStepStore(step samlStep, err error) *samlStepStore {
	return &samlStepStore{
		fakeSAMLStore: &fakeSAMLStore{
			provider: testSAMLProvider(), requestOK: true, replayOK: true, resolveOK: true,
			sessionAuth: AuthContext{
				Mode: AuthModeBrowserSession, TenantID: "tenant_a", WorkspaceID: "workspace_a",
				SubjectClass: "external_saml", SubjectIDHash: "sha256:subject",
			},
		},
		step: step,
		err:  err,
	}
}

func (s *samlStepStore) GetSAMLProvider(ctx context.Context, id string) (SAMLProviderConfig, bool, error) {
	if s.step == samlStepProvider {
		return SAMLProviderConfig{}, false, s.err
	}
	return s.fakeSAMLStore.GetSAMLProvider(ctx, id)
}

func (s *samlStepStore) CreateSAMLRequest(ctx context.Context, id string, record SAMLRequestCreateRecord) error {
	if s.step == samlStepCreateRequest {
		return s.err
	}
	return s.fakeSAMLStore.CreateSAMLRequest(ctx, id, record)
}

func (s *samlStepStore) ConsumeSAMLRequest(
	ctx context.Context, id, requestHash, relayHash string, now time.Time,
) (string, bool, error) {
	if s.step == samlStepConsume {
		return "", false, s.err
	}
	return s.fakeSAMLStore.ConsumeSAMLRequest(ctx, id, requestHash, relayHash, now)
}

func (s *samlStepStore) ReserveSAMLReplay(ctx context.Context, id, hash string, expires time.Time) (bool, error) {
	if s.step == samlStepReplay {
		return false, s.err
	}
	return s.fakeSAMLStore.ReserveSAMLReplay(ctx, id, hash, expires)
}

func (s *samlStepStore) ResolveSAMLPrincipal(
	ctx context.Context, id string, principal samlauth.Principal, now time.Time,
) (AuthContext, bool, error) {
	if s.step == samlStepResolve {
		return AuthContext{}, false, s.err
	}
	return s.fakeSAMLStore.ResolveSAMLPrincipal(ctx, id, principal, now)
}

func (s *samlStepStore) CreateBrowserSession(ctx context.Context, record BrowserSessionCreateRecord) error {
	if s.step == samlStepCreateSession {
		return s.err
	}
	return s.fakeSAMLStore.CreateBrowserSession(ctx, record)
}

// failingSAMLRequestBuilder fails every BuildSAMLRedirect call with err, or
// returns a redirect with an empty request id when err is nil.
type failingSAMLRequestBuilder struct{ err error }

func (b failingSAMLRequestBuilder) BuildSAMLRedirect(
	context.Context, SAMLProviderConfig, string,
) (SAMLAuthnRequest, error) {
	if b.err != nil {
		return SAMLAuthnRequest{}, b.err
	}
	return SAMLAuthnRequest{RedirectURL: "https://idp.example.test/sso"}, nil
}

// samlFailureHandler builds a SAML handler over store with the fixed ACS
// clock the fake verifier's assertion window accepts.
func samlFailureHandler(store *samlStepStore, newSecret func() (string, error)) *SAMLHandler {
	now := time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC)
	return &SAMLHandler{
		Store: store, Sessions: store, Verifier: fakeSAMLVerifier{},
		RequestBuilder: fakeSAMLRequestBuilder{}, NewSecret: newSecret,
		Now: func() time.Time { return now },
	}
}

// samlACSBody is a form body whose SAMLResponse answers the fake builder's
// request id.
func samlACSBody() string {
	form := url.Values{}
	form.Set("RelayState", "relay-secret")
	form.Set("SAMLResponse", testSAMLResponseForRequest("request-1"))
	return form.Encode()
}

const (
	samlMetadataPath = "/api/v0/auth/saml/providers/provider_a/metadata"
	samlLoginPath    = "/api/v0/auth/saml/providers/provider_a/login"
	samlACSPath      = "/api/v0/auth/saml/providers/provider_a/acs"
)

// TestSAMLStepFailuresAnswerFixedText drives every SAML metadata, login, and
// ACS step whose store, builder, or generator error reaches the answer
// through a backend fault, a client cancel, and an inner cancel (#7674). The
// body is the step's fixed message; the error, which can quote the provider
// config, SQL, or a host, stays on the request span.
func TestSAMLStepFailuresAnswerFixedText(t *testing.T) {
	storeFails := func(step samlStep) func(error) failure.Mounter {
		return func(err error) failure.Mounter {
			return samlFailureHandler(newSAMLStepStore(step, err), failingSecret(0, nil))
		}
	}
	acsFails := func(step samlStep) func(error) failure.Mounter {
		return func(err error) failure.Mounter {
			return formPost(samlFailureHandler(newSAMLStepStore(step, err), failingSecret(0, nil)))
		}
	}
	acsSecretFails := func(call int) func(error) failure.Mounter {
		return func(err error) failure.Mounter {
			return formPost(samlFailureHandler(newSAMLStepStore(samlStepNone, err), failingSecret(call, err)))
		}
	}
	routes := []failure.Route{
		{Name: "metadata provider lookup", Path: samlMetadataPath, Message: "failed to load saml provider", Handler: storeFails(samlStepProvider)},
		{Name: "login provider lookup", Path: samlLoginPath, Message: "failed to load saml provider", Handler: storeFails(samlStepProvider)},
		{
			Name: "login relay state generation", Path: samlLoginPath, Message: "failed to create saml request",
			Handler: func(err error) failure.Mounter {
				return samlFailureHandler(newSAMLStepStore(samlStepNone, err), failingSecret(1, err))
			},
		},
		{
			Name: "login request build", Path: samlLoginPath, Message: "failed to build saml request",
			Handler: func(err error) failure.Mounter {
				handler := samlFailureHandler(newSAMLStepStore(samlStepNone, err), failingSecret(0, nil))
				handler.RequestBuilder = failingSAMLRequestBuilder{err: err}
				return handler
			},
		},
		{Name: "login request persist", Path: samlLoginPath, Message: "failed to persist saml request", Handler: storeFails(samlStepCreateRequest)},
		{Name: "acs provider lookup", Method: http.MethodPost, Path: samlACSPath, Body: samlACSBody(), Message: "failed to load saml provider", Handler: acsFails(samlStepProvider)},
		{Name: "acs request consume", Method: http.MethodPost, Path: samlACSPath, Body: samlACSBody(), Message: "failed to consume saml request", Handler: acsFails(samlStepConsume)},
		{Name: "acs replay reserve", Method: http.MethodPost, Path: samlACSPath, Body: samlACSBody(), Message: "failed to reserve saml replay key", Handler: acsFails(samlStepReplay)},
		{Name: "acs principal resolve", Method: http.MethodPost, Path: samlACSPath, Body: samlACSBody(), Message: "failed to resolve saml principal", Handler: acsFails(samlStepResolve)},
		{Name: "acs session secret generation", Method: http.MethodPost, Path: samlACSPath, Body: samlACSBody(), Message: "failed to create browser session", Handler: acsSecretFails(1)},
		{Name: "acs csrf secret generation", Method: http.MethodPost, Path: samlACSPath, Body: samlACSBody(), Message: "failed to create browser session", Handler: acsSecretFails(2)},
		{Name: "acs session create", Method: http.MethodPost, Path: samlACSPath, Body: samlACSBody(), Message: "failed to create browser session", Handler: acsFails(samlStepCreateSession)},
	}
	runAuthFailureSteps(t, failure.Suite{Routes: routes}, errorFlowKinds)
}

// TestSAMLSentinelFailuresAnswerFixedText drives the SAML steps that have no
// caller error of their own: a metadata render failure (the provider config
// is invalid; the render never sees the request context), an AuthnRequest
// whose request id hashes to empty, and a resolved principal with no tenant
// or workspace. Each answers its fixed message with the render error or a
// package sentinel on the span, so only the server fault case applies.
func TestSAMLSentinelFailuresAnswerFixedText(t *testing.T) {
	routes := []failure.Route{
		{
			Name: "metadata render", Path: samlMetadataPath, Message: "failed to render saml metadata",
			Handler: func(err error) failure.Mounter {
				store := newSAMLStepStore(samlStepNone, err)
				store.provider.ServiceProvider = samlauth.ServiceProviderConfig{}
				return samlFailureHandler(store, failingSecret(0, nil))
			},
		},
		{
			Name: "login empty request id", Path: samlLoginPath, Message: "failed to create saml request",
			Handler: func(err error) failure.Mounter {
				handler := samlFailureHandler(newSAMLStepStore(samlStepNone, err), failingSecret(0, nil))
				handler.RequestBuilder = failingSAMLRequestBuilder{}
				return handler
			},
		},
		{
			Name: "acs principal without tenant", Method: http.MethodPost, Path: samlACSPath, Body: samlACSBody(),
			Message: "saml principal resolved without tenant or workspace",
			Handler: func(err error) failure.Mounter {
				store := newSAMLStepStore(samlStepNone, err)
				store.sessionAuth.TenantID = ""
				return formPost(samlFailureHandler(store, failingSecret(0, nil)))
			},
		},
	}
	runAuthFailureSteps(t, failure.Suite{Routes: routes}, faultOnlyKinds)
}

// formPost marks each request to inner as an HTML form post.
func formPost(inner failure.Mounter) failure.Mounter {
	return withHeaders{inner: inner, headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"}}
}
