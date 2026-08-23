// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"fmt"
	"strings"
)

// QueryProfile names one supported query runtime profile.
type QueryProfile string

// Query profiles define the supported runtime evidence tiers.
const (
	// ProfileLocalLightweight serves bounded content-backed local reads.
	ProfileLocalLightweight   QueryProfile = "local_lightweight"
	ProfileLocalAuthoritative QueryProfile = "local_authoritative"
	ProfileLocalFullStack     QueryProfile = "local_full_stack"
	ProfileProduction         QueryProfile = "production"
)

// GraphBackend names one supported graph adapter.
type GraphBackend string

// Graph backends define the supported canonical and compatibility adapters.
const (
	// GraphBackendNeo4j selects the Neo4j compatibility adapter.
	GraphBackendNeo4j    GraphBackend = "neo4j"
	GraphBackendNornicDB GraphBackend = "nornicdb"
)

// ParseGraphBackend validates raw against the supported adapters.
func ParseGraphBackend(raw string) (GraphBackend, error) {
	switch GraphBackend(strings.TrimSpace(raw)) {
	case "":
		return GraphBackendNornicDB, nil
	case GraphBackendNeo4j:
		return GraphBackendNeo4j, nil
	case GraphBackendNornicDB:
		return GraphBackendNornicDB, nil
	default:
		return "", fmt.Errorf("invalid graph backend %q", strings.TrimSpace(raw))
	}
}

// NormalizeQueryProfile returns a supported profile or the empty profile.
func NormalizeQueryProfile(raw string) QueryProfile {
	profile, err := ParseQueryProfile(raw)
	if err != nil {
		return ""
	}
	return profile
}

// ParseQueryProfile validates raw against the supported query profiles.
func ParseQueryProfile(raw string) (QueryProfile, error) {
	switch QueryProfile(strings.TrimSpace(raw)) {
	case "":
		return "", nil
	case ProfileLocalLightweight:
		return ProfileLocalLightweight, nil
	case ProfileLocalAuthoritative:
		return ProfileLocalAuthoritative, nil
	case ProfileLocalFullStack:
		return ProfileLocalFullStack, nil
	case ProfileProduction:
		return ProfileProduction, nil
	default:
		return "", fmt.Errorf("invalid query profile %q", strings.TrimSpace(raw))
	}
}
