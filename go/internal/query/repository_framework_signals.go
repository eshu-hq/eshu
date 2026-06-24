// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

func collectRepositoryFrameworkSignals(
	file FileContent,
	frameworks map[string]*repositoryFrameworkAggregate,
) {
	relativePath := cleanRepositoryRelativePath(file.RelativePath)
	if relativePath == "" {
		return
	}

	base := strings.ToLower(filepath.Base(relativePath))
	content := file.Content
	lowerContent := strings.ToLower(content)

	switch {
	case base == "package.json":
		collectPackageJSONFrameworkSignals(relativePath, content, frameworks)
	case base == "pyproject.toml", base == "pipfile", base == "requirements.txt",
		strings.HasPrefix(base, "requirements-"), strings.HasPrefix(base, "requirements_"), base == "setup.py":
		collectPythonFrameworkSignals(relativePath, lowerContent, "package_dependency", frameworks)
	case strings.HasPrefix(base, "next.config."):
		noteRepositoryFrameworkSignal(frameworks, "nextjs", "config_file", relativePath, 1)
	}

	switch strings.ToLower(filepath.Ext(relativePath)) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts":
		collectJavaScriptFrameworkSignals(relativePath, lowerContent, frameworks)
	case ".py":
		collectPythonFrameworkSignals(relativePath, lowerContent, "source_import", frameworks)
	}
}

func collectPackageJSONFrameworkSignals(
	relativePath string,
	content string,
	frameworks map[string]*repositoryFrameworkAggregate,
) {
	type packageManifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}

	var manifest packageManifest
	if err := json.Unmarshal([]byte(content), &manifest); err == nil {
		for dependency := range manifest.Dependencies {
			noteRepositoryFrameworkFromPackage(frameworks, dependency, relativePath)
		}
		for dependency := range manifest.DevDependencies {
			noteRepositoryFrameworkFromPackage(frameworks, dependency, relativePath)
		}
		return
	}

	lowerContent := strings.ToLower(content)
	for framework, dependency := range map[string]string{
		"express": "express",
		"fastify": "fastify",
		"hapi":    "@hapi/hapi",
		"nextjs":  `"next"`,
		"nestjs":  "@nestjs/core",
		"react":   "react",
		"svelte":  "svelte",
		"vue":     "vue",
	} {
		if strings.Contains(lowerContent, strings.ToLower(dependency)) {
			noteRepositoryFrameworkSignal(frameworks, framework, "package_dependency", relativePath, 1)
		}
	}
}

func noteRepositoryFrameworkFromPackage(
	frameworks map[string]*repositoryFrameworkAggregate,
	dependency string,
	relativePath string,
) {
	switch strings.ToLower(strings.TrimSpace(dependency)) {
	case "react":
		noteRepositoryFrameworkSignal(frameworks, "react", "package_dependency", relativePath, 1)
	case "express":
		noteRepositoryFrameworkSignal(frameworks, "express", "package_dependency", relativePath, 1)
	case "fastify":
		noteRepositoryFrameworkSignal(frameworks, "fastify", "package_dependency", relativePath, 1)
	case "@hapi/hapi", "hapi":
		noteRepositoryFrameworkSignal(frameworks, "hapi", "package_dependency", relativePath, 1)
	case "next":
		noteRepositoryFrameworkSignal(frameworks, "nextjs", "package_dependency", relativePath, 1)
	case "@nestjs/core":
		noteRepositoryFrameworkSignal(frameworks, "nestjs", "package_dependency", relativePath, 1)
	case "vue":
		noteRepositoryFrameworkSignal(frameworks, "vue", "package_dependency", relativePath, 1)
	case "svelte":
		noteRepositoryFrameworkSignal(frameworks, "svelte", "package_dependency", relativePath, 1)
	}
}

func collectJavaScriptFrameworkSignals(
	relativePath string,
	lowerContent string,
	frameworks map[string]*repositoryFrameworkAggregate,
) {
	for framework, needle := range map[string]string{
		"express": "express",
		"fastify": "fastify",
		"hapi":    "@hapi/hapi",
		"nestjs":  "@nestjs/core",
		"react":   "react",
		"vue":     "vue",
		"svelte":  "svelte",
	} {
		if strings.Contains(lowerContent, "'"+needle+"'") ||
			strings.Contains(lowerContent, `"`+needle+`"`) ||
			strings.Contains(lowerContent, "require(\""+needle+"\")") ||
			strings.Contains(lowerContent, "require('"+needle+"')") {
			noteRepositoryFrameworkSignal(frameworks, framework, "source_import", relativePath, 1)
		}
	}
}

