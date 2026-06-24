// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package maven parses Maven POM (pom.xml) manifests into the parent parser
// payload contract so the supply-chain impact reducer can correlate
// repository-declared Maven dependencies to package-registry identity.
//
// The parser never executes Maven, resolves parent POMs, or performs network
// lookups. It records the dependency truth that can be proven from the file
// alone and marks unresolved property references or missing versions as
// partial/unresolved evidence rather than guessing.
package maven

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// Parse decodes a Maven POM and returns the parent parser payload with a
// content_entity-shaped "variables" bucket carrying one row per declared
// dependency. The returned package_manager is always "maven"; the reducer
// normalizes that to the Maven ecosystem.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	payload := shared.BasePayload(path, "maven", isDependency)
	if len(source) == 0 {
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}

	var project pomProject
	if err := xml.Unmarshal(source, &project); err != nil {
		return nil, fmt.Errorf("parse maven pom %q: %w", path, err)
	}

	rows := make([]map[string]any, 0)
	properties := project.Properties.values()
	lineNumber := 1
	for _, dependency := range project.Dependencies.Dependency {
		row, ok := buildDependencyRow(dependency, "dependencies", properties, lineNumber)
		if !ok {
			continue
		}
		rows = append(rows, row)
		lineNumber++
	}
	for _, dependency := range project.DependencyManagement.Dependencies.Dependency {
		row, ok := buildDependencyRow(dependency, "dependencyManagement", properties, lineNumber)
		if !ok {
			continue
		}
		rows = append(rows, row)
		lineNumber++
	}
	for _, profile := range project.Profiles.Profile {
		for _, dependency := range profile.Dependencies.Dependency {
			row, ok := buildDependencyRow(dependency, "profiles:"+profile.ID+":dependencies", properties, lineNumber)
			if !ok {
				continue
			}
			rows = append(rows, row)
			lineNumber++
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		left, _ := rows[i]["name"].(string)
		right, _ := rows[j]["name"].(string)
		return left < right
	})
	payload["variables"] = rows

	if options.IndexSource {
		payload["source"] = string(source)
	}
	return payload, nil
}

type pomProject struct {
	XMLName              xml.Name          `xml:"project"`
	Properties           pomProperties     `xml:"properties"`
	Dependencies         pomDependencyList `xml:"dependencies"`
	DependencyManagement struct {
		Dependencies pomDependencyList `xml:"dependencies"`
	} `xml:"dependencyManagement"`
	Profiles struct {
		Profile []struct {
			ID           string            `xml:"id"`
			Dependencies pomDependencyList `xml:"dependencies"`
		} `xml:"profile"`
	} `xml:"profiles"`
}

type pomDependencyList struct {
	Dependency []pomDependency `xml:"dependency"`
}

type pomDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
	Optional   string `xml:"optional"`
	Type       string `xml:"type"`
	Classifier string `xml:"classifier"`
}

type pomProperties struct {
	Entries []pomProperty `xml:",any"`
}

type pomProperty struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

func (p pomProperties) values() map[string]string {
	out := make(map[string]string, len(p.Entries))
	for _, entry := range p.Entries {
		name := entry.XMLName.Local
		if name == "" {
			continue
		}
		out[name] = strings.TrimSpace(entry.Value)
	}
	return out
}

var propertyReferencePattern = regexp.MustCompile(`\$\{([^}]+)\}`)

func buildDependencyRow(
	dependency pomDependency,
	baseSection string,
	properties map[string]string,
	lineNumber int,
) (map[string]any, bool) {
	groupID := strings.TrimSpace(dependency.GroupID)
	artifactID := strings.TrimSpace(dependency.ArtifactID)
	if groupID == "" || artifactID == "" {
		return nil, false
	}

	scope := strings.ToLower(strings.TrimSpace(dependency.Scope))
	if scope == "" {
		scope = "compile"
	}
	section := baseSection
	if scope != "compile" {
		section = baseSection + ":" + scope
	}

	rawVersion := strings.TrimSpace(dependency.Version)
	resolutionState := "resolved"
	unresolvedKeys := []string(nil)
	value := rawVersion
	if rawVersion == "" {
		resolutionState = "partial"
	} else {
		resolved, unresolved := resolvePropertyReferences(rawVersion, properties)
		value = resolved
		if len(unresolved) > 0 {
			resolutionState = "unresolved"
			unresolvedKeys = unresolved
			value = rawVersion
		}
	}

	row := map[string]any{
		"name":                        groupID + ":" + artifactID,
		"line_number":                 lineNumber,
		"value":                       value,
		"section":                     section,
		"config_kind":                 "dependency",
		"package_manager":             "maven",
		"lang":                        "maven",
		"dependency_scope":            scope,
		"dependency_resolution_state": resolutionState,
		"direct_dependency":           true,
		"dependency_path_kind":        "manifest",
	}
	if strings.EqualFold(strings.TrimSpace(dependency.Optional), "true") {
		row["dependency_optional"] = true
	} else {
		row["dependency_optional"] = false
	}
	if dependency.Type != "" {
		row["dependency_type"] = strings.TrimSpace(dependency.Type)
	}
	if dependency.Classifier != "" {
		row["dependency_classifier"] = strings.TrimSpace(dependency.Classifier)
	}
	if len(unresolvedKeys) > 0 {
		row["dependency_unresolved_keys"] = unresolvedKeys
	}
	return row, true
}

func resolvePropertyReferences(raw string, properties map[string]string) (string, []string) {
	if !strings.Contains(raw, "${") {
		return raw, nil
	}
	unresolved := make([]string, 0)
	seen := make(map[string]struct{})
	resolved := propertyReferencePattern.ReplaceAllStringFunc(raw, func(match string) string {
		key := strings.TrimSpace(match[2 : len(match)-1])
		if key == "" {
			return match
		}
		if value, ok := properties[key]; ok && value != "" {
			return value
		}
		if _, recorded := seen[key]; !recorded {
			seen[key] = struct{}{}
			unresolved = append(unresolved, key)
		}
		return match
	})
	sort.Strings(unresolved)
	return resolved, unresolved
}
