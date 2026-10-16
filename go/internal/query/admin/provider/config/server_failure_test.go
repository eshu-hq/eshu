// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// providerConfigAdminScope adds the shared tenant admin to the request context.
func providerConfigAdminScope(ctx context.Context) context.Context {
	return auth.ContextWithAuthContext(ctx, providerConfigAdminAuth())
}

// runFailureSteps drives every route through the fault, client-cancel, and
// inner-cancel cases. Provider-config reads and writes run on the writer
// database, so the reader-fence case does not apply.
func runFailureSteps(t *testing.T, suite failure.Suite) {
	t.Helper()
	for _, route := range suite.Routes {
		for _, tc := range failure.Cases() {
			if tc.Kind == failure.ReaderFence {
				continue
			}
			t.Run(route.Name+"/"+tc.Name, func(t *testing.T) {
				if err := failure.Check(suite, route, tc); err != nil {
					t.Error(err)
				}
			})
		}
	}
}

// TestProviderConfigReadFailuresAnswerFixedText drives every provider-config
// read through a backend fault and a client cancel (#7674).
func TestProviderConfigReadFailuresAnswerFixedText(t *testing.T) {
	reads := func(err error) failure.Mounter {
		return &ReadHandler{Store: &fakeAdminProviderConfigReadStore{forceErr: err}}
	}
	routes := []failure.Route{
		{Name: "list", Path: "/api/v0/auth/admin/provider-configs", Message: providerConfigListFailedMessage},
		{Name: "get", Path: "/api/v0/auth/admin/provider-configs/pc_1", Message: providerConfigReadFailedMessage},
		{
			Name: "revisions", Path: "/api/v0/auth/admin/provider-configs/pc_1/revisions",
			Message: providerConfigRevisionListFailedMessage,
		},
	}
	for i := range routes {
		routes[i].Handler = reads
		routes[i].Scope = providerConfigAdminScope
	}
	runFailureSteps(t, failure.Suite{Routes: routes})
}

// TestProviderConfigMutationFailuresAnswerFixedText drives every
// provider-config write, and the connection test, through a backend fault and
// a client cancel (#7674). An unclassified store error is the write's 500; a
// tester error is the connection test's 500.
func TestProviderConfigMutationFailuresAnswerFixedText(t *testing.T) {
	writes := func(err error) failure.Mounter {
		return &MutationHandler{
			Store:     &fakeAdminProviderConfigMutationStore{forceErr: err},
			Tester:    &fakeProviderConfigConnectionTester{result: ConnectionTestResult{OK: true, RevisionID: "rev_1"}},
			Audit:     &testutil.FakeGovernanceAuditAppender{},
			ReadStore: defaultProviderConfigLoginReadyReadStore(),
		}
	}
	routes := []failure.Route{
		{Name: "create", Path: "/api/v0/auth/admin/provider-configs", Body: validOIDCCreateBody},
		{Name: "update", Path: "/api/v0/auth/admin/provider-configs/pc_1", Body: validOIDCCreateBody},
		{Name: "revert", Path: "/api/v0/auth/admin/provider-configs/pc_1/revert", Body: `{"revision_id":"rev_1"}`},
		{Name: "enable", Path: "/api/v0/auth/admin/provider-configs/pc_1/enable"},
		{Name: "disable", Path: "/api/v0/auth/admin/provider-configs/pc_1/disable"},
	}
	for i := range routes {
		routes[i].Method = http.MethodPost
		routes[i].Message = providerConfigWriteFailedMessage
		routes[i].Handler = writes
		routes[i].Scope = providerConfigAdminScope
	}
	routes = append(routes, failure.Route{
		Name: "test connection", Method: http.MethodPost,
		Path:    "/api/v0/auth/admin/provider-configs/pc_1/test-connection",
		Message: connectionTestRunFailedMessage, Scope: providerConfigAdminScope,
		Handler: func(err error) failure.Mounter {
			return &MutationHandler{
				Store:  &fakeAdminProviderConfigMutationStore{},
				Tester: &fakeProviderConfigConnectionTester{forceErr: err},
				Audit:  &testutil.FakeGovernanceAuditAppender{},
			}
		},
	})
	runFailureSteps(t, failure.Suite{Routes: routes})
}
