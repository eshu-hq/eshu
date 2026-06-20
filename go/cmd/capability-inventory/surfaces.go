package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
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
	pages, err := enumerateConsolePages(filepath.Join(root, "apps", "console", "src", "pages"))
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
	return capabilitycatalog.LiveSurfaces{Surfaces: surfaces}, nil
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

// enumerateConsolePages returns the routed console page component names under
// the console pages directory. The tracking unit is a routed page, not any
// component, so it matches files ending in Page.tsx (the project convention: the
// console router elements are all *Page components) and excludes co-located
// panels and sub-components. Test files are skipped, and a missing directory
// yields no pages so the generator still runs in a Go-only checkout.
func enumerateConsolePages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read console pages dir %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".test.tsx") || !strings.HasSuffix(name, "Page.tsx") {
			continue
		}
		names = append(names, strings.TrimSuffix(name, ".tsx"))
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
