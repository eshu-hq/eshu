package collector

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const gitServiceCatalogCollectorInstanceID = "git-service-catalog"

func serviceCatalogFactCount(
	repoPath string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	snapshot RepositorySnapshot,
) int {
	total := 0
	if len(snapshot.ContentFileMetas) > 0 {
		for _, meta := range snapshot.ContentFileMetas {
			total += serviceCatalogFactCountForFile(
				repoPath,
				scopeID,
				generationID,
				observedAt,
				meta.RelativePath,
				nil,
			)
		}
		return total
	}
	for _, fileSnapshot := range snapshot.ContentFiles {
		total += len(serviceCatalogManifestEnvelopes(
			[]byte(fileSnapshot.Body),
			scopeID,
			generationID,
			observedAt,
			fileSnapshot.RelativePath,
		))
	}
	return total
}

func serviceCatalogFactCountForFile(
	repoPath string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	relativePath string,
	body []byte,
) int {
	if _, ok := serviceCatalogProviderForPath(relativePath); !ok {
		return 0
	}
	if body == nil {
		raw, err := os.ReadFile(filepath.Join(repoPath, filepath.FromSlash(relativePath)))
		if err != nil {
			return 0
		}
		body = raw
	}
	return len(serviceCatalogManifestEnvelopes(body, scopeID, generationID, observedAt, relativePath))
}

func emitServiceCatalogFactsForContentFile(
	ch chan<- facts.Envelope,
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
		ch <- envelope
	}
}

func serviceCatalogManifestEnvelopes(
	body []byte,
	scopeID string,
	generationID string,
	observedAt time.Time,
	relativePath string,
) []facts.Envelope {
	provider, ok := serviceCatalogProviderForPath(relativePath)
	if !ok {
		return nil
	}
	ctx := servicecatalog.FixtureContext{
		ScopeID:             scopeID,
		GenerationID:        generationID,
		CollectorInstanceID: gitServiceCatalogCollectorInstanceID,
		ObservedAt:          observedAt,
		SourceURI:           serviceCatalogSourceURI(relativePath),
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
	base := strings.ToLower(path.Base(serviceCatalogSourceURI(relativePath)))
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

func serviceCatalogSourceURI(relativePath string) string {
	return path.Clean(filepath.ToSlash(strings.TrimSpace(relativePath)))
}