func collectPythonFrameworkSignals(
	relativePath string,
	lowerContent string,
	evidenceKind string,
	frameworks map[string]*repositoryFrameworkAggregate,
) {
	for framework, needles := range map[string][]string{
		"fastapi": {"fastapi", "from fastapi import", "import fastapi"},
		"flask":   {"flask", "from flask import", "import flask"},
		"django":  {"django", "from django", "import django"},
	} {
		for _, needle := range needles {
			if strings.Contains(lowerContent, needle) {
				noteRepositoryFrameworkSignal(frameworks, framework, evidenceKind, relativePath, 1)
				break
			}
		}
	}
}

func noteRepositoryFrameworkSignal(
	frameworks map[string]*repositoryFrameworkAggregate,
	framework string,
	evidenceKind string,
	relativePath string,
	count int,
) {
	framework = strings.TrimSpace(strings.ToLower(framework))
	evidenceKind = strings.TrimSpace(evidenceKind)
	relativePath = cleanRepositoryRelativePath(relativePath)
	if framework == "" || evidenceKind == "" || count <= 0 {
		return
	}

	aggregate, ok := frameworks[framework]
	if !ok {
		aggregate = &repositoryFrameworkAggregate{
			evidenceKinds: map[string]struct{}{},
			paths:         map[string]struct{}{},
		}
		frameworks[framework] = aggregate
	}
	aggregate.signalCount += count
	aggregate.evidenceKinds[evidenceKind] = struct{}{}
	if relativePath != "" {
		aggregate.paths[relativePath] = struct{}{}
	}
}

func repositoryFrameworkConfidence(aggregate *repositoryFrameworkAggregate) string {
	if aggregate == nil {
		return "low"
	}
	if _, ok := aggregate.evidenceKinds["semantic_entity"]; ok {
		return "high"
	}
	if len(aggregate.evidenceKinds) >= 2 {
		return "high"
	}
	if _, ok := aggregate.evidenceKinds["package_dependency"]; ok {
		return "medium"
	}
	if _, ok := aggregate.evidenceKinds["source_import"]; ok {
		return "medium"
	}
	return "low"
}

func isRepositoryNarrativeCandidate(relativePath string) bool {
	lowerPath := strings.ToLower(cleanRepositoryRelativePath(relativePath))
	if lowerPath == "" {
		return false
	}
	if isRepositoryDocumentationFile(lowerPath) {
		return true
	}
	if isCatalogDescriptorPath(lowerPath) {
		return true
	}
	base := strings.ToLower(filepath.Base(lowerPath))
	switch {
	case base == "package.json",
		base == "pyproject.toml",
		base == "pipfile",
		base == "requirements.txt",
		strings.HasPrefix(base, "requirements-"),
		strings.HasPrefix(base, "requirements_"),
		base == "setup.py",
		strings.HasPrefix(base, "next.config."):
		return true
	}
	if isServiceEvidenceCandidate(FileContent{RelativePath: lowerPath}, "") {
		return true
	}
	switch filepath.Ext(lowerPath) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts", ".py":
		return strings.Contains(base, "app") ||
			strings.Contains(base, "server") ||
			strings.Contains(base, "main") ||
			strings.Contains(base, "index") ||
			strings.Contains(base, "api")
	default:
		return false
	}
}

func isRepositoryDocumentationFile(relativePath string) bool {
	lowerPath := strings.ToLower(cleanRepositoryRelativePath(relativePath))
	if lowerPath == "" {
		return false
	}
	base := strings.ToLower(filepath.Base(lowerPath))
	if strings.HasPrefix(base, "readme.") {
		return true
	}
	if isCatalogDescriptorPath(lowerPath) {
		return true
	}
	if strings.HasPrefix(lowerPath, "docs/") && strings.HasSuffix(lowerPath, ".md") {
		return true
	}
	return false
}

func isCatalogDescriptorPath(relativePath string) bool {
	base := strings.ToLower(filepath.Base(relativePath))
	return base == "catalog-info.yaml" || base == "catalog-info.yml"
}

func stringIntMapValue(value map[string]any, key string) map[string]int {
	if len(value) == 0 {
		return nil
	}
	raw, ok := value[key]
	if !ok {
		return nil
	}
	typed, ok := raw.(map[string]int)
	if ok {
		return typed
	}
	return nil
}

func cloneStringAnyMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
