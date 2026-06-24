// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomdocument

// cycloneDXDocument is the slice of the CycloneDX JSON shape Eshu projects
// into stable facts. Fields outside the shape are surfaced as warning facts
// rather than being silently consumed.
type cycloneDXDocument struct {
	BOMFormat       string                 `json:"bomFormat"`
	SpecVersion     string                 `json:"specVersion"`
	SerialNumber    string                 `json:"serialNumber"`
	Version         any                    `json:"version"`
	Metadata        *cycloneDXMetadata     `json:"metadata"`
	Components      []cycloneDXComponent   `json:"components"`
	Dependencies    []cycloneDXDependency  `json:"dependencies"`
	Vulnerabilities []map[string]any       `json:"vulnerabilities"`
	Services        []map[string]any       `json:"services"`
	Compositions    []map[string]any       `json:"compositions"`
	Formulation     []map[string]any       `json:"formulation"`
	Annotations     []map[string]any       `json:"annotations"`
	Extensions      *map[string]any        `json:"properties"`
	Signatures      *map[string]any        `json:"signature"`
	Definitions     *map[string]any        `json:"definitions"`
	ExternalRefs    []cycloneDXExternalRef `json:"externalReferences"`
}

type cycloneDXMetadata struct {
	Timestamp string              `json:"timestamp"`
	Tools     any                 `json:"tools"`
	Component *cycloneDXComponent `json:"component"`
	Supplier  *cycloneDXSupplier  `json:"supplier"`
	Authors   []map[string]any    `json:"authors"`
	Licenses  []cycloneDXLicense  `json:"licenses"`
}

type cycloneDXComponent struct {
	Type         string                 `json:"type"`
	BOMRef       string                 `json:"bom-ref"`
	Name         string                 `json:"name"`
	Group        string                 `json:"group"`
	Version      string                 `json:"version"`
	Description  string                 `json:"description"`
	Publisher    string                 `json:"publisher"`
	PURL         string                 `json:"purl"`
	CPE          string                 `json:"cpe"`
	SWID         *map[string]any        `json:"swid"`
	Hashes       []cycloneDXHash        `json:"hashes"`
	Licenses     []cycloneDXLicense     `json:"licenses"`
	Supplier     *cycloneDXSupplier     `json:"supplier"`
	Scope        string                 `json:"scope"`
	Properties   []map[string]any       `json:"properties"`
	ExternalRefs []cycloneDXExternalRef `json:"externalReferences"`
	Components   []cycloneDXComponent   `json:"components"`
	Evidence     *map[string]any        `json:"evidence"`
}

type cycloneDXHash struct {
	Alg     string `json:"alg"`
	Content string `json:"content"`
}

type cycloneDXLicense struct {
	License    *cycloneDXLicenseDetail `json:"license"`
	Expression string                  `json:"expression"`
}

type cycloneDXLicenseDetail struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type cycloneDXSupplier struct {
	Name    string           `json:"name"`
	URL     []string         `json:"url"`
	Contact []map[string]any `json:"contact"`
}

type cycloneDXExternalRef struct {
	Type    string `json:"type"`
	URL     string `json:"url"`
	Comment string `json:"comment"`
}

type cycloneDXDependency struct {
	Ref       string                `json:"ref"`
	DependsOn []string              `json:"dependsOn"`
	Provides  []string              `json:"provides"`
	Children  []cycloneDXDependency `json:"dependencies"`
}
