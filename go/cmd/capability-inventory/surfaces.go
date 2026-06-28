// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const defaultSurfaceArtifactOut = "internal/capabilitycatalog/data/surface-inventory.generated.json"

// liveSurfaces enumerates every platform surface from live code, specs, and the
// source tree rooted at root (the repository root). The collector, reducer
// domain, and MCP tool sets come from in-code registries; command binaries and
// console pages are read from the source tree; API routes are parsed from the
// served OpenAPI spec. This is the authoritative live set the surface inventory
// drift gate reconciles against, so a surface added or removed in code without
// updating the committed artifact is caught.
func liveSurfaces(root string) (capabilitycatalog.LiveSurfaces, error) {
	commands, err := enumerateCommandBinaries(filepath.Join(root, "go", "cmd"))
	if err != nil {
		return capabilitycatalog.LiveSurfaces{}, err
	}
	pages, err := enumerateConsolePages(root)
	if err != nil {
		return capabilitycatalog.LiveSurfaces{}, err
	}
	routes, err := enumerateAPIRoutes()
	if err != nil {
		return capabilitycatalog.LiveSurfaces{}, err
	}

	surfaces := map[capabilitycatalog.SurfaceCategory][]string{
		capabilitycatalog.SurfaceCommand:       commands,
		capabilitycatalog.SurfaceCollector:     enumerateCollectors(),
		capabilitycatalog.SurfaceReducerDomain: enumerateReducerDomains(),
		capabilitycatalog.SurfaceAPIRoute:      routes,
		capabilitycatalog.SurfaceMCPTool:       enumerateMCPTools(),
		capabilitycatalog.SurfaceConsolePage:   pages,
	}
	return capabilitycatalog.LiveSurfaces{
		Surfaces:           surfaces,
		CollectorFactKinds: collectorFactKinds(),
	}, nil
}

