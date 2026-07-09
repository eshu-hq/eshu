// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const gitServiceCatalogCollectorInstanceID = "git-service-catalog"

func emitServiceCatalogFactsForContentFile(
	w factStreamWriter,
	scopeID string,
	generationID string,
	observedAt time.Time,
	relativePath string,
	body string,
) {
	for _, envelope := range serviceCatalogManifestEnvelopes(
		[]byte(body),
		scopeID,
		generationID,
		observedAt,
		relativePath,
	) {
		w.send(envelope)
	}
}

func serviceCatalogManifestEnvelopes(
	body []byte,
	scopeID string,
	generationID string,
	observedAt time.Time,
	relativePath string,
) []facts.Envelope {
	sourceURI, ok := serviceCatalogSourceURI(relativePath)
	if !ok {
		return nil
	}
	provider, ok := serviceCatalogProviderForPath(sourceURI)
	if !ok {
		return nil
	}
	ctx := servicecatalog.FixtureContext{
		ScopeID:             scopeID,
		GenerationID:        generationID,
		CollectorInstanceID: gitServiceCatalogCollectorInstanceID,
		ObservedAt:          observedAt,
		SourceURI:           sourceURI,
	}
	var (
		envelopes []facts.Envelope
		err       error
	)
	switch provider {
	case servicecatalog.ProviderBackstage:
		envelopes, err = servicecatalog.BackstageManifestEnvelopes(body, ctx)
	case servicecatalog.ProviderOpsLevel:
		envelopes, err = servicecatalog.OpsLevelManifestEnvelopes(body, ctx)
	case servicecatalog.ProviderCortex:
		envelopes, err = servicecatalog.CortexManifestEnvelopes(body, ctx)
	}
	if err != nil {
		return nil
	}
	return envelopes
}

func serviceCatalogProviderForPath(relativePath string) (servicecatalog.Provider, bool) {
	sourceURI, ok := serviceCatalogSourceURI(relativePath)
	if !ok {
		return "", false
	}
	base := strings.ToLower(path.Base(sourceURI))
	switch base {
	case "catalog-info.yaml", "catalog-info.yml":
		return servicecatalog.ProviderBackstage, true
	case "opslevel.yml", "opslevel.yaml":
		return servicecatalog.ProviderOpsLevel, true
	case "cortex.yaml", "cortex.yml":
		return servicecatalog.ProviderCortex, true
	default:
		return "", false
	}
}

func serviceCatalogSourceURI(relativePath string) (string, bool) {
	sourceURI := path.Clean(filepath.ToSlash(strings.TrimSpace(relativePath)))
	if sourceURI == "." || path.IsAbs(sourceURI) || sourceURI == ".." || strings.HasPrefix(sourceURI, "../") {
		return "", false
	}
	return sourceURI, true
}
