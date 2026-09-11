// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"

	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/auth"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/catalog"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/cicd"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/cloud"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/code"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/evidence"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/freshness"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/iac"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/impact"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/infrastructure"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/repository"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/search"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/service"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/status"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/supplychain"
)

const swaggerUIHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Eshu API - Swagger UI</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function () {
      window.ui = SwaggerUIBundle({
        url: "/api/v0/openapi.json",
        dom_id: "#swagger-ui",
        deepLinking: true
      });
    };
  </script>
</body>
</html>
`

const redocHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Eshu API - ReDoc</title>
</head>
<body>
  <redoc spec-url="/api/v0/openapi.json"></redoc>
  <script src="https://cdn.jsdelivr.net/npm/redoc@2/bundles/redoc.standalone.js"></script>
</body>
</html>
`

// Spec returns the OpenAPI 3.0 specification for the Eshu Query API,
// assembled from the path fragments in paths/ and the shared component
// and schema blocks. The version placeholder is substituted at call time,
// so the returned document always names the running build.
func Spec() string {
	return strings.Replace(
		specPrefix+
			repository.Routes+
			repository.StatsAndCoverage+
			repository.Branches+
			repository.Freshness+
			search.Entities+
			evidence.Investigations+
			service.IntelligenceReport+
			code.Routes+
			code.RouteToCaller+
			code.Graph+
			code.Quality+
			code.Security+
			code.DeadCodeScan+
			code.DeadCodeInvestigation+
			code.CrossRepoDeadCode+
			code.Symbols+
			code.Flow+
			iac.Routes+
			iac.Resources+
			cloud.AWSRuntimeDrift+
			iac.TerraformConfigStateDrift+
			iac.ReplatformingSelectors+
			iac.ReplatformingRollups+
			iac.Replatforming+
			iac.ReplatformingOwnership+
			search.Content+
			status.Metrics+
			catalog.Capabilities+
			catalog.SurfaceInventory+
			status.Admin+
			infrastructure.Routes+
			search.GraphEntities+
			infrastructure.ResourceAggregate+
			cloud.Routes+
			cloud.Inventory+
			cloud.RuntimeDrift+
			impact.Routes+
			impact.Contract+
			impact.Rest+
			impact.Exposure+
			evidence.Routes+
			evidence.Bundle+
			evidence.DocumentationFindingAggregate+
			repository.PackageRegistry+
			repository.PackageRegistryAggregate+
			repository.Dependencies+
			code.Owners+
			cicd.Routes+
			cicd.RunCorrelationAggregate+
			service.Catalog+
			infrastructure.Kubernetes+
			infrastructure.SecretsIAM+
			infrastructure.ObservabilityCoverage+
			supplychain.Images+
			supplychain.TagHistory+
			supplychain.VulnerabilityScannerContract+
			supplychain.ContainerImages+
			supplychain.Routes+
			supplychain.ImpactAggregate+
			supplychain.SecurityAlerts+
			supplychain.SecurityAlertAggregate+
			supplychain.ContainerImageIdentityAggregate+
			supplychain.AdvisoryCatalog+
			supplychain.AdvisoryEvidence+
			supplychain.SBOMAttestations+
			supplychain.SBOMAttestationAttachmentAggregate+
			evidence.IncidentContext+
			evidence.WorkItem+
			evidence.VisualizationPackets+
			freshness.Generations+
			freshness.ChangedSince+
			freshness.ServiceChangedSince+
			status.HostedReadiness+
			status.OperatorControlPlane+
			status.Operations+
			freshness.Causality+
			status.CollectorReadiness+
			status.Governance+
			status.Semantic+
			status.AnswerNarration+
			catalog.ComponentExtensions+
			status.CollectorExtractionReadiness+
			status.FactSchemaVersion+
			catalog.Playbooks+
			evidence.InvestigationWorkflows+
			search.Semantic+
			search.SemanticEvidence+
			auth.Routes+
			auth.Setup+
			auth.Tokens+
			auth.TOTP+
			auth.AdminReads+
			auth.AdminMutations+
			auth.AdminProviderConfigs+
			auth.SignInPolicy+
			search.Ask+
			status.Routes+
			status.Compare+
			components,
		"__ESHU_VERSION__",
		buildinfo.AppVersion(),
		1,
	)
}

// ServeSpec writes the assembled specification as JSON.
func ServeSpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(Spec()))
}

// ServeSwaggerUI serves a browser UI for exploring the OpenAPI schema.
func ServeSwaggerUI(w http.ResponseWriter, _ *http.Request) {
	serveDocumentationHTML(w, swaggerUIHTML)
}

// ServeReDoc serves a reader-friendly OpenAPI reference page.
func ServeReDoc(w http.ResponseWriter, _ *http.Request) {
	serveDocumentationHTML(w, redocHTML)
}

func serveDocumentationHTML(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}