// enumerateCommandBinaries returns the command binary names under go/cmd: one
// per subdirectory. Non-directory entries (such as the package README) are
// skipped.
func enumerateCommandBinaries(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read commands dir %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

var (
	consolePageImportPattern     = regexp.MustCompile(`import\s+\{\s*([A-Za-z0-9_]+Page)\s*\}\s+from\s+"\.\/pages\/([A-Za-z0-9_]+Page)"`)
	consoleLazyPageImportPattern = regexp.MustCompile(`const\s+([A-Za-z0-9_]+Page)\s*=\s*(?:React\.)?lazy\s*\(\s*\(\)\s*=>\s*import\s*\(\s*"\.\/pages\/([A-Za-z0-9_]+Page)"\s*\)`)
	consoleRoutePattern          = regexp.MustCompile(`(?s)<Route\b.*?/>`)
	consoleRoutePagePattern      = regexp.MustCompile(`<([A-Za-z0-9_]+Page)(?:\s|/|>)`)
)

// consoleRouterFiles lists the console source files that together hold the
// router's page imports and <Route> table. The shell (App.tsx) and the extracted
// routes table (appRoutes.tsx) are read as one logical router so that splitting
// the routes out of App.tsx to honor the file-size limit does not drop console
// page surfaces from the inventory.
var consoleRouterFiles = []string{"App.tsx", "appRoutes.tsx"}

// enumerateConsolePages returns the routed console page component names from the
// console router. The tracking unit is a routed page, not any Page.tsx file in
// the pages directory; orphan, prototype, and test-only page components must not
// be reported as production console surfaces. Imports and <Route> elements are
// read across every consoleRouterFiles entry, so the router may be split across
// the App shell and an extracted routes table without losing surfaces.
func enumerateConsolePages(root string) ([]string, error) {
	var combined strings.Builder
	for _, name := range consoleRouterFiles {
		routerPath := filepath.Join(root, "apps", "console", "src", name)
		fileBody, err := os.ReadFile(routerPath) // #nosec G304 -- routerPath is constructed by the program from a fixed internal template joined to the repo root, not from untrusted input
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read console router %s: %w", routerPath, err)
		}
		combined.Write(fileBody)
		combined.WriteByte('\n')
	}
	body := combined.String()
	imports := map[string]string{}
	for _, match := range consolePageImportPattern.FindAllStringSubmatch(string(body), -1) {
		if match[1] == match[2] {
			imports[match[1]] = match[2]
		}
	}
	for _, match := range consoleLazyPageImportPattern.FindAllStringSubmatch(string(body), -1) {
		if match[1] == match[2] {
			imports[match[1]] = match[2]
		}
	}
	seen := map[string]struct{}{}
	var names []string
	for _, route := range consoleRoutePattern.FindAllString(string(body), -1) {
		for _, match := range consoleRoutePagePattern.FindAllStringSubmatch(route, -1) {
			name, ok := imports[match[1]]
			if !ok {
				continue
			}
			pagePath := filepath.Join(root, "apps", "console", "src", "pages", name+".tsx")
			if _, err := os.Stat(pagePath); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("stat console page %s: %w", pagePath, err)
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// httpOperationMethods is the set of OpenAPI path-item keys that are HTTP
// operations. Other keys (parameters, summary, description, servers, $ref) are
// path-item metadata, not operations, and are skipped.
var httpOperationMethods = map[string]struct{}{
	"get": {}, "put": {}, "post": {}, "delete": {},
	"options": {}, "head": {}, "patch": {}, "trace": {},
}

// enumerateAPIRoutes parses the served OpenAPI spec and returns one surface per
// method+path operation (for example "GET /api/v0/capabilities"). Recording the
// method, not just the path key, means adding a second operation such as
// "POST /api/v0/foo" beside an existing "GET /api/v0/foo" changes the inventory,
// so method-level API surface changes cannot bypass the drift gate. The spec is
// the single declaration of the HTTP surface.
func enumerateAPIRoutes() ([]string, error) {
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(query.OpenAPISpec()), &doc); err != nil {
		return nil, fmt.Errorf("parse openapi spec: %w", err)
	}
	var names []string
	for path, item := range doc.Paths {
		for method := range item {
			if _, ok := httpOperationMethods[strings.ToLower(method)]; !ok {
				continue
			}
			names = append(names, strings.ToUpper(method)+" "+path)
		}
	}
	sort.Strings(names)
	return names, nil
}

// enumerateCollectors returns every collector family from the scope registry.
func enumerateCollectors() []string {
	kinds := scope.AllCollectorKinds()
	names := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		names = append(names, string(kind))
	}
	sort.Strings(names)
	return names
}

// enumerateReducerDomains returns every reducer domain from the domain registry.
func enumerateReducerDomains() []string {
	domains := reducer.AllDomains()
	names := make([]string, 0, len(domains))
	for _, domain := range domains {
		names = append(names, string(domain))
	}
	sort.Strings(names)
	return names
}

// enumerateMCPTools returns every read-only MCP tool name from the registry.
func enumerateMCPTools() []string {
	tools := mcp.ReadOnlyTools()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

// collectorFactKinds maps each first-party collector kind to the core fact
// families it can emit. capabilitycatalog reconciles this live map against the
// surface-inventory overlay so a newly registered fact kind cannot ship without
// source-to-read-surface provenance.
func collectorFactKinds() map[string][]string {
	return map[string][]string{
		string(scope.CollectorGit): appendFactKinds(
			facts.DocumentationFactKinds(),
			facts.ServiceCatalogFactKinds(),
		),
		string(scope.CollectorAWS): appendFactKinds(
			facts.AWSFactKinds(),
			facts.EC2InstancePostureFactKinds(),
			facts.RDSPostureFactKinds(),
			facts.S3BucketPostureFactKinds(),
			facts.S3ExternalPrincipalGrantFactKinds(),
			filterFactKinds(facts.SecretsIAMFactKinds(), "aws_", "eks_"),
		),
		string(scope.CollectorAzure): facts.AzureFactKinds(),
		string(scope.CollectorGCP): appendFactKinds(
			facts.GCPFactKinds(),
			filterFactKinds(facts.SecretsIAMFactKinds(), "gcp_"),
		),
		string(scope.CollectorTerraformState):            facts.TerraformStateFactKinds(),
		string(scope.CollectorDocumentation):             facts.DocumentationFactKinds(),
		string(scope.CollectorOCIRegistry):               facts.OCIRegistryFactKinds(),
		string(scope.CollectorPackageRegistry):           facts.PackageRegistryFactKinds(),
		string(scope.CollectorVulnerabilityIntelligence): facts.VulnerabilityIntelligenceFactKinds(),
		string(scope.CollectorSBOMAttestation):           facts.SBOMAttestationFactKinds(),
		string(scope.CollectorSecurityAlert):             facts.SecurityAlertFactKinds(),
		string(scope.CollectorCICDRun):                   facts.CICDRunFactKinds(),
		string(scope.CollectorPagerDuty): appendFactKinds(
			facts.IncidentContextFactKinds(),
			facts.IncidentRoutingFactKinds(),
		),
		string(scope.CollectorJira):               facts.WorkItemFactKinds(),
		string(scope.CollectorScannerWorker):      facts.ScannerWorkerFactKinds(),
		string(scope.CollectorSemanticExtraction): facts.SemanticFactKinds(),
		string(scope.CollectorKubernetesLive): appendFactKinds(
			facts.KubernetesLiveFactKinds(),
			filterFactKinds(facts.SecretsIAMFactKinds(), "k8s_", "eks_"),
		),
		string(scope.CollectorVaultLive):       filterFactKinds(facts.SecretsIAMFactKinds(), "vault_", "secrets_iam_"),
		string(scope.CollectorPrometheusMimir): facts.ObservabilityFactKinds(),
		string(scope.CollectorTempo):           facts.ObservabilityFactKinds(),
		string(scope.CollectorGrafana):         facts.ObservabilityFactKinds(),
		string(scope.CollectorLoki):            facts.ObservabilityFactKinds(),
	}
}

func appendFactKinds(groups ...[]string) []string {
	var out []string
	for _, group := range groups {
		out = append(out, group...)
	}
	sort.Strings(out)
	return compactFactKinds(out)
}

func filterFactKinds(kinds []string, prefixes ...string) []string {
	var out []string
	for _, kind := range kinds {
		for _, prefix := range prefixes {
			if strings.HasPrefix(kind, prefix) {
				out = append(out, kind)
				break
			}
		}
	}
	sort.Strings(out)
	return compactFactKinds(out)
}

func compactFactKinds(kinds []string) []string {
	if len(kinds) == 0 {
		return nil
	}
	out := kinds[:0]
	var previous string
	for i, kind := range kinds {
		if kind == "" || (i > 0 && kind == previous) {
			continue
		}
		out = append(out, kind)
		previous = kind
	}
	return out
}

// buildSurfaceInventory enumerates live surfaces under root, loads the editorial
// overlay from specsDir, and reconciles them into the surface inventory plus
// findings.
func buildSurfaceInventory(specsDir, root string) (capabilitycatalog.SurfaceInventory, []capabilitycatalog.Finding, error) {
	live, err := liveSurfaces(root)
	if err != nil {
		return capabilitycatalog.SurfaceInventory{}, nil, err
	}
	overlay, err := capabilitycatalog.LoadSurfaceOverlay(filepath.Join(specsDir, capabilitycatalog.SurfaceOverlayFileName))
	if err != nil {
		return capabilitycatalog.SurfaceInventory{}, nil, err
	}
	inv, findings := capabilitycatalog.BuildSurfaceInventory(live, overlay)
	return inv, findings, nil
}
