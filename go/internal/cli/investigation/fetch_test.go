// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation_test

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/investigation"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestDefaultDepsWiresEveryFamily guards against a family whose fetch is left
// nil in DefaultDeps: BuildPacket would panic on a nil func rather than fail
// with a message, and no other test in this file would notice because they all
// inject their own fetches.
func TestDefaultDepsWiresEveryFamily(t *testing.T) {
	t.Parallel()

	deps := investigation.DefaultDeps()
	if deps.FetchSupplyChainExplain == nil {
		t.Error("FetchSupplyChainExplain is nil")
	}
	if deps.FetchAdmissionDecisions == nil {
		t.Error("FetchAdmissionDecisions is nil")
	}
	if deps.FetchDriftFindings == nil {
		t.Error("FetchDriftFindings is nil")
	}
}

// recordClient captures the path and body each fetch sends so the route and
// query-string construction are covered without a live API.
type recordClient struct {
	path string
	body any
	err  error
}

func (c *recordClient) GetEnvelope(path string, _ any) error {
	c.path = path
	return c.err
}

func (c *recordClient) PostEnvelope(path string, body, _ any) error {
	c.path = path
	c.body = body
	return c.err
}

func TestDefaultFetchesBuildTheirRoutes(t *testing.T) {
	t.Parallel()

	deps := investigation.DefaultDeps()

	t.Run("supply-chain explain sets only the non-empty filter fields", func(t *testing.T) {
		t.Parallel()

		client := &recordClient{}
		if _, err := deps.FetchSupplyChainExplain(client, query.SupplyChainImpactExplanationFilter{
			AdvisoryID: "GHSA-x", PackageID: "pkg:npm/y", CVEID: "   ",
		}); err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if !strings.HasPrefix(client.path, "/api/v0/supply-chain/impact/explain?") {
			t.Fatalf("path = %q", client.path)
		}
		values, err := url.ParseQuery(strings.SplitN(client.path, "?", 2)[1])
		if err != nil {
			t.Fatalf("parse query: %v", err)
		}
		if values.Get("advisory_id") != "GHSA-x" || values.Get("package_id") != "pkg:npm/y" {
			t.Fatalf("query = %v", values)
		}
		if values.Has("cve_id") || values.Has("finding_id") {
			t.Fatalf("whitespace-only and empty filter fields must be omitted, got %v", values)
		}
	})

	t.Run("admission decisions carries the params through", func(t *testing.T) {
		t.Parallel()

		client := &recordClient{}
		if _, err := deps.FetchAdmissionDecisions(client, url.Values{"scope_id": {"s1"}}); err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if client.path != "/api/v0/evidence/admission-decisions?scope_id=s1" {
			t.Fatalf("path = %q", client.path)
		}
	})

	t.Run("drift findings posts the body", func(t *testing.T) {
		t.Parallel()

		client := &recordClient{}
		body := map[string]any{"scope_id": "acct1"}
		if _, err := deps.FetchDriftFindings(client, body); err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if client.path != "/api/v0/cloud/runtime-drift/findings" {
			t.Fatalf("path = %q", client.path)
		}
		got, ok := client.body.(map[string]any)
		if !ok || got["scope_id"] != "acct1" {
			t.Fatalf("body = %v", client.body)
		}
	})

	t.Run("a transport error reaches the caller unwrapped", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("request failed: connection refused")
		if _, err := deps.FetchSupplyChainExplain(&recordClient{err: sentinel},
			query.SupplyChainImpactExplanationFilter{FindingID: "f"}); !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want the transport error preserved", err)
		}
	})
}
