// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func readComposeDocument(t *testing.T, name string) composeDocument {
	t.Helper()

	return readComposeDocumentWithIncludes(t, name, map[string]bool{})
}

func readComposeDocumentWithIncludes(t *testing.T, name string, seen map[string]bool) composeDocument {
	t.Helper()

	cleanName := filepath.Clean(name)
	if seen[cleanName] {
		return composeDocument{}
	}
	seen[cleanName] = true

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", cleanName))
	if err != nil {
		t.Fatalf("read %s: %v", cleanName, err)
	}

	var doc composeDocument
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", cleanName, err)
	}
	if doc.Services == nil {
		doc.Services = map[string]composeService{}
	}
	for _, included := range doc.Include {
		includedName := filepath.Join(filepath.Dir(cleanName), included)
		includedDoc := readComposeDocumentWithIncludes(t, includedName, seen)
		for serviceName, service := range includedDoc.Services {
			if _, exists := doc.Services[serviceName]; exists {
				t.Fatalf("compose service %q declared more than once through %s", serviceName, cleanName)
			}
			doc.Services[serviceName] = service
		}
	}
	return doc
}

func readRemoteE2EComposeSource(t *testing.T) string {
	t.Helper()

	return readComposeSourceWithIncludes(t, "docker-compose.remote-e2e.yaml", map[string]bool{})
}

func readComposeSourceWithIncludes(t *testing.T, name string, seen map[string]bool) string {
	t.Helper()

	cleanName := filepath.Clean(name)
	if seen[cleanName] {
		return ""
	}
	seen[cleanName] = true

	raw := readRepositoryFile(t, "../../..", cleanName)
	var doc composeDocument
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("parse %s: %v", cleanName, err)
	}
	var builder strings.Builder
	builder.WriteString(raw)
	for _, service := range doc.Services {
		for key, value := range service.Environment {
			if rendered, ok := value.(string); ok {
				builder.WriteByte('\n')
				builder.WriteString(key)
				builder.WriteString(": ")
				builder.WriteString(rendered)
			}
		}
	}
	for _, included := range doc.Include {
		builder.WriteByte('\n')
		builder.WriteString(readComposeSourceWithIncludes(t, filepath.Join(filepath.Dir(cleanName), included), seen))
	}
	return builder.String()
}
