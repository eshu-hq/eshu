// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencecontinuity

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type capabilityMatrixFile struct {
	Capabilities []struct {
		Capability string   `yaml:"capability"`
		Tools      []string `yaml:"tools"`
	} `yaml:"capabilities"`
}

type surfaceInventoryFile struct {
	Surfaces []struct {
		Category string `json:"category"`
		Name     string `json:"name"`
	} `json:"surfaces"`
}

// ValidateRepository loads repository-local specs and generated surface truth,
// then validates the evidence-continuity matrix against them.
func ValidateRepository(repoRoot string) ([]Finding, error) {
	contract, err := LoadContract(filepath.Join(repoRoot, "specs", "evidence-continuity.v1.yaml"))
	if err != nil {
		return nil, err
	}
	surfaces, err := LoadSurfaceIndex(repoRoot)
	if err != nil {
		return nil, err
	}
	findings := Validate(contract, surfaces)
	findings = append(findings, validateReferencedTests(repoRoot, contract)...)
	sortFindings(findings)
	return findings, nil
}

// LoadContract reads the evidence-continuity matrix.
func LoadContract(path string) (Contract, error) {
	var contract Contract
	if err := readYAML(path, &contract); err != nil {
		return Contract{}, err
	}
	return contract, nil
}

// LoadSurfaceIndex loads known capabilities and generated public surfaces.
func LoadSurfaceIndex(repoRoot string) (SurfaceIndex, error) {
	capabilities, capabilityMCPTools, err := loadCapabilities(filepath.Join(repoRoot, "specs"))
	if err != nil {
		return SurfaceIndex{}, err
	}
	apiRoutes, mcpTools, err := loadSurfaces(filepath.Join(repoRoot, "go", "internal", "capabilitycatalog", "data", "surface-inventory.generated.json"))
	if err != nil {
		return SurfaceIndex{}, err
	}
	return SurfaceIndex{
		Capabilities:       capabilities,
		CapabilityMCPTools: capabilityMCPTools,
		APIRoutes:          apiRoutes,
		MCPTools:           mcpTools,
	}, nil
}

func loadCapabilities(specsDir string) (map[string]struct{}, map[string]map[string]struct{}, error) {
	capabilities := map[string]struct{}{}
	capabilityMCPTools := map[string]map[string]struct{}{}
	if err := loadCapabilityFile(filepath.Join(specsDir, "capability-matrix.v1.yaml"), capabilities, capabilityMCPTools); err != nil {
		return nil, nil, err
	}
	fragmentDir := filepath.Join(specsDir, "capability-matrix")
	entries, err := os.ReadDir(fragmentDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read capability matrix fragments: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		if err := loadCapabilityFile(filepath.Join(fragmentDir, entry.Name()), capabilities, capabilityMCPTools); err != nil {
			return nil, nil, err
		}
	}
	return capabilities, capabilityMCPTools, nil
}

func loadCapabilityFile(path string, capabilities map[string]struct{}, capabilityMCPTools map[string]map[string]struct{}) error {
	var matrix capabilityMatrixFile
	if err := readYAML(path, &matrix); err != nil {
		return err
	}
	for _, capability := range matrix.Capabilities {
		name := strings.TrimSpace(capability.Capability)
		if name == "" {
			continue
		}
		capabilities[name] = struct{}{}
		if _, ok := capabilityMCPTools[name]; !ok {
			capabilityMCPTools[name] = map[string]struct{}{}
		}
		for _, tool := range capability.Tools {
			tool = strings.TrimSpace(tool)
			if tool != "" {
				capabilityMCPTools[name][tool] = struct{}{}
			}
		}
	}
	return nil
}

