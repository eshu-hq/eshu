package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestNewMCPQueryRouterWiresCollectorListReadiness guards against the regression
// where the MCP server built its own PackageRegistry, CICD, and SupplyChain
// handlers without a CollectorListReadinessStore. When the field is nil,
// attachCollectorListReadiness omits collector_readiness for the gated MCP
// list_* tools, so the empty-vs-not_configured contract silently fails on the
// primary MCP path even though the API helper wires it. Each handler must carry
// the readiness probe so the MCP path returns the envelope.
func TestNewMCPQueryRouterWiresCollectorListReadiness(t *testing.T) {
	t.Parallel()

	router := newMCPQueryRouter(
		nil,
		nil,
		nil,
		staticStatusReader{},
		query.ProfileProduction,
		query.GraphBackendNornicDB,
		nil,
		nil,
		"",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
		false,
	)

	if router.PackageRegistry == nil || router.PackageRegistry.CollectorReadiness == nil {
		t.Fatal("newMCPQueryRouter().PackageRegistry.CollectorReadiness = nil, want wired readiness probe")
	}
	if router.CICD == nil || router.CICD.CollectorReadiness == nil {
		t.Fatal("newMCPQueryRouter().CICD.CollectorReadiness = nil, want wired readiness probe")
	}
	if router.SupplyChain == nil || router.SupplyChain.CollectorReadiness == nil {
		t.Fatal("newMCPQueryRouter().SupplyChain.CollectorReadiness = nil, want wired readiness probe")
	}
}
