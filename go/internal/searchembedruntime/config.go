// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchembedruntime

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/searchembed"
	"github.com/eshu-hq/eshu/go/internal/searchembedprovider"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
)

const (
	// EnvLocalEmbedder explicitly selects the deterministic no-network embedder.
	EnvLocalEmbedder = "ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER"
	// EnvProviderProfilesJSON names the provider profile registry.
	EnvProviderProfilesJSON = semanticprofile.EnvProviderProfilesJSON
	// EnvProviderProfileID selects one search_documents provider profile when
	// multiple governed profiles are configured.
	EnvProviderProfileID = "ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID"
)

const (
	// LocalProviderProfileID is the persisted-vector profile id for zero-key local mode.
	LocalProviderProfileID = "local"
	// AutoLocalEmbedder selects a governed provider when configured, otherwise
	// falling back to the deterministic local hash embedder.
	AutoLocalEmbedder = "auto_hash"
	// LocalEmbeddingModelID is the persisted-vector model id for the hash embedder.
	LocalEmbeddingModelID = "local-hash-v1"
	// SourceClass is the curated search-document source class used for vectors.
	SourceClass = semanticprofile.SourceSearchDocuments
	// VectorIndexVersion is the current persisted vector schema version.
	VectorIndexVersion = "vector-v1"
)

// Config is the selected semantic-search embedding runtime identity.
type Config struct {
	Enabled            bool
	Embedder           searchhybrid.Embedder
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
	VectorRetrieval    searchhybrid.VectorRetrievalMode
	policy             semanticpolicy.Policy
	profile            semanticprofile.ProviderProfile
	policyRequired     bool
}

// ConfigFromEnv selects an embedder from environment configuration without
// calling provider endpoints.
func ConfigFromEnv(getenv func(string) string, client *http.Client) (Config, error) {
	if getenv == nil {
		return Config{}, nil
	}
	local := strings.TrimSpace(getenv(EnvLocalEmbedder))
	switch {
	case isExplicitLocalEmbedder(local):
		return localConfig(local)
	case local != "" && local != AutoLocalEmbedder:
		return Config{}, fmt.Errorf("invalid %s %q", EnvLocalEmbedder, local)
	}

	rawProfiles := strings.TrimSpace(getenv(EnvProviderProfilesJSON))
	if rawProfiles == "" {
		return autoLocalConfig(local)
	}
	profiles, err := semanticprofile.ParseProfilesJSON(rawProfiles)
	if err != nil {
		return Config{}, err
	}
	policy, err := semanticpolicy.LoadFromEnv(getenv)
	if err != nil {
		return Config{}, err
	}
	profiles = applySourcePolicy(profiles, policy)
	profile, ok, err := selectProviderProfile(profiles, strings.TrimSpace(getenv(EnvProviderProfileID)))
	if err != nil || !ok {
		if err != nil {
			return Config{}, err
		}
		return autoLocalConfig(local)
	}
	embedder, err := searchembedprovider.New(profile, getenv, client)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Enabled:            true,
		Embedder:           embedder,
		ProviderProfileID:  profile.ProfileID,
		SourceClass:        SourceClass,
		EmbeddingModelID:   profile.ModelID,
		VectorIndexVersion: VectorIndexVersion,
		VectorRetrieval:    searchhybrid.VectorRetrievalAuto,
		policy:             policy,
		profile:            profile,
		policyRequired:     true,
	}, nil
}

// AllowsSearchDocument reports whether the selected embedder may process one
// curated search document under the configured source policy.
func (c Config) AllowsSearchDocument(repoID string, documentID string, sourcePath string) bool {
	if !c.Enabled {
		return false
	}
	if !c.policyRequired {
		return true
	}
	decision := semanticpolicy.Evaluate(
		c.policy,
		semanticpolicy.Request{
			ProviderProfileID: c.ProviderProfileID,
			SourceClass:       SourceClass,
			Scope: semanticpolicy.Scope{
				Kind: semanticpolicy.ScopeRepository,
				ID:   strings.TrimSpace(repoID),
			},
			SourceID:   strings.TrimSpace(documentID),
			DocumentID: strings.TrimSpace(documentID),
			SourcePath: strings.TrimSpace(sourcePath),
			ACLState:   semanticpolicy.ACLAllowed,
		},
		semanticprofile.ProviderStatuses([]semanticprofile.ProviderProfile{c.profile}),
	)
	return decision.Allowed
}

func isExplicitLocalEmbedder(local string) bool {
	return local == "hash" || local == "local_hash"
}

func autoLocalConfig(local string) (Config, error) {
	if local != AutoLocalEmbedder {
		return Config{}, nil
	}
	return localConfig("hash")
}

func localConfig(local string) (Config, error) {
	if !isExplicitLocalEmbedder(local) {
		return Config{}, fmt.Errorf("invalid %s %q", EnvLocalEmbedder, local)
	}
	embedder, err := searchembed.NewHashEmbedder(searchembed.DefaultDimensions)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Enabled:            true,
		Embedder:           embedder,
		ProviderProfileID:  LocalProviderProfileID,
		SourceClass:        SourceClass,
		EmbeddingModelID:   LocalEmbeddingModelID,
		VectorIndexVersion: VectorIndexVersion,
		VectorRetrieval:    searchhybrid.VectorRetrievalAuto,
	}, nil
}

func applySourcePolicy(
	profiles []semanticprofile.ProviderProfile,
	policy semanticpolicy.Policy,
) []semanticprofile.ProviderProfile {
	statuses := semanticpolicy.ApplyToProviderStatuses(semanticprofile.ProviderStatuses(profiles), policy)
	allowed := make(map[string][]string, len(statuses))
	for _, row := range statuses {
		if row.SourcePolicyConfigured {
			allowed[row.ProfileID] = row.SourceClasses
		}
	}
	out := make([]semanticprofile.ProviderProfile, 0, len(profiles))
	for _, profile := range profiles {
		profile.SourcePolicyConfigured = false
		profile.SourceClasses = nil
		if classes, ok := allowed[profile.ProfileID]; ok {
			profile.SourcePolicyConfigured = true
			profile.SourceClasses = slices.Clone(classes)
		}
		out = append(out, profile)
	}
	return out
}

func selectProviderProfile(
	profiles []semanticprofile.ProviderProfile,
	selector string,
) (semanticprofile.ProviderProfile, bool, error) {
	eligible := make([]semanticprofile.ProviderProfile, 0, len(profiles))
	for _, profile := range profiles {
		if !slices.Contains(profile.SourceClasses, semanticprofile.SourceSearchDocuments) {
			continue
		}
		if !profile.SourcePolicyConfigured {
			continue
		}
		eligible = append(eligible, profile)
	}
	if selector != "" {
		for _, profile := range eligible {
			if profile.ProfileID == selector {
				return profile, true, nil
			}
		}
		return semanticprofile.ProviderProfile{}, false, fmt.Errorf("semantic search provider profile %q is not configured for %s", selector, SourceClass)
	}
	switch len(eligible) {
	case 0:
		return semanticprofile.ProviderProfile{}, false, nil
	case 1:
		return eligible[0], true, nil
	default:
		return semanticprofile.ProviderProfile{}, false, fmt.Errorf("multiple semantic search provider profiles configured; set %s", EnvProviderProfileID)
	}
}