func loadSurfaces(path string) (map[string]struct{}, map[string]struct{}, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- static verifier reads repo-local generated inventory paths, not request input.
	if err != nil {
		return nil, nil, fmt.Errorf("read surface inventory %s: %w", path, err)
	}
	var inventory surfaceInventoryFile
	if err := json.Unmarshal(raw, &inventory); err != nil {
		return nil, nil, fmt.Errorf("parse surface inventory %s: %w", path, err)
	}
	apiRoutes := map[string]struct{}{}
	mcpTools := map[string]struct{}{}
	for _, surface := range inventory.Surfaces {
		switch surface.Category {
		case "api_route":
			apiRoutes[surface.Name] = struct{}{}
		case "mcp_tool":
			mcpTools[surface.Name] = struct{}{}
		}
	}
	return apiRoutes, mcpTools, nil
}

func readYAML(path string, out any) error {
	raw, err := os.ReadFile(path) // #nosec G304 -- static verifier reads repo-local YAML specs selected by developer or CI.
	if err != nil {
		return fmt.Errorf("read yaml %s: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parse yaml %s: %w", path, err)
	}
	return nil
}

type proofRefSite struct {
	rowID string
	label string
	ref   string
}

func validateReferencedTests(repoRoot string, contract Contract) []Finding {
	cache := map[string]map[string]struct{}{}
	var findings []Finding
	for _, site := range collectProofRefs(contract) {
		packages, tests, ok := parseGoTestRef(site.ref)
		if !ok {
			continue
		}
		for _, test := range tests {
			found := false
			var loadErr error
			for _, pkg := range packages {
				names, err := loadPackageTestNames(repoRoot, pkg, cache)
				if err != nil {
					loadErr = err
					continue
				}
				if _, ok := names[test]; ok {
					found = true
					break
				}
			}
			if found {
				continue
			}
			message := fmt.Sprintf("%s proof references unknown test %q", site.label, test)
			if loadErr != nil {
				message = fmt.Sprintf("%s: %v", message, loadErr)
			}
			findings = append(findings, Finding{Kind: FindingInvalidProofRef, RowID: site.rowID, Message: message})
		}
	}
	return findings
}

func collectProofRefs(contract Contract) []proofRefSite {
	var sites []proofRefSite
	for _, row := range contract.Rows {
		sites = append(
			sites,
			proofRefSite{rowID: row.ID, label: "source fact", ref: row.SourceFact.Ref},
			proofRefSite{rowID: row.ID, label: "projection/read-model", ref: row.Continuity.Projection.Ref},
			proofRefSite{rowID: row.ID, label: "API answer", ref: row.Continuity.API.Ref},
			proofRefSite{rowID: row.ID, label: "MCP answer", ref: row.Continuity.MCP.Ref},
			proofRefSite{rowID: row.ID, label: "no-provider", ref: row.EmptyStates.NoProvider.Ref},
			proofRefSite{rowID: row.ID, label: "no-collector", ref: row.EmptyStates.NoCollector.Ref},
			proofRefSite{rowID: row.ID, label: "empty-state", ref: row.EmptyStates.Empty.Ref},
		)
		for _, proof := range row.NegativeCase {
			sites = append(sites, proofRefSite{rowID: row.ID, label: proof.Case, ref: proof.Ref})
		}
	}
	return sites
}

func loadPackageTestNames(repoRoot, pkg string, cache map[string]map[string]struct{}) (map[string]struct{}, error) {
	if names, ok := cache[pkg]; ok {
		return names, nil
	}
	dir, err := packageDir(repoRoot, pkg)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read tests for %s: %w", pkg, err)
	}
	names := map[string]struct{}{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse tests for %s: %w", pkg, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				names[fn.Name.Name] = struct{}{}
			}
		}
	}
	cache[pkg] = names
	return names, nil
}

func packageDir(repoRoot, pkg string) (string, error) {
	pkg = strings.TrimSpace(pkg)
	if !strings.HasPrefix(pkg, "./") || strings.Contains(pkg, "..") {
		return "", fmt.Errorf("unsupported proof package %q", pkg)
	}
	return filepath.Join(repoRoot, "go", filepath.FromSlash(strings.TrimPrefix(pkg, "./"))), nil
}
