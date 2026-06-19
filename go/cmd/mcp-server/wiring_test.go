package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestWireAPIReturnsResolveAPIKeyErrorBeforeConnectingDatastores(t *testing.T) {
	t.Setenv("ESHU_API_KEY", "")
	t.Setenv("ESHU_AUTO_GENERATE_API_KEY", "true")
	t.Setenv("ESHU_HOME", "/dev/null/eshu")

	_, _, _, err := wireAPI(context.Background(), func(string) string {
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want non-nil")
	}
}

func TestWireAPIReturnsInvalidQueryProfileErrorBeforeConnectingDatastores(t *testing.T) {
	_, _, _, err := wireAPI(context.Background(), func(key string) string {
		if key == "ESHU_QUERY_PROFILE" {
			return "not-a-real-profile"
		}
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "load query profile") {
		t.Fatalf("wireAPI() error = %q, want load query profile context", err)
	}
}

func TestWireAPIReturnsInvalidGraphBackendErrorBeforeConnectingDatastores(t *testing.T) {
	_, _, _, err := wireAPI(context.Background(), func(key string) string {
		if key == "ESHU_GRAPH_BACKEND" {
			return "not-a-real-backend"
		}
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "load graph backend") {
		t.Fatalf("wireAPI() error = %q, want load graph backend context", err)
	}
}

func TestWireAPIReturnsInvalidSemanticProviderProfilesBeforeConnectingDatastores(t *testing.T) {
	_, _, _, err := wireAPI(context.Background(), func(key string) string {
		if key == semanticprofile.EnvProviderProfilesJSON {
			return `[{"profile_id":"semantic-docs-default","provider_kind":"deepseek","credential_source":{"kind":"environment_variable","handle":"sk-live-123"},"model_id":"deepseek-chat","source_classes":["documentation"],"source_policy_configured":true}]`
		}
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "load semantic provider profiles") {
		t.Fatalf("wireAPI() error = %q, want semantic provider profile context", err)
	}
}

func TestWireAPIReturnsInvalidSemanticPolicyBeforeConnectingDatastores(t *testing.T) {
	_, _, _, err := wireAPI(context.Background(), func(key string) string {
		if key == semanticpolicy.EnvPolicyJSON {
			return `{"enabled":true,"rules":[]}`
		}
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "load semantic extraction policy") {
		t.Fatalf("wireAPI() error = %q, want semantic extraction policy context", err)
	}
}

func TestNewMCPQueryRouterMountsMCPBackedHandlers(t *testing.T) {
	t.Parallel()

	router := newMCPQueryRouter(
		nil,
		nil,
		nil,
		staticStatusReader{},
		query.ProfileLocalFullStack,
		query.GraphBackendNornicDB,
		nil,
		nil,
		"",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
	)

	if router.IaC == nil {
		t.Fatal("newMCPQueryRouter().IaC = nil, want MCP find_dead_iac route mounted")
	}
	if router.IaC.Reachability == nil {
		t.Fatal("newMCPQueryRouter().IaC.Reachability = nil, want materialized reachability store")
	}
	if router.Evidence == nil {
		t.Fatal("newMCPQueryRouter().Evidence = nil, want relationship evidence drilldown route mounted")
	}
	if router.Evidence.AdmissionDecisions == nil {
		t.Fatal("newMCPQueryRouter().Evidence.AdmissionDecisions = nil, want admission decision read model store")
	}
	if router.Documentation == nil {
		t.Fatal("newMCPQueryRouter().Documentation = nil, want documentation findings routes mounted")
	}
	if router.SupplyChain == nil {
		t.Fatal("newMCPQueryRouter().SupplyChain = nil, want SBOM attestation attachment route mounted")
	}
	if router.SupplyChain.SBOMAttachments == nil {
		t.Fatal("newMCPQueryRouter().SupplyChain.SBOMAttachments = nil, want Postgres read model store")
	}
	if router.SupplyChain.ContainerImageIdentities == nil {
		t.Fatal("newMCPQueryRouter().SupplyChain.ContainerImageIdentities = nil, want Postgres read model store")
	}
	if router.SupplyChain.AdvisoryEvidence == nil {
		t.Fatal("newMCPQueryRouter().SupplyChain.AdvisoryEvidence = nil, want Postgres read model store")
	}
	if router.SupplyChain.ImpactFindings == nil {
		t.Fatal("newMCPQueryRouter().SupplyChain.ImpactFindings = nil, want Postgres read model store")
	}
	if router.SupplyChain.ImpactExplanations == nil {
		t.Fatal("newMCPQueryRouter().SupplyChain.ImpactExplanations = nil, want Postgres explain store")
	}
	if router.SupplyChain.Readiness == nil {
		t.Fatal("newMCPQueryRouter().SupplyChain.Readiness = nil, want Postgres readiness store")
	}
	if router.SupplyChain.SecurityAlerts == nil {
		t.Fatal("newMCPQueryRouter().SupplyChain.SecurityAlerts = nil, want Postgres security alert reconciliation store")
	}
	if router.CICD == nil {
		t.Fatal("newMCPQueryRouter().CICD = nil, want CI/CD run correlation route mounted")
	}
	if router.CICD.Correlations == nil {
		t.Fatal("newMCPQueryRouter().CICD.Correlations = nil, want Postgres read model store")
	}
	if router.ServiceCatalog == nil {
		t.Fatal("newMCPQueryRouter().ServiceCatalog = nil, want service catalog route mounted")
	}
	if router.SemanticSearch == nil {
		t.Fatal("newMCPQueryRouter().SemanticSearch = nil, want semantic search route mounted")
	}
	if router.SemanticSearch.Index == nil {
		t.Fatal("newMCPQueryRouter().SemanticSearch.Index = nil, want Postgres search-index store")
	}
	if router.ServiceCatalog.Correlations == nil {
		t.Fatal("newMCPQueryRouter().ServiceCatalog.Correlations = nil, want Postgres read model store")
	}
	if router.ObservabilityCoverage == nil {
		t.Fatal("newMCPQueryRouter().ObservabilityCoverage = nil, want observability coverage route mounted")
	}
	if router.ObservabilityCoverage.Correlations == nil {
		t.Fatal("newMCPQueryRouter().ObservabilityCoverage.Correlations = nil, want Postgres read model store")
	}
	if router.SemanticEvidence == nil {
		t.Fatal("newMCPQueryRouter().SemanticEvidence = nil, want semantic evidence route mounted")
	}
}

func TestNewMCPQueryRouterUsesSuppliedStatusReader(t *testing.T) {
	t.Parallel()

	reader := staticStatusReader{}
	router := newMCPQueryRouter(
		nil,
		nil,
		nil,
		reader,
		query.ProfileLocalFullStack,
		query.GraphBackendNornicDB,
		nil,
		nil,
		"",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
	)

	if router.Status == nil {
		t.Fatal("newMCPQueryRouter().Status = nil, want status handler")
	}
	if router.Status.StatusReader != reader {
		t.Fatalf("newMCPQueryRouter().Status.StatusReader = %#v, want supplied reader", router.Status.StatusReader)
	}
	if router.Capabilities == nil {
		t.Fatal("newMCPQueryRouter().Capabilities = nil, want capability catalog handler mounted for get_capability_catalog")
	}
}

func TestNewSupplyChainEvidenceSourceIsWired(t *testing.T) {
	t.Parallel()

	if source := newSupplyChainEvidenceSource(nil, nil); source == nil {
		t.Fatal("newSupplyChainEvidenceSource() = nil, want durable supply-chain evidence source")
	}
}

func TestOpenQueryGraphAcceptsNornicDBOnSharedBoltPath(t *testing.T) {
	t.Parallel()

	_, _, err := openQueryGraph(context.Background(), func(key string) string {
		switch key {
		case "ESHU_GRAPH_BACKEND":
			return "nornicdb"
		case "ESHU_QUERY_PROFILE":
			return "production"
		default:
			return ""
		}
	}, "production", nil)
	if err == nil {
		t.Fatal("openQueryGraph() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "ESHU_NEO4J_URI") && !strings.Contains(err.Error(), "NEO4J_URI") {
		t.Fatalf("openQueryGraph() error = %q, want shared bolt config context", err)
	}
}

type staticStatusReader struct{}

func (staticStatusReader) ReadStatusSnapshot(context.Context, time.Time) (statuspkg.RawSnapshot, error) {
	return statuspkg.RawSnapshot{}, nil
}

func TestOpenQueryGraphDefaultsLocalLightweightDatabaseToNornic(t *testing.T) {
	t.Parallel()

	driver, databaseName, err := openQueryGraph(context.Background(), func(key string) string {
		if key == "ESHU_QUERY_PROFILE" {
			return "local_lightweight"
		}
		return ""
	}, query.ProfileLocalLightweight, nil)
	if err != nil {
		t.Fatalf("openQueryGraph() error = %v, want nil", err)
	}
	if driver != nil {
		t.Fatal("openQueryGraph() driver != nil, want nil for local_lightweight")
	}
	if databaseName != "nornic" {
		t.Fatalf("openQueryGraph() database = %q, want nornic", databaseName)
	}
}

func TestLoadGraphBackendDefaultsToNornicDB(t *testing.T) {
	t.Parallel()

	got, err := loadGraphBackend(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadGraphBackend() error = %v, want nil", err)
	}
	if got != query.GraphBackendNornicDB {
		t.Fatalf("loadGraphBackend() = %q, want %q", got, query.GraphBackendNornicDB)
	}
}
